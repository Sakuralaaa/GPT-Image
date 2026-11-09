package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/storage"
	"chatgpt2api/internal/util"
)

const recoveryJobsDocumentName = "recovery_jobs.json"

// Recovery item states.
const (
	RecoveryQueued       = "queued"
	RecoveryRunning      = "running"
	RecoveryAwaitingCode = "awaiting_code"
	RecoverySucceeded    = "succeeded"
	RecoveryFailed       = "failed"
	RecoveryCancelled    = "cancelled"
)

// RecoverySettings tunes manual jobs and the automatic self-heal loop.
type RecoverySettings struct {
	DefaultConcurrency   int  `json:"default_concurrency"`
	GlobalConcurrency    int  `json:"global_concurrency"`
	OTPWaitSeconds       int  `json:"otp_wait_seconds"`
	AutoEnabled          bool `json:"auto_enabled"`
	AutoConcurrency      int  `json:"auto_concurrency"`
	AutoIntervalMinutes  int  `json:"auto_interval_minutes"`
	AutoCooldownMinutes  int  `json:"auto_cooldown_minutes"`
	AutoDailyAttemptCap  int  `json:"auto_daily_attempt_cap"`
	AutoIncludeExpired   bool `json:"auto_include_expired"`
	AutoSkipWithoutCreds bool `json:"auto_skip_without_creds"`
	// outlookEmail (github.com/assast/outlookEmail) channel, recovery only.
	OutlookBaseURL string   `json:"outlook_base_url"`
	OutlookAPIKey  string   `json:"outlook_api_key"`
	OutlookDomains []string `json:"outlook_domains"`
	// Recovery-only cloudflare_temp_email deployments, routed by suffix.
	CloudflareMails []RecoveryCFMail `json:"cloudflare_mails"`
}

func defaultRecoverySettings() RecoverySettings {
	return RecoverySettings{
		DefaultConcurrency:   3,
		GlobalConcurrency:    6,
		OTPWaitSeconds:       90,
		AutoEnabled:          false,
		AutoConcurrency:      2,
		AutoIntervalMinutes:  10,
		AutoCooldownMinutes:  60,
		AutoDailyAttemptCap:  3,
		AutoIncludeExpired:   false,
		AutoSkipWithoutCreds: true,
		OutlookDomains:       []string{"outlook.com", "hotmail.com"},
	}
}

func (s RecoverySettings) normalized() RecoverySettings {
	d := defaultRecoverySettings()
	clamp := func(v, lo, hi, def int) int {
		if v <= 0 {
			return def
		}
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	s.DefaultConcurrency = clamp(s.DefaultConcurrency, 1, 20, d.DefaultConcurrency)
	s.GlobalConcurrency = clamp(s.GlobalConcurrency, 1, 50, d.GlobalConcurrency)
	s.OTPWaitSeconds = clamp(s.OTPWaitSeconds, 15, 600, d.OTPWaitSeconds)
	s.AutoConcurrency = clamp(s.AutoConcurrency, 1, 20, d.AutoConcurrency)
	s.AutoIntervalMinutes = clamp(s.AutoIntervalMinutes, 1, 1440, d.AutoIntervalMinutes)
	s.AutoCooldownMinutes = clamp(s.AutoCooldownMinutes, 1, 10080, d.AutoCooldownMinutes)
	if s.OutlookDomains == nil {
		s.OutlookDomains = d.OutlookDomains
	}
	cleaned := []string{}
	for _, domain := range s.OutlookDomains {
		if domain = strings.ToLower(strings.TrimSpace(domain)); domain != "" {
			cleaned = append(cleaned, domain)
		}
	}
	s.OutlookDomains = cleaned
	s.OutlookBaseURL = strings.TrimSpace(s.OutlookBaseURL)
	s.OutlookAPIKey = strings.TrimSpace(s.OutlookAPIKey)
	s.CloudflareMails = normalizeRecoveryCFMails(s.CloudflareMails)
	s.AutoDailyAttemptCap = clamp(s.AutoDailyAttemptCap, 1, 100, d.AutoDailyAttemptCap)
	return s
}

// RecoveryItem is one account inside a recovery job.
type RecoveryItem struct {
	ID           string   `json:"id"`
	Email        string   `json:"email"`
	Status       string   `json:"status"`
	Stage        string   `json:"stage"`
	Error        string   `json:"error"`
	Method       string   `json:"method"`
	Attempts     int      `json:"attempts"`
	TokenPreview string   `json:"token_preview"`
	Channel      string   `json:"channel"`
	Steps        []string `json:"steps"`
	UpdatedAt    string   `json:"updated_at"`
	FinishedAt   string   `json:"finished_at"`
}

// RecoveryJob is a persistent batch of recovery items.
type RecoveryJob struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Source      string          `json:"source"`
	Concurrency int             `json:"concurrency"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	Items       []*RecoveryItem `json:"items"`
}

type recoveryHealState struct {
	Day      string `json:"day"`
	Attempts int    `json:"attempts"`
	Failures int    `json:"failures"`
	NextAt   string `json:"next_at"`
}

// recoverySession keeps an in-flight email-OTP login between sending the
// code and validating it.
type recoverySession struct {
	worker    *registerWorker
	flow      *registerFlow
	email     string
	createdAt time.Time
}

// AccountRecovery runs persistent background recovery jobs: per item it
// tries a session refresh, a password relogin, then an email-OTP login with
// automatic code retrieval, and parks the item in "awaiting_code" when the
// code has to be typed in by the admin.
type AccountRecovery struct {
	mu       sync.Mutex
	accounts *AccountService
	register *RegisterService
	store    storage.JSONDocumentBackend
	jobs     []*RecoveryJob
	settings RecoverySettings
	heal     map[string]*recoveryHealState
	running  map[string]context.CancelFunc
	pending  map[string]*recoverySession
	wake     chan struct{}

	httpClient    *http.Client  // outlookEmail client override (tests)
	fetchInterval time.Duration // mail poll interval override (tests)
	mailConfigFn  func() map[string]any

	// pipeline hooks (overridable in tests)
	refreshFn  func(ctx context.Context, token string) (string, error)
	reloginFn  func(ctx context.Context, token string, creds map[string]any) (string, string, error)
	startOTPFn func(ctx context.Context, email string) (*recoverySession, error)
	fetchFn    func(ctx context.Context, email string, creds map[string]any, since time.Time, seen map[string]struct{}, wait time.Duration) (string, error)
	// newProvider builds a mail provider for a config entry (tests swap the HTTP client).
	newProvider func(mailConfig map[string]any, providerName, providerRef string) (registerMailboxProvider, error)
	validateFn  func(session *recoverySession, code string) (string, string, error)
}

// NewAccountRecovery wires the recovery queue against the pool and the
// register service (mail configuration and worker construction).
func NewAccountRecovery(accounts *AccountService, register *RegisterService, backend storage.Backend) *AccountRecovery {
	r := &AccountRecovery{
		accounts: accounts,
		register: register,
		settings: defaultRecoverySettings(),
		heal:     map[string]*recoveryHealState{},
		running:  map[string]context.CancelFunc{},
		pending:  map[string]*recoverySession{},
		wake:     make(chan struct{}, 1),
	}
	if store, ok := backend.(storage.JSONDocumentBackend); ok {
		r.store = store
	}
	r.refreshFn = r.defaultRefresh
	r.reloginFn = r.defaultRelogin
	r.startOTPFn = r.defaultStartOTP
	r.fetchFn = r.defaultFetchCode
	r.newProvider = createRegisterMailProvider
	r.validateFn = r.defaultValidate
	r.load()
	return r
}

func (r *AccountRecovery) load() {
	if r.store == nil {
		return
	}
	raw, err := r.store.LoadJSONDocument(recoveryJobsDocumentName)
	if err != nil || raw == nil {
		return
	}
	var doc struct {
		Jobs     []*RecoveryJob                `json:"jobs"`
		Settings *RecoverySettings             `json:"settings"`
		Heal     map[string]*recoveryHealState `json:"heal"`
	}
	if err := decodeInto(raw, &doc); err != nil {
		return
	}
	if doc.Settings != nil {
		r.settings = openRecoverySettings(*doc.Settings).normalized()
	}
	if doc.Heal != nil {
		r.heal = doc.Heal
	}
	for _, job := range doc.Jobs {
		if job == nil {
			continue
		}
		for _, item := range job.Items {
			switch item.Status {
			case RecoveryRunning:
				// Interrupted by a restart: run it again.
				item.Status = RecoveryQueued
				item.Stage = "服务重启后重新排队"
			case RecoveryAwaitingCode:
				// The login session lived in memory and is gone.
				item.Status = RecoveryFailed
				item.Error = "服务重启，登录会话已失效，请重试"
				item.Stage = ""
			}
		}
		r.jobs = append(r.jobs, job)
	}
}

func (r *AccountRecovery) saveLocked() {
	if r.store == nil {
		return
	}
	// Keep the newest 50 jobs.
	if len(r.jobs) > 50 {
		r.jobs = r.jobs[len(r.jobs)-50:]
	}
	_ = r.store.SaveJSONDocument(recoveryJobsDocumentName, map[string]any{
		"jobs":     r.jobs,
		"settings": sealRecoverySettings(r.settings),
		"heal":     r.heal,
	})
}

// Start launches the dispatcher and the auto self-heal loop.
func (r *AccountRecovery) Start(ctx context.Context) {
	go r.dispatchLoop(ctx)
	go r.autoHealLoop(ctx)
	r.signal()
}

func (r *AccountRecovery) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Settings returns the current recovery settings.
func (r *AccountRecovery) Settings() RecoverySettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.settings
}

// UpdateSettings merges the given fields into the settings. Invalid
// settings (e.g. one suffix on two Cloudflare entries) are rejected and the
// current settings are kept.
func (r *AccountRecovery) UpdateSettings(body map[string]any) (RecoverySettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := map[string]any{}
	_ = decodeInto(r.settings, &current)
	for k, v := range body {
		current[k] = v
	}
	next := r.settings
	next.CloudflareMails = nil
	if err := decodeInto(current, &next); err != nil {
		return r.settings, err
	}
	next = next.normalized()
	if err := validateRecoveryCFMails(next.CloudflareMails); err != nil {
		return r.settings, err
	}
	r.settings = next
	r.saveLocked()
	r.signal()
	return r.settings, nil
}

// CreateJob queues a job for the given emails (deduplicated).
func (r *AccountRecovery) CreateJob(name string, emails []string, concurrency int, source string) (*RecoveryJob, error) {
	seen := map[string]struct{}{}
	items := []*RecoveryItem{}
	now := util.NowLocal()
	for _, raw := range emails {
		for _, email := range strings.FieldsFunc(raw, func(c rune) bool { return c == ',' || c == '\n' || c == ' ' || c == ';' || c == '\t' }) {
			email = strings.ToLower(strings.TrimSpace(email))
			if email == "" || !strings.Contains(email, "@") {
				continue
			}
			if _, ok := seen[email]; ok {
				continue
			}
			seen[email] = struct{}{}
			items = append(items, &RecoveryItem{ID: util.NewHex(8), Email: email, Status: RecoveryQueued, Stage: "排队中", UpdatedAt: now, Steps: []string{}})
		}
	}
	if len(items) == 0 {
		return nil, errors.New("没有有效的邮箱")
	}
	r.mu.Lock()
	if concurrency <= 0 {
		concurrency = r.settings.DefaultConcurrency
	}
	if concurrency > 20 {
		concurrency = 20
	}
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("找回任务 %s", now)
	}
	if source == "" {
		source = "manual"
	}
	job := &RecoveryJob{ID: util.NewHex(10), Name: name, Source: source, Concurrency: concurrency, CreatedAt: now, UpdatedAt: now, Items: items}
	r.jobs = append(r.jobs, job)
	r.saveLocked()
	out := r.copyJobLocked(job)
	r.mu.Unlock()
	r.signal()
	return out, nil
}

// EmailsForAccountIDs resolves pool account ids to their emails.
func (r *AccountRecovery) EmailsForAccountIDs(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		token := r.accounts.GetTokenByID(id)
		if token == "" {
			continue
		}
		if account := r.accounts.GetAccount(token); account != nil {
			if email := util.Clean(account["email"]); email != "" {
				out = append(out, email)
			}
		}
	}
	return out
}

func (r *AccountRecovery) copyJobLocked(job *RecoveryJob) *RecoveryJob {
	copied := *job
	copied.Items = make([]*RecoveryItem, 0, len(job.Items))
	for _, item := range job.Items {
		c := *item
		c.Steps = append([]string{}, item.Steps...)
		copied.Items = append(copied.Items, &c)
	}
	return &copied
}

// ListJobs returns all jobs newest first with per-status counts.
func (r *AccountRecovery) ListJobs() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, 0, len(r.jobs))
	for i := len(r.jobs) - 1; i >= 0; i-- {
		out = append(out, r.publicJobLocked(r.jobs[i]))
	}
	return out
}

// GetJob returns one job.
func (r *AccountRecovery) GetJob(id string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job := r.findJobLocked(id); job != nil {
		return r.publicJobLocked(job)
	}
	return nil
}

func (r *AccountRecovery) publicJobLocked(job *RecoveryJob) map[string]any {
	counts := map[string]int{}
	for _, item := range job.Items {
		counts[item.Status]++
	}
	status := "done"
	if counts[RecoveryQueued]+counts[RecoveryRunning] > 0 {
		status = "running"
	} else if counts[RecoveryAwaitingCode] > 0 {
		status = "awaiting_code"
	}
	out := map[string]any{}
	_ = decodeInto(r.copyJobLocked(job), &out)
	out["counts"] = counts
	out["total"] = len(job.Items)
	out["status"] = status
	return out
}

func (r *AccountRecovery) findJobLocked(id string) *RecoveryJob {
	for _, job := range r.jobs {
		if job.ID == id {
			return job
		}
	}
	return nil
}

func (r *AccountRecovery) findItemLocked(jobID, itemID string) (*RecoveryJob, *RecoveryItem) {
	job := r.findJobLocked(jobID)
	if job == nil {
		return nil, nil
	}
	for _, item := range job.Items {
		if item.ID == itemID {
			return job, item
		}
	}
	return job, nil
}

func itemMatches(item *RecoveryItem, itemIDs map[string]struct{}) bool {
	if len(itemIDs) == 0 {
		return true
	}
	_, ok := itemIDs[item.ID]
	return ok
}

func idSet(ids []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

// Retry re-queues failed/cancelled items (all, or the given ids).
func (r *AccountRecovery) Retry(jobID string, itemIDs []string) (int, error) {
	r.mu.Lock()
	job := r.findJobLocked(jobID)
	if job == nil {
		r.mu.Unlock()
		return 0, errors.New("job not found")
	}
	set := idSet(itemIDs)
	n := 0
	for _, item := range job.Items {
		if !itemMatches(item, set) {
			continue
		}
		if item.Status == RecoveryFailed || item.Status == RecoveryCancelled || (len(set) > 0 && item.Status == RecoveryAwaitingCode) {
			r.dropSessionLocked(item.ID)
			item.Status = RecoveryQueued
			item.Stage = "排队中"
			item.Error = ""
			item.FinishedAt = ""
			item.UpdatedAt = util.NowLocal()
			n++
		}
	}
	job.UpdatedAt = util.NowLocal()
	r.saveLocked()
	r.mu.Unlock()
	r.signal()
	return n, nil
}

// Cancel stops queued/running/awaiting items (all, or the given ids).
func (r *AccountRecovery) Cancel(jobID string, itemIDs []string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.findJobLocked(jobID)
	if job == nil {
		return 0, errors.New("job not found")
	}
	set := idSet(itemIDs)
	n := 0
	for _, item := range job.Items {
		if !itemMatches(item, set) {
			continue
		}
		switch item.Status {
		case RecoveryQueued, RecoveryRunning, RecoveryAwaitingCode:
			if cancel := r.running[item.ID]; cancel != nil {
				cancel()
			}
			r.dropSessionLocked(item.ID)
			item.Status = RecoveryCancelled
			item.Stage = ""
			item.FinishedAt = util.NowLocal()
			item.UpdatedAt = item.FinishedAt
			n++
		}
	}
	r.saveLocked()
	return n, nil
}

// DeleteJob removes a job, cancelling anything still active.
func (r *AccountRecovery) DeleteJob(jobID string) bool {
	_, _ = r.Cancel(jobID, nil)
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, job := range r.jobs {
		if job.ID == jobID {
			r.jobs = append(r.jobs[:i], r.jobs[i+1:]...)
			r.saveLocked()
			return true
		}
	}
	return false
}

// SubmitCodes validates manually entered codes for awaiting items. codes maps
// item id (or email) to code.
func (r *AccountRecovery) SubmitCodes(jobID string, codes map[string]string) (int, map[string]string) {
	errs := map[string]string{}
	accepted := 0
	r.mu.Lock()
	job := r.findJobLocked(jobID)
	if job == nil {
		r.mu.Unlock()
		return 0, map[string]string{"job": "job not found"}
	}
	type submission struct {
		item    *RecoveryItem
		session *recoverySession
		code    string
	}
	var subs []submission
	for key, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		var target *RecoveryItem
		for _, item := range job.Items {
			if item.ID == key || strings.EqualFold(item.Email, key) {
				target = item
				break
			}
		}
		if target == nil {
			errs[key] = "条目不存在"
			continue
		}
		if target.Status != RecoveryAwaitingCode {
			errs[key] = "该条目不在待填码状态"
			continue
		}
		session := r.pending[target.ID]
		if session == nil {
			errs[key] = "登录会话已失效，请重试"
			continue
		}
		target.Status = RecoveryRunning
		target.Stage = "校验验证码"
		target.UpdatedAt = util.NowLocal()
		subs = append(subs, submission{item: target, session: session, code: code})
		accepted++
	}
	r.saveLocked()
	r.mu.Unlock()
	for _, sub := range subs {
		go r.finishOTP(jobID, sub.item.ID, sub.session, sub.code)
	}
	return accepted, errs
}

func (r *AccountRecovery) dropSessionLocked(itemID string) {
	if session := r.pending[itemID]; session != nil {
		delete(r.pending, itemID)
		if session.worker != nil {
			session.worker.close()
		}
	}
}

func (r *AccountRecovery) update(jobID, itemID string, fn func(item *RecoveryItem)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, item := r.findItemLocked(jobID, itemID)
	if item == nil {
		return false
	}
	if item.Status == RecoveryCancelled {
		return false
	}
	fn(item)
	item.UpdatedAt = util.NowLocal()
	job.UpdatedAt = item.UpdatedAt
	r.saveLocked()
	return true
}

func (r *AccountRecovery) stage(jobID, itemID, stage string) {
	r.update(jobID, itemID, func(item *RecoveryItem) {
		item.Stage = stage
		item.Steps = append(item.Steps, time.Now().Format("15:04:05")+" "+stage)
		if len(item.Steps) > 20 {
			item.Steps = item.Steps[len(item.Steps)-20:]
		}
	})
}

func (r *AccountRecovery) dispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		r.expireAwaiting()
		r.dispatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-ticker.C:
		}
	}
}

func (r *AccountRecovery) dispatch(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	global := r.settings.GlobalConcurrency
	active := len(r.running)
	for _, job := range r.jobs {
		limit := job.Concurrency
		if job.Source == "auto" {
			limit = r.settings.AutoConcurrency
		}
		if limit <= 0 {
			limit = r.settings.DefaultConcurrency
		}
		jobRunning := 0
		for _, item := range job.Items {
			if _, ok := r.running[item.ID]; ok {
				jobRunning++
			}
		}
		for _, item := range job.Items {
			if active >= global {
				return
			}
			if jobRunning >= limit {
				break
			}
			if item.Status != RecoveryQueued {
				continue
			}
			itemCtx, cancel := context.WithCancel(ctx)
			r.running[item.ID] = cancel
			item.Status = RecoveryRunning
			item.Stage = "开始处理"
			item.Attempts++
			item.UpdatedAt = util.NowLocal()
			active++
			jobRunning++
			go r.runItem(itemCtx, job.ID, item.ID, item.Email)
		}
	}
	r.saveLocked()
}

func (r *AccountRecovery) releaseRunning(itemID string) {
	r.mu.Lock()
	if cancel := r.running[itemID]; cancel != nil {
		cancel()
	}
	delete(r.running, itemID)
	r.mu.Unlock()
	r.signal()
}

func (r *AccountRecovery) succeed(jobID, itemID, email, method, token string) {
	r.update(jobID, itemID, func(item *RecoveryItem) {
		item.Status = RecoverySucceeded
		item.Stage = "完成"
		item.Method = method
		item.Error = ""
		item.TokenPreview = util.AnonymizeToken(token)
		item.FinishedAt = util.NowLocal()
	})
	r.recordHeal(email, true)
}

func (r *AccountRecovery) fail(jobID, itemID, email, reason string) {
	r.update(jobID, itemID, func(item *RecoveryItem) {
		item.Status = RecoveryFailed
		item.Stage = ""
		item.Error = reason
		item.FinishedAt = util.NowLocal()
	})
	if token := r.accounts.AccountTokenByEmail(email); token != "" {
		r.accounts.RecordAccountError(token, "找回失败: "+reason)
	}
	r.recordHeal(email, false)
}

// runItem executes the per-account pipeline.
func (r *AccountRecovery) runItem(ctx context.Context, jobID, itemID, email string) {
	defer r.releaseRunning(itemID)
	var reasons []string
	token := r.accounts.AccountTokenByEmail(email)
	var creds map[string]any
	if token != "" {
		creds = r.accounts.AccountCredentials(token)
	}
	if creds == nil {
		creds = map[string]any{"email": email}
	}
	// 1. Session refresh.
	if token != "" && util.Clean(creds["session_token"]) != "" {
		r.stage(jobID, itemID, "刷新 Token")
		if newToken, err := r.refreshFn(ctx, token); err == nil && newToken != "" {
			r.succeed(jobID, itemID, email, "refresh", newToken)
			return
		} else if err != nil {
			reasons = append(reasons, "刷新失败: "+err.Error())
		}
	}
	if ctx.Err() != nil {
		return
	}
	// 2. Password relogin.
	if util.Clean(creds["password"]) != "" {
		r.stage(jobID, itemID, "密码登录")
		at, st, err := r.reloginFn(ctx, token, creds)
		if err == nil && at != "" {
			final := r.ingest(token, email, at, st)
			r.succeed(jobID, itemID, email, "password", final)
			return
		}
		if err != nil {
			reasons = append(reasons, "密码登录失败: "+err.Error())
		}
	}
	if ctx.Err() != nil {
		return
	}
	// 3. Email OTP login through the routed mail channel only.
	route := r.routeFor(email, creds)
	r.update(jobID, itemID, func(item *RecoveryItem) { item.Channel = route.Channel })
	seen := map[string]struct{}{}
	if route.Ready {
		seen = r.snapshotSeenMail(email, creds)
	}
	since := time.Now()
	r.stage(jobID, itemID, "发送邮箱验证码")
	session, err := r.startOTPFn(ctx, email)
	if err != nil {
		reasons = append(reasons, "验证码登录失败: "+err.Error())
		r.fail(jobID, itemID, email, strings.Join(reasons, "；"))
		return
	}
	var fetchErr error
	if route.Ready {
		r.stage(jobID, itemID, "自动收取验证码 ["+route.Channel+"]")
		wait := time.Duration(r.Settings().OTPWaitSeconds) * time.Second
		code, err := r.fetchFn(ctx, email, creds, since, seen, wait)
		if ctx.Err() != nil {
			closeSession(session)
			return
		}
		if code != "" {
			r.finishOTP(jobID, itemID, session, code)
			return
		}
		fetchErr = err
	} else {
		fetchErr = errors.New(route.Reason)
	}
	r.mu.Lock()
	r.pending[itemID] = session
	r.mu.Unlock()
	note := "验证码已发送，请手动填写"
	if fetchErr != nil {
		note += "（自动收码失败: " + fetchErr.Error() + "）"
	}
	r.update(jobID, itemID, func(item *RecoveryItem) {
		item.Status = RecoveryAwaitingCode
		item.Stage = "待填码"
		item.Error = strings.Join(append(reasons, note), "；")
	})
}

// expireAwaiting fails awaiting items whose login session is older than
// the OTP validity window and releases their workers.
func (r *AccountRecovery) expireAwaiting() {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, job := range r.jobs {
		for _, item := range job.Items {
			if item.Status != RecoveryAwaitingCode {
				continue
			}
			session := r.pending[item.ID]
			if session != nil && time.Since(session.createdAt) < recoverySessionTTL {
				continue
			}
			r.dropSessionLocked(item.ID)
			item.Status = RecoveryFailed
			item.Stage = ""
			item.Error = "等待填码超时，登录会话已失效，请重试"
			item.FinishedAt = util.NowLocal()
			changed = true
		}
	}
	if changed {
		r.saveLocked()
	}
}

const recoverySessionTTL = 20 * time.Minute

func decodeJSONBody(body io.Reader, dst any) error {
	data, err := io.ReadAll(io.LimitReader(body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func closeSession(session *recoverySession) {
	if session != nil && session.worker != nil {
		session.worker.close()
	}
}

func (r *AccountRecovery) finishOTP(jobID, itemID string, session *recoverySession, code string) {
	r.mu.Lock()
	delete(r.pending, itemID)
	r.mu.Unlock()
	defer closeSession(session)
	r.stage(jobID, itemID, "校验验证码")
	at, st, err := r.validateFn(session, code)
	if err != nil {
		r.fail(jobID, itemID, session.email, "验证码登录失败: "+err.Error())
		return
	}
	token := r.accounts.AccountTokenByEmail(session.email)
	final := r.ingest(token, session.email, at, st)
	r.succeed(jobID, itemID, session.email, "email_otp", final)
}

// ingest rotates renewed tokens into the pool.
func (r *AccountRecovery) ingest(oldToken, email, accessToken, sessionToken string) string {
	if oldToken != "" && r.accounts.RefreshAccountViaSession(oldToken, accessToken, sessionToken, "") {
		r.accounts.UpdateAccount(accessToken, map[string]any{"relogin_failures": 0, "last_error": nil})
		return accessToken
	}
	return r.accounts.AddRecoveredAccount(map[string]any{"access_token": accessToken, "session_token": sessionToken, "email": email})
}

func (r *AccountRecovery) defaultRefresh(ctx context.Context, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return r.accounts.RefreshAccountNow(ctx, token)
}

func (r *AccountRecovery) defaultRelogin(ctx context.Context, token string, creds map[string]any) (string, string, error) {
	if r.register == nil {
		return "", "", errors.New("注册服务未就绪")
	}
	return r.register.reloginAccount(token, creds)
}

func (r *AccountRecovery) mailConfig() map[string]any {
	if r.mailConfigFn != nil {
		return r.mailConfigFn()
	}
	if r.register == nil {
		return map[string]any{}
	}
	return util.StringMap(cloneMap(r.register.Get())["mail"])
}

// defaultStartOTP opens the passwordless email-OTP login for an existing
// account and leaves the flow waiting for the code.
func (r *AccountRecovery) defaultStartOTP(ctx context.Context, email string) (*recoverySession, error) {
	if r.register == nil {
		return nil, errors.New("注册服务未就绪")
	}
	config := cloneMap(r.register.Get())
	config["mail"] = cloneMap(util.StringMap(config["mail"]))
	worker, err := newRegisterWorker(r.register, 0, config)
	if err != nil {
		return nil, fmt.Errorf("创建登录会话失败: %w", err)
	}
	flow := newRegisterFlow(worker, ctx)
	flow.email = email
	if err := worker.prewarmCloudflare(ctx); err != nil {
		worker.step("找回登录 Cloudflare 预热失败（继续尝试）: " + err.Error())
	}
	fail := func(err error) (*recoverySession, error) {
		worker.close()
		return nil, err
	}
	if !flow.warmup() {
		return fail(errors.New("warmup 失败：chatgpt.com 未种到 oai-did cookie"))
	}
	csrf, err := flow.getCSRFToken()
	if err != nil {
		return fail(fmt.Errorf("获取 CSRF 失败: %w", err))
	}
	authURL, err := flow.getAuthURL(csrf)
	if err != nil {
		return fail(fmt.Errorf("获取 Auth URL 失败: %w", err))
	}
	if _, err := flow.authOAuthInit(authURL); err != nil {
		return fail(fmt.Errorf("authorize 初始化失败: %w", err))
	}
	var isNew bool
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fail(ctx.Err())
			case <-time.After(time.Duration(15*attempt) * time.Second):
			}
		}
		isNew, lastErr = flow.signup(email)
		if lastErr == nil {
			break
		}
		if !strings.Contains(lastErr.Error(), "HTTP 429") && !strings.Contains(lastErr.Error(), "HTTP 409") {
			break
		}
	}
	if lastErr != nil {
		return fail(fmt.Errorf("提交邮箱失败: %w", lastErr))
	}
	if isNew {
		return fail(errors.New("该邮箱未注册过 ChatGPT 账号（上游返回了注册页）"))
	}
	pageType := flow.existingPageType
	mode := strings.ToLower(flow.existingVerificationMode)
	if pageType == "login_password" || strings.Contains(pageType, "password") {
		if !flow.sendPasswordlessOTP(registerAuthBase + "/log-in/password") {
			return fail(errors.New("切换邮箱验证码登录失败（passwordless/send-otp）"))
		}
	} else if mode != "passwordless_login" && mode != "passwordless_signup" {
		if err := flow.sendOTP(registerAuthBase + "/email-verification"); err != nil {
			if !flow.resendOTP(registerAuthBase + "/email-verification") {
				return fail(fmt.Errorf("发送验证码失败: %w", err))
			}
		}
	}
	return &recoverySession{worker: worker, flow: flow, email: email, createdAt: time.Now()}, nil
}

func (r *AccountRecovery) defaultValidate(session *recoverySession, code string) (string, string, error) {
	if session == nil || session.flow == nil {
		return "", "", errors.New("登录会话已失效")
	}
	otpPayload, err := session.flow.validateOTP(code)
	if err != nil {
		return "", "", fmt.Errorf("验证码校验失败: %w", err)
	}
	continueURL := util.Clean(otpPayload["continue_url"])
	if continueURL == "" {
		continueURL = session.flow.reauthorizeForSession()
	}
	callbackURL := ""
	if continueURL != "" {
		callbackURL, _ = session.flow.followRedirectChain(continueURL)
	}
	if callbackURL != "" {
		session.flow.consumeCallback(callbackURL)
	}
	sessionToken, accessToken := session.flow.getAuthSession()
	if accessToken == "" && callbackURL == "" && continueURL != "" {
		if retry := session.flow.reauthorizeForSession(); retry != "" {
			if cb, _ := session.flow.followRedirectChain(retry); cb != "" {
				session.flow.consumeCallback(cb)
			}
			sessionToken, accessToken = session.flow.getAuthSession()
		}
	}
	if accessToken == "" {
		return "", "", errors.New("登录完成但未获取有效 access_token")
	}
	return accessToken, sessionToken, nil
}

// ── auto self-heal ──────────────────────────────────────────────────────────

func (r *AccountRecovery) recordHeal(email string, success bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.heal[email]
	if state == nil {
		return
	}
	if success {
		state.Failures = 0
		state.NextAt = ""
	} else {
		state.Failures++
		backoff := time.Duration(r.settings.AutoCooldownMinutes) * time.Minute
		for i := 1; i < state.Failures && backoff < 24*time.Hour; i++ {
			backoff *= 2
		}
		if backoff > 24*time.Hour {
			backoff = 24 * time.Hour
		}
		state.NextAt = time.Now().Add(backoff).UTC().Format(time.RFC3339)
	}
	r.saveLocked()
}

func (r *AccountRecovery) autoHealLoop(ctx context.Context) {
	last := time.Time{}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		settings := r.Settings()
		if !settings.AutoEnabled || time.Since(last) < time.Duration(settings.AutoIntervalMinutes)*time.Minute {
			continue
		}
		last = time.Now()
		r.RunAutoHeal()
	}
}

// RunAutoHeal enqueues eligible broken accounts into the auto job. It returns
// the number of accounts queued.
func (r *AccountRecovery) RunAutoHeal() int {
	settings := r.Settings()
	candidates := []string{}
	for _, account := range r.accounts.ExportAccounts() {
		status := util.Clean(account["status"])
		if status != "异常" && !(settings.AutoIncludeExpired && status == "过期待刷新") {
			continue
		}
		if !util.ToBool(account["enabled"]) {
			continue
		}
		email := strings.ToLower(util.Clean(account["email"]))
		if email == "" {
			continue
		}
		if settings.AutoSkipWithoutCreds && util.Clean(account["password"]) == "" && util.Clean(account["session_token"]) == "" &&
			!r.hasMailSource(email, account) {
			continue
		}
		candidates = append(candidates, email)
	}
	if len(candidates) == 0 {
		return 0
	}
	now := time.Now()
	day := now.In(apiKeyUsageZone).Format("2006-01-02")
	r.mu.Lock()
	active := map[string]struct{}{}
	for _, job := range r.jobs {
		for _, item := range job.Items {
			if item.Status == RecoveryQueued || item.Status == RecoveryRunning || item.Status == RecoveryAwaitingCode {
				active[item.Email] = struct{}{}
			}
		}
	}
	var job *RecoveryJob
	for i := len(r.jobs) - 1; i >= 0; i-- {
		if r.jobs[i].Source == "auto" && strings.HasSuffix(r.jobs[i].Name, day) {
			job = r.jobs[i]
			break
		}
	}
	queued := 0
	for _, email := range candidates {
		if _, ok := active[email]; ok {
			continue
		}
		state := r.heal[email]
		if state == nil {
			state = &recoveryHealState{}
			r.heal[email] = state
		}
		if state.Day != day {
			state.Day = day
			state.Attempts = 0
		}
		if state.Attempts >= settings.AutoDailyAttemptCap {
			continue
		}
		if state.NextAt != "" {
			if next, err := time.Parse(time.RFC3339, state.NextAt); err == nil && now.Before(next) {
				continue
			}
		}
		state.Attempts++
		if job == nil {
			job = &RecoveryJob{ID: util.NewHex(10), Name: "自动自愈 " + day, Source: "auto", Concurrency: settings.AutoConcurrency, CreatedAt: util.NowLocal(), Items: []*RecoveryItem{}}
			r.jobs = append(r.jobs, job)
		}
		job.Items = append(job.Items, &RecoveryItem{ID: util.NewHex(8), Email: email, Status: RecoveryQueued, Stage: "排队中", UpdatedAt: util.NowLocal(), Steps: []string{}})
		job.UpdatedAt = util.NowLocal()
		queued++
	}
	r.saveLocked()
	r.mu.Unlock()
	if queued > 0 {
		r.signal()
	}
	return queued
}

// Summary returns counts across all jobs for the dashboard.
func (r *AccountRecovery) Summary() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := map[string]int{}
	for _, job := range r.jobs {
		for _, item := range job.Items {
			counts[item.Status]++
		}
	}
	return map[string]any{"jobs": len(r.jobs), "counts": counts, "auto_enabled": r.settings.AutoEnabled}
}

// decodeInto converts between JSON-compatible representations.
func decodeInto(src any, dst any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
