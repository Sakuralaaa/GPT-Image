package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"chatgpt2api/internal/util"
)

// MailProviderOutlookEmail registers with real mailboxes held by an
// outlookEmail instance (github.com/assast/outlookEmail). Mailboxes are leased
// through its project API (claim-random) so each address is used once per
// project, and the outcome is reported back (complete-success / -failed).
const MailProviderOutlookEmail = "outlook_email"

const (
	outlookEmailDefaultProject = "gpt-image"
	outlookEmailCallerID       = "gpt-image"
	outlookEmailLeaseSeconds   = 1800
	outlookEmailCodeSkew       = 30 * time.Second
	outlookEmailDefaultTag     = "已注册GPT"
)

type registerOutlookEmailProvider struct {
	registerHTTPMailProvider
	entry map[string]any
}

func outlookEmailEntryConfig(entry map[string]any) (base, key, project string, err error) {
	base = strings.TrimRight(strings.TrimSpace(util.Clean(entry["api_base"])), "/")
	key = strings.TrimSpace(util.Clean(entry["api_key"]))
	project = strings.ToLower(strings.TrimSpace(util.Clean(entry["project_key"])))
	if project == "" {
		project = outlookEmailDefaultProject
	}
	if base == "" || key == "" {
		return "", "", "", errors.New("outlook_email 需要配置 api_base 与 api_key（读写权限）")
	}
	return base, key, project, nil
}

func (p *registerOutlookEmailProvider) call(method, path, key string, payload any) (map[string]any, int, error) {
	base, _, _, err := outlookEmailEntryConfig(p.entry)
	if err != nil {
		return nil, 0, err
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, base+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-API-Key", key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", p.conf.UserAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var decoded any
	if err := decodeJSONBody(resp.Body, &decoded); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d 返回非 JSON", resp.StatusCode)
	}
	return util.StringMap(decoded), resp.StatusCode, nil
}

// CreateMailbox leases one mailbox from the outlookEmail project pool. The
// project is (re)started first so newly imported mailboxes join the pool.
func (p *registerOutlookEmailProvider) CreateMailbox(_ string) (map[string]any, error) {
	_, key, project, err := outlookEmailEntryConfig(p.entry)
	if err != nil {
		return nil, err
	}
	startPayload := map[string]any{"project_key": project}
	if name := util.Clean(p.entry["project_name"]); name != "" {
		startPayload["name"] = name
	}
	if groups := outlookEmailGroupIDs(p.entry["group_ids"]); len(groups) > 0 {
		startPayload["group_ids"] = groups
	}
	if body, status, err := p.call(http.MethodPost, "/api/external/projects/start", key, startPayload); err != nil {
		return nil, fmt.Errorf("outlookEmail 启动项目失败: %w", err)
	} else if status != http.StatusOK || !outlookEmailOK(body) {
		return nil, fmt.Errorf("outlookEmail 启动项目失败: HTTP %d %s", status, outlookEmailError(body))
	}
	taskID := util.NewUUID()
	body, status, err := p.call(http.MethodPost, "/api/external/projects/"+project+"/claim-random", key, map[string]any{
		"caller_id":     outlookEmailCallerID,
		"task_id":       taskID,
		"lease_seconds": outlookEmailLeaseSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("outlookEmail 领取邮箱失败: %w", err)
	}
	if status != http.StatusOK || !outlookEmailOK(body) {
		return nil, fmt.Errorf("outlookEmail 领取邮箱失败: HTTP %d %s", status, outlookEmailError(body))
	}
	data := util.StringMap(body["data"])
	address := strings.TrimSpace(util.Clean(data["email"]))
	claimToken := util.Clean(data["claim_token"])
	accountID := util.ToInt(data["account_id"], 0)
	if address == "" || claimToken == "" || accountID <= 0 {
		return nil, errors.New("outlookEmail 领取结果缺少 email / account_id / claim_token")
	}
	return map[string]any{
		"provider":     MailProviderOutlookEmail,
		"provider_ref": util.Clean(p.entry["provider_ref"]),
		"address":      address,
		// A non-empty token keeps relogin on this mailbox instead of leasing a
		// new one (see reloginMailbox).
		"token":              MailProviderOutlookEmail,
		"outlook_project":    project,
		"outlook_account_id": accountID,
		"outlook_claim":      claimToken,
		"outlook_task_id":    taskID,
		"created_at":         time.Now().Unix(),
	}, nil
}

// FetchLatestMessage returns the newest OpenAI code mail received after the
// mailbox was leased (or first polled), so stale codes in a reused real inbox
// are ignored.
func (p *registerOutlookEmailProvider) FetchLatestMessage(mailbox map[string]any) (map[string]any, error) {
	base, key, _, err := outlookEmailEntryConfig(p.entry)
	if err != nil {
		return nil, err
	}
	address := util.Clean(mailbox["address"])
	if address == "" {
		return nil, errors.New("outlook_email mailbox missing address")
	}
	since := util.ToInt(mailbox["created_at"], 0)
	if since <= 0 {
		since = int(time.Now().Unix())
		mailbox["created_at"] = since
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.conf.RequestTimeout+5*time.Second)
	defer cancel()
	messages, err := fetchOutlookEmailMessages(ctx, p.client, base, key, address)
	if err != nil {
		return nil, err
	}
	cutoff := time.Unix(int64(since), 0).Add(-outlookEmailCodeSkew)
	var best *recoveryMailMessage
	for i := range messages {
		m := &messages[i]
		if m.Code == "" {
			continue
		}
		if !m.ReceivedAt.IsZero() && m.ReceivedAt.Before(cutoff) {
			continue
		}
		if best == nil || m.ReceivedAt.After(best.ReceivedAt) {
			best = m
		}
	}
	if best == nil {
		return nil, nil
	}
	out := map[string]any{
		"provider":     MailProviderOutlookEmail,
		"mailbox":      address,
		"message_id":   strings.TrimPrefix(best.Ref, "outlook:"),
		"subject":      best.Subject,
		"text_content": best.Text,
	}
	if !best.ReceivedAt.IsZero() {
		out["received_at"] = best.ReceivedAt.UTC().Format(time.RFC3339)
	}
	return out, nil
}

// reportRegisterMailboxResult closes an outlookEmail lease with the outcome of
// the registration. Other providers need no report.
func reportRegisterMailboxResult(mailConfig map[string]any, mailbox map[string]any, ctx context.Context, runErr error) {
	if util.Clean(mailbox["provider"]) != MailProviderOutlookEmail || util.Clean(mailbox["outlook_claim"]) == "" {
		return
	}
	entry, err := selectRegisterMailEntry(mailConfig, MailProviderOutlookEmail, util.Clean(mailbox["provider_ref"]))
	if err != nil {
		return
	}
	_, key, _, err := outlookEmailEntryConfig(entry)
	if err != nil {
		return
	}
	conf := registerMailSettingsFromConfig(mailConfig)
	p := &registerOutlookEmailProvider{registerHTTPMailProvider: registerHTTPMailProvider{client: registerMailHTTPClient(conf.RequestTimeout), conf: conf}, entry: entry}
	defer p.Close()
	action, detail := "complete-success", "注册成功"
	switch {
	case runErr == nil:
	case ctx != nil && ctx.Err() != nil:
		action, detail = "release", "任务中断"
	default:
		action, detail = "complete-failed", truncateText(runErr.Error(), 480)
	}
	project := firstNonEmpty(util.Clean(mailbox["outlook_project"]), outlookEmailDefaultProject)
	accountID := util.ToInt(mailbox["outlook_account_id"], 0)
	_, _, _ = p.call(http.MethodPost, "/api/external/projects/"+project+"/"+action, key, map[string]any{
		"account_id":  accountID,
		"claim_token": util.Clean(mailbox["outlook_claim"]),
		"caller_id":   outlookEmailCallerID,
		"task_id":     util.Clean(mailbox["outlook_task_id"]),
		"detail":      detail,
	})
	mailbox["outlook_claim"] = ""
	if runErr == nil && accountID > 0 {
		_ = p.tagAccount(accountID)
	}
}

// tagAccount marks a registered mailbox with a tag in the outlookEmail UI.
// Tags are only exposed by the session API, so this needs the web login
// password and is skipped when none is configured.
func (p *registerOutlookEmailProvider) tagAccount(accountID int) error {
	password := util.Clean(p.entry["login_password"])
	if password == "" {
		return nil
	}
	base, _, _, err := outlookEmailEntryConfig(p.entry)
	if err != nil {
		return err
	}
	tagName := firstNonEmpty(strings.TrimSpace(util.Clean(p.entry["tag_name"])), outlookEmailDefaultTag)
	sess := &outlookEmailSession{client: p.client, base: base, ua: p.conf.UserAgent}
	if body, status, err := sess.do(http.MethodPost, "/login", map[string]any{"password": password}); err != nil {
		return err
	} else if status != http.StatusOK || !outlookEmailOK(body) {
		return fmt.Errorf("outlookEmail 登录失败: HTTP %d %s", status, outlookEmailError(body))
	}
	if body, status, err := sess.do(http.MethodGet, "/api/csrf-token", nil); err == nil && status == http.StatusOK {
		sess.csrf = util.Clean(body["csrf_token"])
	}
	tagID := 0
	body, status, err := sess.do(http.MethodGet, "/api/tags", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK || !outlookEmailOK(body) {
		return fmt.Errorf("outlookEmail 读取标签失败: HTTP %d %s", status, outlookEmailError(body))
	}
	for _, tag := range util.AsMapSlice(body["tags"]) {
		if strings.TrimSpace(util.Clean(tag["name"])) == tagName {
			tagID = util.ToInt(tag["id"], 0)
			break
		}
	}
	if tagID <= 0 {
		created, status, err := sess.do(http.MethodPost, "/api/tags", map[string]any{"name": tagName, "color": "#10a37f"})
		if err != nil {
			return err
		}
		if status != http.StatusOK || !outlookEmailOK(created) {
			return fmt.Errorf("outlookEmail 创建标签失败: HTTP %d %s", status, outlookEmailError(created))
		}
		tagID = util.ToInt(util.StringMap(created["tag"])["id"], 0)
	}
	if tagID <= 0 {
		return errors.New("outlookEmail 标签 ID 无效")
	}
	result, status, err := sess.do(http.MethodPost, "/api/accounts/tags", map[string]any{
		"account_ids": []int{accountID},
		"tag_id":      tagID,
		"action":      "add",
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK || !outlookEmailOK(result) {
		return fmt.Errorf("outlookEmail 打标签失败: HTTP %d %s", status, outlookEmailError(result))
	}
	return nil
}

// outlookEmailSession carries the Flask session cookie by hand so it also
// works when the cookie is marked Secure but the instance is served over http.
type outlookEmailSession struct {
	client  *http.Client
	base    string
	ua      string
	cookies map[string]string
	csrf    string
}

func (s *outlookEmailSession) do(method, path string, payload any) (map[string]any, int, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.base+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.ua)
	if s.csrf != "" {
		req.Header.Set("X-CSRFToken", s.csrf)
	}
	parts := make([]string, 0, len(s.cookies))
	for name, value := range s.cookies {
		parts = append(parts, name+"="+value)
	}
	if len(parts) > 0 {
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	client := *s.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	for _, c := range resp.Cookies() {
		if s.cookies == nil {
			s.cookies = map[string]string{}
		}
		s.cookies[c.Name] = c.Value
	}
	var decoded any
	if err := decodeJSONBody(resp.Body, &decoded); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d 返回非 JSON", resp.StatusCode)
	}
	return util.StringMap(decoded), resp.StatusCode, nil
}

func outlookEmailOK(body map[string]any) bool {
	if body == nil {
		return false
	}
	ok, present := body["success"]
	return !present || util.ToBool(ok)
}

func outlookEmailError(body map[string]any) string {
	return firstNonEmpty(util.Clean(body["error"]), util.Clean(body["message"]))
}

func outlookEmailGroupIDs(value any) []int {
	var out []int
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if id := util.ToInt(item, 0); id > 0 {
				out = append(out, id)
			}
		}
	case []int:
		for _, id := range typed {
			if id > 0 {
				out = append(out, id)
			}
		}
	case string:
		for _, part := range strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
			if id := util.ToInt(part, 0); id > 0 {
				out = append(out, id)
			}
		}
	}
	return out
}
