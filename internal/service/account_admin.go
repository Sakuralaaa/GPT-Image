package service

import (
	"context"
	"errors"
	"strings"

	"chatgpt2api/internal/util"
)

// accountCredentialFields are the self-healing credentials an admin may view
// and edit in the account detail drawer.
var accountCredentialFields = []string{"email", "password", "mail_provider", "mail_ref", "mail_token", "session_token", "note", "mail_channel"}

// ErrAccountNotFound is returned when an account id does not resolve.
var ErrAccountNotFound = errors.New("account not found")

// AccountTokenByID resolves the pool access token for a public account id.
func (s *AccountService) AccountTokenByID(id string) string {
	return s.GetTokenByID(id)
}

// AccountTokenByEmail returns the access token of the first pool entry whose
// email matches (case-insensitive).
func (s *AccountService) AccountTokenByEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if strings.EqualFold(util.Clean(item["email"]), email) {
			return util.Clean(item["access_token"])
		}
	}
	return ""
}

// AccountDetail returns the public view of an account plus its stored
// credentials (decrypted) for the admin detail drawer.
func (s *AccountService) AccountDetail(id string) (map[string]any, error) {
	token := s.GetTokenByID(id)
	if token == "" {
		return nil, ErrAccountNotFound
	}
	account := s.GetAccount(token)
	if account == nil {
		return nil, ErrAccountNotFound
	}
	public := publicAccounts([]map[string]any{account})
	if len(public) == 0 {
		return nil, ErrAccountNotFound
	}
	detail := public[0]
	for _, field := range accountCredentialFields {
		detail[field] = util.Clean(account[field])
	}
	detail["refresh_next_at"] = account["refresh_next_at"]
	detail["session_expires"] = account["session_expires"]
	return detail, nil
}

// AccountCredentials returns the relogin credentials stored on an account.
func (s *AccountService) AccountCredentials(accessToken string) map[string]any {
	account := s.GetAccount(accessToken)
	if account == nil {
		return nil
	}
	return map[string]any{
		"email":         util.Clean(account["email"]),
		"password":      util.Clean(account["password"]),
		"mail_provider": util.Clean(account["mail_provider"]),
		"mail_ref":      util.Clean(account["mail_ref"]),
		"mail_token":    util.Clean(account["mail_token"]),
		"session_token": util.Clean(account["session_token"]),
		"mail_channel":  util.Clean(account["mail_channel"]),
	}
}

// UpdateAccountCredentials edits the stored credentials of an account. Only
// keys present in updates are touched; empty strings clear a field.
func (s *AccountService) UpdateAccountCredentials(id string, updates map[string]any) (map[string]any, error) {
	token := s.GetTokenByID(id)
	if token == "" {
		return nil, ErrAccountNotFound
	}
	next := map[string]any{}
	for _, field := range accountCredentialFields {
		if value, ok := updates[field]; ok {
			next[field] = strings.TrimSpace(util.Clean(value))
		}
	}
	if len(next) == 0 {
		return nil, errors.New("no updates provided")
	}
	if s.UpdateAccount(token, next) == nil {
		return nil, ErrAccountNotFound
	}
	return s.AccountDetail(id)
}

// ReplaceAccountTokens overwrites an account's access token (and optionally
// its session token) with values pasted by the admin.
func (s *AccountService) ReplaceAccountTokens(id, accessToken, sessionToken string) (string, error) {
	token := s.GetTokenByID(id)
	if token == "" {
		return "", ErrAccountNotFound
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", errors.New("access_token is required")
	}
	if sessionToken == "" {
		if account := s.GetAccount(token); account != nil {
			sessionToken = util.Clean(account["session_token"])
		}
	}
	if !s.RefreshAccountViaSession(token, accessToken, sessionToken, "") {
		return "", ErrAccountNotFound
	}
	s.UpdateAccount(accessToken, map[string]any{"relogin_failures": 0})
	return accessToken, nil
}

// RefreshAccountNow synchronously renews an account's access token from its
// stored session token. It returns the new access token.
func (s *AccountService) RefreshAccountNow(ctx context.Context, accessToken string) (string, error) {
	account := s.GetAccount(accessToken)
	if account == nil {
		return "", ErrAccountNotFound
	}
	sessionToken := util.Clean(account["session_token"])
	if sessionToken == "" {
		return "", errors.New("账号没有 session_token，无法刷新")
	}
	newAccessToken, newSessionToken, newExpires, err := s.refresher.RefreshToken(ctx, accessToken, sessionToken)
	if err != nil {
		s.UpdateAccount(accessToken, map[string]any{"last_error": err.Error()})
		return "", err
	}
	if newAccessToken == "" {
		return "", errors.New("session 刷新未返回 access_token")
	}
	if !s.RefreshAccountViaSession(accessToken, newAccessToken, newSessionToken, newExpires) {
		return "", ErrAccountNotFound
	}
	return newAccessToken, nil
}

// RecordAccountError stores the latest failure reason on an account.
func (s *AccountService) RecordAccountError(accessToken, reason string) {
	if accessToken == "" {
		return
	}
	s.UpdateAccount(accessToken, map[string]any{"last_error": reason})
}

// ExportAccounts returns every account with its credentials (decrypted) for
// CSV/JSON export.
func (s *AccountService) ExportAccounts() []map[string]any {
	s.mu.Lock()
	items := make([]map[string]any, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, util.CopyMap(item))
	}
	s.mu.Unlock()
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		token := util.Clean(item["access_token"])
		out = append(out, map[string]any{
			"id":              accountIDFromToken(token),
			"email":           util.Clean(item["email"]),
			"password":        util.Clean(item["password"]),
			"mail_provider":   util.Clean(item["mail_provider"]),
			"mail_ref":        util.Clean(item["mail_ref"]),
			"mail_token":      util.Clean(item["mail_token"]),
			"type":            util.Clean(item["type"]),
			"status":          util.Clean(item["status"]),
			"enabled":         accountEnabledValue(item),
			"access_token":    token,
			"session_token":   util.Clean(item["session_token"]),
			"refresh_token":   util.Clean(item["refresh_token"]),
			"mail_channel":    util.Clean(item["mail_channel"]),
			"created_at":      util.Clean(item["created_at"]),
			"last_refresh_at": util.Clean(item["last_refresh_at"]),
			"last_error":      util.Clean(item["last_error"]),
			"note":            util.Clean(item["note"]),
		})
	}
	return out
}

// AccountStatusCounts summarizes the pool by status for the dashboard.
func (s *AccountService) AccountStatusCounts() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	byStatus := map[string]int{}
	byType := map[string]int{}
	enabled, quota := 0, 0
	for _, item := range s.items {
		byStatus[firstNonEmpty(util.Clean(item["status"]), "正常")]++
		byType[firstNonEmpty(util.Clean(item["type"]), "Free")]++
		if accountEnabledValue(item) {
			enabled++
		}
		quota += util.ToInt(item["quota"], 0)
	}
	return map[string]any{
		"total":     len(s.items),
		"enabled":   enabled,
		"by_status": byStatus,
		"by_type":   byType,
		"quota":     quota,
	}
}
