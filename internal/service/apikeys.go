package service

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/storage"
	"chatgpt2api/internal/util"
)

const apiKeysDocumentName = "api_keys.json"

// apiKeyUsageZone is the zone daily counters reset in.
var apiKeyUsageZone = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

// APIKey is a standalone API credential that is not bound to any user.
type APIKey struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	KeyHash          string   `json:"key_hash"`
	KeyPrefix        string   `json:"key_prefix"`
	Enabled          bool     `json:"enabled"`
	ExpiresAt        string   `json:"expires_at"`
	TotalLimit       int      `json:"total_limit"`
	DailyLimit       int      `json:"daily_limit"`
	ImageLimit       int      `json:"image_limit"`
	RPMLimit         int      `json:"rpm_limit"`
	ConcurrencyLimit int      `json:"concurrency_limit"`
	Models           []string `json:"models"`
	UsedRequests     int      `json:"used_requests"`
	UsedImages       int      `json:"used_images"`
	TodayRequests    int      `json:"today_requests"`
	TodayImages      int      `json:"today_images"`
	TodayDate        string   `json:"today_date"`
	LastUsedAt       string   `json:"last_used_at"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	Source           string   `json:"source,omitempty"`
}

// APIKeyError is an OpenAI-style admission error.
type APIKeyError struct {
	Status  int
	Code    string
	Type    string
	Message string
}

func (e *APIKeyError) Error() string { return e.Message }

// OpenAIError renders the error body.
func (e *APIKeyError) OpenAIError() map[string]any {
	return map[string]any{"error": map[string]any{"message": e.Message, "type": e.Type, "param": nil, "code": e.Code}}
}

// APIKeyService stores standalone API keys and enforces their limits.
type APIKeyService struct {
	mu      sync.Mutex
	store   storage.JSONDocumentBackend
	items   []*APIKey
	windows map[string][]time.Time
	running map[string]int
	dirtyAt time.Time
	now     func() time.Time
}

// NewAPIKeyService loads keys from the JSON document store.
func NewAPIKeyService(backend storage.Backend) *APIKeyService {
	s := &APIKeyService{windows: map[string][]time.Time{}, running: map[string]int{}, now: time.Now}
	if store, ok := backend.(storage.JSONDocumentBackend); ok {
		s.store = store
	}
	s.items = s.load()
	return s
}

func (s *APIKeyService) load() []*APIKey {
	if s.store == nil {
		return nil
	}
	raw, err := s.store.LoadJSONDocument(apiKeysDocumentName)
	if err != nil || raw == nil {
		return nil
	}
	out := []*APIKey{}
	for _, item := range util.AsMapSlice(util.StringMap(raw)["items"]) {
		key := apiKeyFromMap(item)
		if key.ID != "" && key.KeyHash != "" {
			out = append(out, key)
		}
	}
	return out
}

func apiKeyFromMap(item map[string]any) *APIKey {
	return &APIKey{
		ID:               util.Clean(item["id"]),
		Name:             util.Clean(item["name"]),
		KeyHash:          util.Clean(item["key_hash"]),
		KeyPrefix:        util.Clean(item["key_prefix"]),
		Enabled:          util.ToBool(util.ValueOr(item["enabled"], true)),
		ExpiresAt:        util.Clean(item["expires_at"]),
		TotalLimit:       util.ToInt(item["total_limit"], 0),
		DailyLimit:       util.ToInt(item["daily_limit"], 0),
		ImageLimit:       util.ToInt(item["image_limit"], 0),
		RPMLimit:         util.ToInt(item["rpm_limit"], 0),
		ConcurrencyLimit: util.ToInt(item["concurrency_limit"], 0),
		Models:           cleanModelList(util.AsStringSlice(item["models"])),
		UsedRequests:     util.ToInt(item["used_requests"], 0),
		UsedImages:       util.ToInt(item["used_images"], 0),
		TodayRequests:    util.ToInt(item["today_requests"], 0),
		TodayImages:      util.ToInt(item["today_images"], 0),
		TodayDate:        util.Clean(item["today_date"]),
		LastUsedAt:       util.Clean(item["last_used_at"]),
		CreatedAt:        util.Clean(item["created_at"]),
		UpdatedAt:        util.Clean(item["updated_at"]),
		Source:           util.Clean(item["source"]),
	}
}

func (k *APIKey) storedMap() map[string]any {
	return map[string]any{
		"id": k.ID, "name": k.Name, "key_hash": k.KeyHash, "key_prefix": k.KeyPrefix,
		"enabled": k.Enabled, "expires_at": k.ExpiresAt,
		"total_limit": k.TotalLimit, "daily_limit": k.DailyLimit, "image_limit": k.ImageLimit,
		"rpm_limit": k.RPMLimit, "concurrency_limit": k.ConcurrencyLimit, "models": append([]string{}, k.Models...),
		"used_requests": k.UsedRequests, "used_images": k.UsedImages,
		"today_requests": k.TodayRequests, "today_images": k.TodayImages, "today_date": k.TodayDate,
		"last_used_at": k.LastUsedAt, "created_at": k.CreatedAt, "updated_at": k.UpdatedAt, "source": k.Source,
	}
}

func (s *APIKeyService) publicLocked(k *APIKey) map[string]any {
	s.rollDayLocked(k)
	out := k.storedMap()
	delete(out, "key_hash")
	out["running"] = s.running[k.ID]
	out["expired"] = k.expired(s.now())
	return out
}

func (s *APIKeyService) saveLocked() error {
	if s.store == nil {
		return nil
	}
	items := make([]map[string]any, 0, len(s.items))
	for _, k := range s.items {
		items = append(items, k.storedMap())
	}
	return s.store.SaveJSONDocument(apiKeysDocumentName, map[string]any{"items": items})
}

func (k *APIKey) expired(now time.Time) bool {
	if k.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, k.ExpiresAt)
	if err != nil {
		return false
	}
	return !now.Before(t)
}

func (s *APIKeyService) today() string {
	return s.now().In(apiKeyUsageZone).Format("2006-01-02")
}

func (s *APIKeyService) rollDayLocked(k *APIKey) {
	if day := s.today(); k.TodayDate != day {
		k.TodayDate = day
		k.TodayRequests = 0
		k.TodayImages = 0
	}
}

func (s *APIKeyService) findLocked(id string) *APIKey {
	for _, k := range s.items {
		if k.ID == id {
			return k
		}
	}
	return nil
}

// List returns every key without its hash, newest first.
func (s *APIKeyService) List() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.items))
	for _, k := range s.items {
		out = append(out, s.publicLocked(k))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return util.Clean(out[i]["created_at"]) > util.Clean(out[j]["created_at"])
	})
	return out
}

// Get returns one key without its hash.
func (s *APIKeyService) Get(id string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k := s.findLocked(id); k != nil {
		return s.publicLocked(k)
	}
	return nil
}

// NameByID maps key ids to names (for log/gallery decoration).
func (s *APIKeyService) NameByID() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for _, k := range s.items {
		out[k.ID] = k.Name
	}
	return out
}

func generateAPIKeyValue() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return "sk-" + strings.TrimRight(base64.RawURLEncoding.EncodeToString(buf), "=")
}

func apiKeyPrefix(raw string) string {
	runes := []rune(raw)
	if len(runes) <= 8 {
		return string(runes[:min(len(runes), 3)]) + "…"
	}
	return string(runes[:7]) + "…" + string(runes[len(runes)-4:])
}

func cleanModelList(models []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, m := range models {
		for _, part := range strings.FieldsFunc(m, func(r rune) bool { return r == ',' || r == '\n' }) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}

func normalizeExpiresAt(value any) (string, error) {
	text := util.Clean(value)
	if text == "" {
		return "", nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, text, apiKeyUsageZone); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("expires_at 格式无效")
}

func (s *APIKeyService) applyInputLocked(k *APIKey, input map[string]any) error {
	if v, ok := input["name"]; ok {
		k.Name = strings.TrimSpace(util.Clean(v))
	}
	if v, ok := input["enabled"]; ok {
		k.Enabled = util.ToBool(v)
	}
	if v, ok := input["expires_at"]; ok {
		exp, err := normalizeExpiresAt(v)
		if err != nil {
			return err
		}
		k.ExpiresAt = exp
	}
	for field, target := range map[string]*int{
		"total_limit": &k.TotalLimit, "daily_limit": &k.DailyLimit, "image_limit": &k.ImageLimit,
		"rpm_limit": &k.RPMLimit, "concurrency_limit": &k.ConcurrencyLimit,
	} {
		if v, ok := input[field]; ok {
			n := util.ToInt(v, 0)
			if n < 0 {
				n = 0
			}
			*target = n
		}
	}
	if v, ok := input["models"]; ok {
		if text, isText := v.(string); isText {
			k.Models = cleanModelList([]string{text})
		} else {
			k.Models = cleanModelList(util.AsStringSlice(v))
		}
	}
	return nil
}

// Create adds a key. When input["key"] is empty a random value is generated.
// The raw value is returned once and never stored.
func (s *APIKeyService) Create(input map[string]any) (map[string]any, string, error) {
	raw := strings.TrimSpace(util.Clean(input["key"]))
	if raw == "" {
		raw = generateAPIKeyValue()
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return nil, "", errors.New("key 不能包含空白字符")
	}
	hash := util.SHA256Hex(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.items {
		if k.KeyHash == hash {
			return nil, "", errors.New("该 Key 已存在")
		}
	}
	now := util.NowISO()
	k := &APIKey{ID: util.NewHex(12), KeyHash: hash, KeyPrefix: apiKeyPrefix(raw), Enabled: true, CreatedAt: now, UpdatedAt: now, Models: []string{}}
	if err := s.applyInputLocked(k, input); err != nil {
		return nil, "", err
	}
	if k.Name == "" {
		k.Name = "Key " + k.ID[:6]
	}
	s.rollDayLocked(k)
	s.items = append(s.items, k)
	if err := s.saveLocked(); err != nil {
		return nil, "", err
	}
	return s.publicLocked(k), raw, nil
}

// Update edits a key's settings.
func (s *APIKeyService) Update(id string, input map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.findLocked(id)
	if k == nil {
		return nil, errors.New("key not found")
	}
	if err := s.applyInputLocked(k, input); err != nil {
		return nil, err
	}
	k.UpdatedAt = util.NowISO()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s.publicLocked(k), nil
}

// Delete removes a key.
func (s *APIKeyService) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, k := range s.items {
		if k.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			delete(s.windows, id)
			_ = s.saveLocked()
			return true
		}
	}
	return false
}

// ResetUsage zeroes the usage counters of a key.
func (s *APIKeyService) ResetUsage(id string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.findLocked(id)
	if k == nil {
		return nil, errors.New("key not found")
	}
	k.UsedRequests, k.UsedImages, k.TodayRequests, k.TodayImages = 0, 0, 0, 0
	k.TodayDate = s.today()
	delete(s.windows, id)
	k.UpdatedAt = util.NowISO()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s.publicLocked(k), nil
}

// Lookup returns the key matching raw without admitting a request.
func (s *APIKeyService) Lookup(raw string) *APIKey {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	hash := util.SHA256Hex(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.items {
		if hmac.Equal([]byte(k.KeyHash), []byte(hash)) {
			copied := *k
			copied.Models = append([]string{}, k.Models...)
			return &copied
		}
	}
	return nil
}

// APIKeyLease tracks one admitted request; Finish must be called once.
type APIKeyLease struct {
	service *APIKeyService
	keyID   string
	once    sync.Once
}

// Finish releases the concurrency slot and records generated images.
func (l *APIKeyLease) Finish(images int) {
	if l == nil || l.service == nil {
		return
	}
	l.once.Do(func() {
		s := l.service
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.running[l.keyID] <= 1 {
			delete(s.running, l.keyID)
		} else {
			s.running[l.keyID]--
		}
		if k := s.findLocked(l.keyID); k != nil && images > 0 {
			s.rollDayLocked(k)
			k.UsedImages += images
			k.TodayImages += images
			_ = s.saveLocked()
		}
	})
}

func apiKeyLimitError(code, message string) *APIKeyError {
	return &APIKeyError{Status: http.StatusTooManyRequests, Code: code, Type: "rate_limit_exceeded", Message: message}
}

// Admit validates the key for a request: enabled, not expired, model allowed,
// and all request/image/RPM/concurrency limits respected. imageUnits is the
// number of images the request asks for (0 for text requests).
func (s *APIKeyService) Admit(keyID, model string, imageUnits int) (*APIKeyLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.findLocked(keyID)
	if k == nil {
		return nil, &APIKeyError{Status: http.StatusUnauthorized, Code: "invalid_api_key", Type: "invalid_request_error", Message: "Incorrect API key provided."}
	}
	now := s.now()
	if !k.Enabled {
		return nil, &APIKeyError{Status: http.StatusForbidden, Code: "api_key_disabled", Type: "permission_error", Message: "This API key has been disabled."}
	}
	if k.expired(now) {
		return nil, &APIKeyError{Status: http.StatusForbidden, Code: "api_key_expired", Type: "permission_error", Message: "This API key has expired."}
	}
	if len(k.Models) > 0 && model != "" {
		allowed := false
		for _, m := range k.Models {
			if strings.EqualFold(m, model) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, &APIKeyError{Status: http.StatusForbidden, Code: "model_not_allowed", Type: "permission_error", Message: fmt.Sprintf("This API key is not allowed to use model %q.", model)}
		}
	}
	s.rollDayLocked(k)
	if k.TotalLimit > 0 && k.UsedRequests >= k.TotalLimit {
		return nil, apiKeyLimitError("total_limit_exceeded", fmt.Sprintf("API key total request limit reached (%d).", k.TotalLimit))
	}
	if k.DailyLimit > 0 && k.TodayRequests >= k.DailyLimit {
		return nil, apiKeyLimitError("daily_limit_exceeded", fmt.Sprintf("API key daily request limit reached (%d).", k.DailyLimit))
	}
	if k.ImageLimit > 0 && imageUnits > 0 && k.UsedImages+imageUnits > k.ImageLimit {
		return nil, apiKeyLimitError("image_limit_exceeded", fmt.Sprintf("API key image limit reached (%d/%d).", k.UsedImages, k.ImageLimit))
	}
	if k.RPMLimit > 0 {
		cutoff := now.Add(-time.Minute)
		window := s.windows[k.ID][:0]
		for _, t := range s.windows[k.ID] {
			if t.After(cutoff) {
				window = append(window, t)
			}
		}
		s.windows[k.ID] = window
		if len(window) >= k.RPMLimit {
			return nil, apiKeyLimitError("rpm_limit_exceeded", fmt.Sprintf("API key rate limit reached (%d requests per minute).", k.RPMLimit))
		}
	}
	if k.ConcurrencyLimit > 0 && s.running[k.ID] >= k.ConcurrencyLimit {
		return nil, apiKeyLimitError("concurrency_limit_exceeded", fmt.Sprintf("API key concurrency limit reached (%d).", k.ConcurrencyLimit))
	}
	if k.RPMLimit > 0 {
		s.windows[k.ID] = append(s.windows[k.ID], now)
	}
	s.running[k.ID]++
	k.UsedRequests++
	k.TodayRequests++
	k.LastUsedAt = now.UTC().Format(time.RFC3339)
	// Persist usage at most every few seconds under load; Finish and the
	// next admission flush the rest.
	if now.Sub(s.dirtyAt) >= 2*time.Second {
		s.dirtyAt = now
		_ = s.saveLocked()
	}
	return &APIKeyLease{service: s, keyID: k.ID}, nil
}

// Flush persists pending usage counters.
func (s *APIKeyService) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.saveLocked()
}

// UsageSummary aggregates today's and total usage across keys.
func (s *APIKeyService) UsageSummary() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	todayReq, todayImg, totalReq, totalImg, enabled := 0, 0, 0, 0, 0
	for _, k := range s.items {
		s.rollDayLocked(k)
		todayReq += k.TodayRequests
		todayImg += k.TodayImages
		totalReq += k.UsedRequests
		totalImg += k.UsedImages
		if k.Enabled && !k.expired(s.now()) {
			enabled++
		}
	}
	return map[string]any{
		"keys": len(s.items), "enabled_keys": enabled,
		"today_requests": todayReq, "today_images": todayImg,
		"total_requests": totalReq, "total_images": totalImg,
	}
}

// MigrateLegacyKeys imports user-bound API keys from the legacy auth store as
// standalone keys (same hash, no limits) so existing clients keep working.
// Already imported hashes are skipped. It returns the number imported.
func (s *APIKeyService) MigrateLegacyKeys(items []map[string]any) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	known := map[string]struct{}{}
	for _, k := range s.items {
		known[k.KeyHash] = struct{}{}
	}
	added := 0
	for _, item := range items {
		if util.Clean(item["kind"]) != AuthKindAPIKey || util.Clean(item["role"]) == AuthRoleAdmin {
			continue
		}
		hash := util.Clean(item["key_hash"])
		if hash == "" {
			continue
		}
		if _, ok := known[hash]; ok {
			continue
		}
		known[hash] = struct{}{}
		prefix := "legacy…"
		if raw := util.Clean(item["key"]); raw != "" {
			prefix = apiKeyPrefix(raw)
		}
		name := firstNonEmpty(util.Clean(item["owner_name"]), util.Clean(item["name"]), "迁移 Key")
		if owner := util.Clean(item["owner_name"]); owner != "" && util.Clean(item["name"]) != "" && owner != util.Clean(item["name"]) {
			name = owner + " · " + util.Clean(item["name"])
		}
		now := util.NowISO()
		s.items = append(s.items, &APIKey{
			ID: util.NewHex(12), Name: name, KeyHash: hash, KeyPrefix: prefix,
			Enabled:   util.ToBool(util.ValueOr(item["enabled"], true)),
			CreatedAt: firstNonEmpty(util.Clean(item["created_at"]), now), UpdatedAt: now,
			Models: []string{}, Source: "migrated", TodayDate: s.today(),
		})
		added++
	}
	if added > 0 {
		_ = s.saveLocked()
	}
	return added
}
