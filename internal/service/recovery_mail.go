package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"chatgpt2api/internal/util"
)

// Recovery mail channels. Routing is deterministic by the account's email
// domain (or a per-account override) and never falls back across channels.
const (
	MailChannelCloudflare = "cloudflare_temp_email"
	MailChannelOutlook    = "outlook"
	MailChannelGPTMail    = "gptmail"
)

// MailRoute is the routed mail channel for an address.
type MailRoute struct {
	Channel   string         `json:"channel"`
	EntryRef  string         `json:"entry_ref"`
	EntryName string         `json:"entry_name"`
	Source    string         `json:"source"` // recovery | register (CF/gptmail entry origin)
	Override  bool           `json:"override"`
	Ready     bool           `json:"ready"`
	Reason    string         `json:"reason"`
	entry     map[string]any // provider config entry (cloudflare/gptmail)
}

func emailDomain(email string) string {
	_, domain, _ := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	return strings.TrimSpace(domain)
}

// isOutlookDomain reports whether the domain belongs to the outlookEmail
// channel: an exact entry of the configured list, or an outlook.* /
// hotmail.* domain (country variants such as outlook.jp, hotmail.co.uk).
func isOutlookDomain(domain string, configured []string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	for _, d := range configured {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return strings.HasPrefix(domain, "outlook.") || strings.HasPrefix(domain, "hotmail.")
}

func entryCoversDomain(entry map[string]any, domain string) bool {
	for _, d := range append(util.AsStringSlice(entry["domain"]), util.Clean(entry["default_domain"])) {
		if d = strings.TrimSpace(d); d != "" && strings.EqualFold(d, domain) {
			return true
		}
	}
	return false
}

// RouteMailChannel picks the channel for an address:
//  1. a recovery-owned Cloudflare entry (settings.CloudflareMails) whose
//     suffixes match the domain;
//  2. a register cloudflare_temp_email entry whose domain list contains the
//     domain (legacy, reason "沿用注册机邮箱配置");
//  3. outlook/hotmail domains -> the outlookEmail channel;
//  4. everything else -> gptmail.
//
// override ("cloudflare_temp_email", "outlook", "gptmail") forces a channel.
func RouteMailChannel(email string, mailConfig map[string]any, settings RecoverySettings, override, storedRef string) MailRoute {
	domain := emailDomain(email)
	entries := []map[string]any{}
	for _, entry := range registerMailEntries(mailConfig) {
		if util.ToBool(entry["enable"]) {
			entries = append(entries, entry)
		}
	}
	findCF := func() map[string]any {
		for _, entry := range entries {
			if util.Clean(entry["type"]) == MailChannelCloudflare && entryCoversDomain(entry, domain) {
				return entry
			}
		}
		return nil
	}
	route := MailRoute{}
	override = strings.TrimSpace(override)
	switch override {
	case MailChannelCloudflare, MailChannelOutlook, MailChannelGPTMail:
		route.Channel = override
		route.Override = true
	default:
		switch {
		case strings.HasPrefix(storedRef, MailProviderOutlookEmail+"#"):
			// Registered through an outlookEmail pool: the mailbox may be any
			// provider outlookEmail manages (Outlook, Gmail, IMAP...).
			route.Channel = MailChannelOutlook
		case findRecoveryCF(settings.CloudflareMails, domain) != nil, findCF() != nil:
			route.Channel = MailChannelCloudflare
		case isOutlookDomain(domain, settings.OutlookDomains):
			route.Channel = MailChannelOutlook
		default:
			route.Channel = MailChannelGPTMail
		}
	}
	switch route.Channel {
	case MailChannelCloudflare:
		if e := findRecoveryCF(settings.CloudflareMails, domain); e != nil {
			applyRecoveryCFRoute(&route, e)
			return route
		}
		entry := findCF()
		if entry == nil && storedRef != "" {
			if e := findRecoveryCFByRef(settings.CloudflareMails, storedRef); e != nil {
				applyRecoveryCFRoute(&route, e)
				return route
			}
		}
		if entry == nil && storedRef != "" {
			for _, e := range entries {
				if util.Clean(e["provider_ref"]) == storedRef && util.Clean(e["type"]) == MailChannelCloudflare {
					entry = e
				}
			}
		}
		if entry == nil {
			route.Reason = "未配置覆盖域名 " + domain + " 的 Cloudflare 临时邮箱（找回设置或注册机邮箱配置）"
			return route
		}
		route.entry = entry
		route.EntryRef = util.Clean(entry["provider_ref"])
		route.EntryName = firstNonEmpty(util.Clean(entry["name"]), util.Clean(entry["api_base"]), route.EntryRef)
		route.Source = MailRouteSourceRegister
		route.Reason = legacyCFRouteReason
		route.Ready = true
	case MailChannelOutlook:
		if strings.TrimSpace(settings.OutlookBaseURL) == "" || strings.TrimSpace(settings.OutlookAPIKey) == "" {
			// Fall back to the register machine's outlook_email entry.
			var picked map[string]any
			for _, e := range entries {
				if util.Clean(e["type"]) != MailProviderOutlookEmail {
					continue
				}
				if picked == nil || (storedRef != "" && util.Clean(e["provider_ref"]) == storedRef) {
					picked = e
				}
			}
			if picked == nil {
				route.Reason = "outlookEmail 渠道未配置（找回设置 base_url / api_key，或注册机 outlook_email 邮箱）"
				return route
			}
			route.entry = picked
			route.EntryRef = util.Clean(picked["provider_ref"])
			route.EntryName = firstNonEmpty(util.Clean(picked["name"]), "outlookEmail")
			route.Source = MailRouteSourceRegister
			route.Ready = true
			return route
		}
		route.EntryRef = "outlookEmail"
		route.EntryName = "outlookEmail"
		route.Ready = true
	case MailChannelGPTMail:
		var picked map[string]any
		for _, e := range entries {
			if util.Clean(e["type"]) != MailChannelGPTMail {
				continue
			}
			if storedRef != "" && util.Clean(e["provider_ref"]) == storedRef {
				picked = e
				break
			}
			if picked == nil || (entryCoversDomain(e, domain) && !entryCoversDomain(picked, domain)) {
				picked = e
			}
		}
		if picked == nil {
			route.Reason = "未配置启用的 gptmail 邮箱渠道"
			return route
		}
		route.entry = picked
		route.EntryRef = util.Clean(picked["provider_ref"])
		route.EntryName = firstNonEmpty(util.Clean(picked["name"]), route.EntryRef)
		route.Source = MailRouteSourceRegister
		route.Ready = true
	}
	return route
}

// recoveryMailMessage is a normalized message read from a channel.
type recoveryMailMessage struct {
	Ref        string
	Subject    string
	Text       string
	HTML       string
	ReceivedAt time.Time
	Code       string
}

// routeFor resolves the channel for an account's credentials.
func (r *AccountRecovery) routeFor(email string, creds map[string]any) MailRoute {
	return RouteMailChannel(email, r.mailConfig(), r.Settings(), util.Clean(creds["mail_channel"]), util.Clean(creds["mail_ref"]))
}

// RouteForAccount exposes the routed channel for the account drawer.
func (r *AccountRecovery) RouteForAccount(email string, creds map[string]any) MailRoute {
	return r.routeFor(email, creds)
}

// readChannelMessages returns recent messages for the address through the
// routed channel only.
func (r *AccountRecovery) readChannelMessages(ctx context.Context, route MailRoute, email string, creds map[string]any) ([]recoveryMailMessage, error) {
	if !route.Ready {
		return nil, errors.New(route.Reason)
	}
	switch route.Channel {
	case MailChannelCloudflare:
		if route.Source == MailRouteSourceRecovery {
			return fetchRecoveryCFMessages(route.entry, email, util.Clean(creds["mail_token"]))
		}
		var errs []string
		token := util.Clean(creds["mail_token"])
		if token != "" {
			provider, err := r.newProvider(r.mailConfig(), MailChannelCloudflare, route.EntryRef)
			if err == nil {
				message, fetchErr := provider.FetchLatestMessage(map[string]any{"provider": MailChannelCloudflare, "provider_ref": route.EntryRef, "address": email, "token": token})
				provider.Close()
				if fetchErr == nil {
					if message == nil {
						return nil, nil
					}
					return []recoveryMailMessage{normalizeRecoveryMessage(message)}, nil
				}
				errs = append(errs, "邮箱 JWT 读取失败: "+fetchErr.Error())
			} else {
				errs = append(errs, err.Error())
			}
		}
		if util.Clean(route.entry["admin_password"]) != "" && util.Clean(route.entry["api_base"]) != "" {
			message, err := fetchAdminMailMessage(route.entry, email)
			if err != nil {
				errs = append(errs, "admin API 读取失败: "+err.Error())
				return nil, errors.New(strings.Join(errs, "；"))
			}
			if message == nil {
				return nil, nil
			}
			return []recoveryMailMessage{normalizeRecoveryMessage(message)}, nil
		}
		if len(errs) == 0 {
			errs = append(errs, "账号没有 mail_token，且该 cloudflare_temp_email 未配置 admin_password")
		}
		return nil, errors.New(strings.Join(errs, "；"))
	case MailChannelGPTMail:
		provider, err := r.newProvider(r.mailConfig(), MailChannelGPTMail, route.EntryRef)
		if err != nil {
			return nil, err
		}
		defer provider.Close()
		message, err := provider.FetchLatestMessage(map[string]any{"provider": MailChannelGPTMail, "provider_ref": route.EntryRef, "address": email})
		if err != nil {
			return nil, fmt.Errorf("gptmail 读取失败: %w", err)
		}
		if message == nil {
			return nil, nil
		}
		return []recoveryMailMessage{normalizeRecoveryMessage(message)}, nil
	case MailChannelOutlook:
		settings := r.Settings()
		if route.entry != nil {
			base, key, _, err := outlookEmailEntryConfig(route.entry)
			if err != nil {
				return nil, err
			}
			return fetchOutlookEmailMessages(ctx, r.outlookClient(), base, key, email)
		}
		return fetchOutlookEmailMessages(ctx, r.outlookClient(), settings.OutlookBaseURL, settings.OutlookAPIKey, email)
	}
	return nil, fmt.Errorf("未知邮箱渠道 %s", route.Channel)
}

func (r *AccountRecovery) outlookClient() *http.Client {
	if r.httpClient != nil {
		return r.httpClient
	}
	return registerMailHTTPClient(20 * time.Second)
}

func normalizeRecoveryMessage(message map[string]any) recoveryMailMessage {
	text, html := extractRegisterMailContent(message)
	return recoveryMailMessage{
		Ref:        registerMailMessageRef(message),
		Subject:    util.Clean(message["subject"]),
		Text:       text,
		HTML:       html,
		ReceivedAt: parseRecoveryMailTime(message["received_at"]),
		Code:       extractRegisterMailCode(message),
	}
}

// parseRecoveryMailTime parses provider timestamps, treating naive
// "2006-01-02 15:04:05" values (cloudflare_temp_email) as UTC.
func parseRecoveryMailTime(value any) time.Time {
	if t := parseRegisterMailTime(value); !t.IsZero() {
		return t
	}
	text := util.Clean(value)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04:05.000"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t
		}
	}
	// Millisecond epochs.
	if n := util.ToInt(value, 0); n > 1_000_000_000_000 {
		return time.UnixMilli(int64(n)).UTC()
	}
	return time.Time{}
}

// recoveryCodeFreshSkew tolerates clock drift between mail servers and us.
const recoveryCodeFreshSkew = 30 * time.Second

// recoveryCodeSettle is how long the fetcher keeps polling after the first
// fresh code shows up, so a second login mail sent for the same attempt
// (which voids the first) wins over the earlier one.
const recoveryCodeSettle = 8 * time.Second

// recoverySeenCodeKey marks a code upstream already rejected.
func recoverySeenCodeKey(code string) string {
	return "code:" + strings.TrimSpace(code)
}

// freshCode returns the newest code among messages received after since (or
// unseen messages when a message carries no timestamp), skipping codes
// already rejected.
func freshCode(messages []recoveryMailMessage, since time.Time, seen map[string]struct{}) string {
	var best recoveryMailMessage
	for _, m := range messages {
		if m.Code == "" {
			continue
		}
		if _, rejected := seen[recoverySeenCodeKey(m.Code)]; rejected {
			continue
		}
		if !m.ReceivedAt.IsZero() {
			if !since.IsZero() && m.ReceivedAt.Before(since.Add(-recoveryCodeFreshSkew)) {
				continue
			}
		} else if _, ok := seen[m.Ref]; ok && m.Ref != "" {
			continue
		}
		if best.Code == "" || m.ReceivedAt.After(best.ReceivedAt) {
			best = m
		}
	}
	return best.Code
}

// ReadLatestMail returns the newest message (and any code in it) through the
// account's routed channel.
func (r *AccountRecovery) ReadLatestMail(email string, creds map[string]any) (map[string]any, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, errors.New("账号没有邮箱")
	}
	if creds == nil {
		creds = map[string]any{}
	}
	route := r.routeFor(email, creds)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	messages, err := r.readChannelMessages(ctx, route, email, creds)
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", route.Channel, err)
	}
	out := map[string]any{"channel": route.Channel, "entry_ref": route.EntryRef, "entry_name": route.EntryName, "empty": len(messages) == 0}
	if len(messages) == 0 {
		return out, nil
	}
	latest := messages[0]
	for _, m := range messages[1:] {
		if m.ReceivedAt.After(latest.ReceivedAt) {
			latest = m
		}
	}
	out["subject"] = latest.Subject
	out["code"] = latest.Code
	out["text"] = truncateText(latest.Text, 4000)
	out["html"] = truncateText(latest.HTML, 20000)
	if !latest.ReceivedAt.IsZero() {
		out["received_at"] = latest.ReceivedAt.Format(time.RFC3339)
	}
	return out, nil
}

func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func (r *AccountRecovery) snapshotSeenMail(email string, creds map[string]any) map[string]struct{} {
	seen := map[string]struct{}{}
	route := r.routeFor(email, creds)
	if !route.Ready {
		return seen
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	messages, _ := r.readChannelMessages(ctx, route, email, creds)
	for _, m := range messages {
		if m.Ref != "" {
			seen[m.Ref] = struct{}{}
		}
	}
	return seen
}

func (r *AccountRecovery) defaultFetchCode(ctx context.Context, email string, creds map[string]any, since time.Time, seen map[string]struct{}, wait time.Duration) (string, error) {
	route := r.routeFor(email, creds)
	if !route.Ready {
		return "", errors.New(route.Reason)
	}
	deadline := time.Now().Add(wait)
	var lastErr error
	found := ""
	var settleUntil time.Time
	for {
		messages, err := r.readChannelMessages(ctx, route, email, creds)
		if err != nil {
			lastErr = err
		} else if code := freshCode(messages, since, seen); code != "" {
			if found == "" {
				settleUntil = time.Now().Add(r.codeSettle())
			}
			found = code
		}
		if found != "" && !time.Now().Before(settleUntil) {
			return found, nil
		}
		if time.Now().After(deadline) {
			if found != "" {
				return found, nil
			}
			if lastErr != nil {
				return "", fmt.Errorf("[%s] %w", route.Channel, lastErr)
			}
			return "", fmt.Errorf("[%s] 等待验证码超时（%s 内未收到新验证码）", route.Channel, wait)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(r.pollInterval()):
		}
	}
}

func (r *AccountRecovery) codeSettle() time.Duration {
	if r.fetchInterval > 0 {
		// Tests poll fast; keep the settle window proportional.
		return 2 * r.fetchInterval
	}
	return recoveryCodeSettle
}

func (r *AccountRecovery) pollInterval() time.Duration {
	if r.fetchInterval > 0 {
		return r.fetchInterval
	}
	return 4 * time.Second
}

func (r *AccountRecovery) hasMailSource(email string, creds map[string]any) bool {
	return r.routeFor(email, creds).Ready
}

// fetchAdminMailMessage reads the newest message for an address through the
// cloudflare_temp_email admin API and normalizes it for code extraction.
func fetchAdminMailMessage(entry map[string]any, email string) (map[string]any, error) {
	apiBase := strings.TrimRight(util.Clean(entry["api_base"]), "/")
	client := registerMailHTTPClient(15 * time.Second)
	defer client.CloseIdleConnections()
	data, err := registerMailRequestJSON(client, "GET", apiBase+"/admin/mails", map[string]string{
		"x-admin-auth": util.Clean(entry["admin_password"]),
		"User-Agent":   "Mozilla/5.0",
		"Accept":       "application/json",
	}, map[string]string{
		"address": email,
		"limit":   "5",
		"offset":  "0",
	}, nil, 200)
	if err != nil {
		return nil, err
	}
	items := util.AsMapSlice(data["results"])
	if len(items) == 0 {
		items = util.AsMapSlice(data["raw"])
		if len(items) == 0 {
			return nil, nil
		}
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

// ── outlookEmail (github.com/assast/outlookEmail) ───────────────────────────

var outlookCodePattern = regexp.MustCompile(`(?:^|[^0-9])([0-9]{6})(?:[^0-9]|$)`)

// fetchOutlookEmailMessages reads recent mail for an address through the
// outlookEmail external API. Only OpenAI/ChatGPT messages yield codes.
func fetchOutlookEmailMessages(ctx context.Context, client *http.Client, baseURL, apiKey, email string) ([]recoveryMailMessage, error) {
	query := url.Values{"email": {email}, "folder": {"all"}, "top": {"10"}}
	target := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/external/emails?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("outlookEmail 请求失败: %w", err)
	}
	defer resp.Body.Close()
	var payload any
	if err := decodeJSONBody(resp.Body, &payload); err != nil {
		return nil, fmt.Errorf("outlookEmail 返回非 JSON (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		msg := util.Clean(util.StringMap(payload)["error"])
		if msg == "" {
			msg = util.Clean(util.StringMap(payload)["message"])
		}
		return nil, fmt.Errorf("outlookEmail HTTP %d %s", resp.StatusCode, msg)
	}
	if body := util.StringMap(payload); body != nil {
		if ok, present := body["success"]; present && !util.ToBool(ok) {
			return nil, fmt.Errorf("outlookEmail 返回失败: %s", firstNonEmpty(util.Clean(body["error"]), util.Clean(body["message"])))
		}
	}
	items := outlookEmailItems(payload)
	out := make([]recoveryMailMessage, 0, len(items))
	for _, item := range items {
		subject := util.Clean(item["subject"])
		from := util.Clean(firstNonNil(item["from"], item["sender"], item["from_address"]))
		if fm := util.StringMap(item["from"]); len(fm) > 0 {
			from = util.Clean(firstNonNil(fm["address"], fm["email"], util.StringMap(fm["emailAddress"])["address"], fm["name"]))
		}
		text := strings.Join([]string{
			util.Clean(firstNonNil(item["body_preview"], item["bodyPreview"], item["preview"])),
			registerContentString(firstNonNil(item["body"], item["content"], item["text"])),
		}, "\n")
		if bm := util.StringMap(item["body"]); len(bm) > 0 {
			text += "\n" + util.Clean(bm["content"])
		}
		m := recoveryMailMessage{
			Ref:        "outlook:" + util.Clean(firstNonNil(item["id"], item["message_id"], item["internetMessageId"])),
			Subject:    subject,
			Text:       text,
			ReceivedAt: parseRecoveryMailTime(firstNonNil(item["date"], item["received_at"], item["receivedDateTime"], item["received"], item["created_at"])),
		}
		lower := strings.ToLower(subject + " " + from + " " + text)
		if strings.Contains(lower, "openai") || strings.Contains(lower, "chatgpt") {
			m.Code = extractRegisterMailCode(map[string]any{"subject": subject, "text_content": text})
			if m.Code == "" {
				if match := outlookCodePattern.FindStringSubmatch(subject + "\n" + text); len(match) > 1 {
					m.Code = match[1]
				}
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// outlookEmailItems extracts the message list from the defensive set of
// response shapes: a bare list, {emails:[...]}, {data:[...]},
// {data:{emails:[...]}}, {value:[...]}.
func outlookEmailItems(payload any) []map[string]any {
	if list := util.AsMapSlice(payload); len(list) > 0 {
		return list
	}
	body := util.StringMap(payload)
	for _, key := range []string{"emails", "data", "messages", "value", "items", "results"} {
		if list := util.AsMapSlice(body[key]); len(list) > 0 {
			return list
		}
		if nested := util.StringMap(body[key]); len(nested) > 0 {
			if list := outlookEmailItems(nested); len(list) > 0 {
				return list
			}
		}
	}
	return nil
}
