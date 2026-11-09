package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"chatgpt2api/internal/util"
)

// RecoveryCFMail is a cloudflare_temp_email (dreamhunter2333/cloudflare_temp_email)
// deployment used only by recovery. Each entry owns a set of address
// suffixes; an address whose domain matches is read through this entry's
// admin API (GET /admin/mails, header x-admin-auth) or, without an admin
// password, through the account's mailbox JWT (GET /api/mails).
type RecoveryCFMail struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	BaseURL       string   `json:"base_url"`
	AdminPassword string   `json:"admin_password"`
	Domains       []string `json:"domains"`
	Enabled       bool     `json:"enabled"`
}

// recoveryCFRefPrefix marks MailRoute.EntryRef values that point at a
// recovery-owned CF entry (as opposed to a register provider_ref).
const recoveryCFRefPrefix = "recovery_cf:"

// Route sources reported in MailRoute.Source.
const (
	MailRouteSourceRecovery = "recovery"
	MailRouteSourceRegister = "register"
)

const legacyCFRouteReason = "沿用注册机邮箱配置"

func newRecoveryCFID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// normalizeCFDomain lowercases a suffix and strips "@" and dots at the edges.
// A leading "*." wildcard is kept.
func normalizeCFDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimLeft(domain, "@")
	wildcard := strings.HasPrefix(domain, "*.")
	domain = strings.Trim(strings.TrimPrefix(domain, "*."), ".")
	if domain == "" {
		return ""
	}
	if wildcard {
		return "*." + domain
	}
	return domain
}

func normalizeRecoveryCFMails(entries []RecoveryCFMail) []RecoveryCFMail {
	out := make([]RecoveryCFMail, 0, len(entries))
	for _, e := range entries {
		e.ID = strings.TrimSpace(e.ID)
		if e.ID == "" {
			e.ID = newRecoveryCFID()
		}
		e.Name = strings.TrimSpace(e.Name)
		e.BaseURL = strings.TrimRight(strings.TrimSpace(e.BaseURL), "/")
		e.AdminPassword = strings.TrimSpace(e.AdminPassword)
		seen := map[string]struct{}{}
		domains := []string{}
		for _, raw := range e.Domains {
			for _, part := range strings.FieldsFunc(raw, func(c rune) bool { return c == ',' || c == '\n' || c == ' ' || c == ';' || c == '\t' }) {
				if d := normalizeCFDomain(part); d != "" {
					if _, dup := seen[d]; !dup {
						seen[d] = struct{}{}
						domains = append(domains, d)
					}
				}
			}
		}
		e.Domains = domains
		if e.Name == "" && len(domains) > 0 {
			e.Name = domains[0]
		}
		out = append(out, e)
	}
	return out
}

// validateRecoveryCFMails rejects entries that would make routing ambiguous:
// the same suffix on two entries, duplicate ids, or an enabled entry with no
// base URL.
func validateRecoveryCFMails(entries []RecoveryCFMail) error {
	owner := map[string]string{}
	ids := map[string]struct{}{}
	for i, e := range entries {
		label := e.Name
		if label == "" {
			label = fmt.Sprintf("#%d", i+1)
		}
		if _, dup := ids[e.ID]; dup {
			return fmt.Errorf("Cloudflare 临时邮箱 id 重复：%s", e.ID)
		}
		ids[e.ID] = struct{}{}
		if e.Enabled && e.BaseURL == "" {
			return fmt.Errorf("Cloudflare 临时邮箱「%s」未填写 Base URL", label)
		}
		if e.BaseURL != "" && !strings.HasPrefix(e.BaseURL, "http://") && !strings.HasPrefix(e.BaseURL, "https://") {
			return fmt.Errorf("Cloudflare 临时邮箱「%s」的 Base URL 需以 http:// 或 https:// 开头", label)
		}
		for _, d := range e.Domains {
			if prev, dup := owner[d]; dup {
				return fmt.Errorf("域名 %s 同时配置在「%s」和「%s」，同一后缀只能属于一个 Cloudflare 临时邮箱", d, prev, label)
			}
			owner[d] = label
		}
	}
	return nil
}

// cfDomainMatch reports whether an address domain matches a configured
// suffix: exact, or "*.example.com" for any subdomain of example.com.
func cfDomainMatch(pattern, domain string) bool {
	if strings.HasPrefix(pattern, "*.") {
		base := strings.TrimPrefix(pattern, "*.")
		return strings.HasSuffix(domain, "."+base)
	}
	return pattern == domain
}

// findRecoveryCF picks the enabled entry covering domain. Exact matches win
// over wildcard matches; among wildcards the longest suffix wins.
func findRecoveryCF(entries []RecoveryCFMail, domain string) *RecoveryCFMail {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil
	}
	var best *RecoveryCFMail
	bestLen := -1
	for i := range entries {
		e := &entries[i]
		if !e.Enabled {
			continue
		}
		for _, d := range e.Domains {
			if !cfDomainMatch(d, domain) {
				continue
			}
			score := len(d)
			if !strings.HasPrefix(d, "*.") {
				score += 1 << 20
			}
			if score > bestLen {
				best, bestLen = e, score
			}
		}
	}
	return best
}

func findRecoveryCFByRef(entries []RecoveryCFMail, ref string) *RecoveryCFMail {
	id := strings.TrimPrefix(ref, recoveryCFRefPrefix)
	if id == "" || id == ref {
		return nil
	}
	for i := range entries {
		if entries[i].Enabled && entries[i].ID == id {
			return &entries[i]
		}
	}
	return nil
}

func applyRecoveryCFRoute(route *MailRoute, e *RecoveryCFMail) {
	route.Source = MailRouteSourceRecovery
	route.EntryRef = recoveryCFRefPrefix + e.ID
	route.EntryName = e.Name
	route.entry = map[string]any{"api_base": e.BaseURL, "admin_password": e.AdminPassword, "name": e.Name}
	route.Ready = true
}

// fetchRecoveryCFMessages reads the newest message for an address from a
// recovery-owned CF entry: admin API first, mailbox JWT as the fallback.
func fetchRecoveryCFMessages(entry map[string]any, email, token string) ([]recoveryMailMessage, error) {
	var errs []string
	if util.Clean(entry["admin_password"]) != "" {
		message, err := fetchAdminMailMessage(entry, email)
		if err == nil {
			if message == nil {
				return nil, nil
			}
			return []recoveryMailMessage{normalizeRecoveryMessage(message)}, nil
		}
		errs = append(errs, "admin API 读取失败: "+err.Error())
	}
	if token != "" {
		message, err := fetchCFJWTMessage(util.Clean(entry["api_base"]), email, token)
		if err == nil {
			if message == nil {
				return nil, nil
			}
			return []recoveryMailMessage{normalizeRecoveryMessage(message)}, nil
		}
		errs = append(errs, "邮箱 JWT 读取失败: "+err.Error())
	}
	if len(errs) == 0 {
		errs = append(errs, "该 Cloudflare 临时邮箱未配置 admin 密码，且账号没有 mail_token")
	}
	return nil, errors.New(strings.Join(errs, "；"))
}

func fetchCFJWTMessage(apiBase, email, token string) (map[string]any, error) {
	client := registerMailHTTPClient(15 * time.Second)
	defer client.CloseIdleConnections()
	data, err := registerMailRequestJSON(client, http.MethodGet, strings.TrimRight(apiBase, "/")+"/api/mails", map[string]string{
		"Authorization": "Bearer " + token,
		"User-Agent":    "Mozilla/5.0",
		"Accept":        "application/json",
	}, map[string]string{"limit": "10", "offset": "0"}, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, item := range util.AsMapSlice(data["results"]) {
		if registerMessageMatchesEmail(item, email) {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	latest := latestRegisterMailMessage(items)
	return map[string]any{
		"provider":     MailChannelCloudflare,
		"mailbox":      email,
		"message_id":   util.Clean(latest["id"]),
		"subject":      util.Clean(latest["subject"]),
		"text_content": registerContentString(latest["text"]) + registerContentString(latest["content"]),
		"html_content": registerContentString(latest["html"]) + registerContentString(latest["message"]),
		"raw":          util.Clean(latest["raw"]),
		"received_at":  firstNonNil(latest["created_at"], latest["createdAt"], latest["date"]),
	}, nil
}

// ProbeRecoveryCFMail performs one authenticated admin request against the
// entry (by id; a non-empty override replaces stored fields, masked
// passwords resolve to the stored one) and reports how many mails it sees.
func (r *AccountRecovery) ProbeRecoveryCFMail(id string, override *RecoveryCFMail) (map[string]any, error) {
	var entry RecoveryCFMail
	for _, e := range r.Settings().CloudflareMails {
		if e.ID == id {
			entry = e
		}
	}
	if override != nil {
		stored := entry.AdminPassword
		entry.BaseURL = strings.TrimRight(strings.TrimSpace(override.BaseURL), "/")
		entry.AdminPassword = UnmaskSecret(strings.TrimSpace(override.AdminPassword), stored)
	}
	if entry.BaseURL == "" || entry.AdminPassword == "" {
		return nil, errors.New("需要 Base URL 和 admin 密码")
	}
	client := registerMailHTTPClient(15 * time.Second)
	defer client.CloseIdleConnections()
	data, err := registerMailRequestJSON(client, http.MethodGet, entry.BaseURL+"/admin/mails", map[string]string{
		"x-admin-auth": entry.AdminPassword,
		"User-Agent":   "Mozilla/5.0",
		"Accept":       "application/json",
	}, map[string]string{"limit": "1", "offset": "0"}, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "count": util.ToInt(data["count"], len(util.AsMapSlice(data["results"])))}, nil
}

// sealRecoverySettings returns a copy with secrets sealed for storage.
func sealRecoverySettings(s RecoverySettings) RecoverySettings {
	if v, err := EncryptSecret(s.OutlookAPIKey); err == nil {
		s.OutlookAPIKey = v
	}
	entries := make([]RecoveryCFMail, len(s.CloudflareMails))
	copy(entries, s.CloudflareMails)
	for i := range entries {
		if v, err := EncryptSecret(entries[i].AdminPassword); err == nil {
			entries[i].AdminPassword = v
		}
	}
	s.CloudflareMails = entries
	return s
}

// openRecoverySettings decrypts sealed secrets (plaintext passes through).
func openRecoverySettings(s RecoverySettings) RecoverySettings {
	if v, err := DecryptSecret(s.OutlookAPIKey); err == nil {
		s.OutlookAPIKey = v
	}
	entries := make([]RecoveryCFMail, len(s.CloudflareMails))
	copy(entries, s.CloudflareMails)
	for i := range entries {
		if v, err := DecryptSecret(entries[i].AdminPassword); err == nil {
			entries[i].AdminPassword = v
		}
	}
	s.CloudflareMails = entries
	return s
}
