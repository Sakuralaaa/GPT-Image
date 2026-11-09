package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"chatgpt2api/internal/service"
	"chatgpt2api/internal/util"
)

type apiKeyLeaseContextKey struct{}

// requireAPIIdentity authenticates a /v1 request: a standalone API key, or an
// admin credential (panel session or admin key) which bypasses key limits.
func (a *App) requireAPIIdentity(w http.ResponseWriter, r *http.Request, overrideAuth string) (service.Identity, bool) {
	token := overrideAuthToken(overrideAuth, r)
	if a.apiKeys != nil {
		if key := a.apiKeys.Lookup(token); key != nil {
			identity := service.Identity{
				ID:             "apikey:" + key.ID,
				Name:           key.Name,
				Role:           service.AuthRoleAPIKey,
				Provider:       service.AuthProviderLocal,
				OwnerID:        "apikey:" + key.ID,
				CredentialID:   key.ID,
				CredentialName: key.Name,
				Kind:           service.AuthKindAPIKey,
			}
			*r = *r.WithContext(withRequestIdentity(r.Context(), identity))
			return identity, true
		}
	}
	if identity := a.auth.Authenticate(token); identity != nil && identity.Role == service.AuthRoleAdmin {
		*r = *r.WithContext(withRequestIdentity(r.Context(), *identity))
		return *identity, true
	}
	writeOpenAIError(w, http.StatusUnauthorized, "invalid_api_key", "invalid_request_error", "Incorrect API key provided.")
	return service.Identity{}, false
}

func writeOpenAIError(w http.ResponseWriter, status int, code, typ, message string) {
	util.WriteJSON(w, status, map[string]any{"error": map[string]any{"message": message, "type": typ, "param": nil, "code": code}})
}

// admitAPIKey enforces the standalone key limits for one request. The
// returned release must be deferred; image usage is recorded through
// finishAPIKeyUsage once the output is known.
func (a *App) admitAPIKey(w http.ResponseWriter, r *http.Request, identity service.Identity, model string, imageUnits int) (func(), bool) {
	if identity.Role != service.AuthRoleAPIKey || a.apiKeys == nil {
		return func() {}, true
	}
	lease, err := a.apiKeys.Admit(identity.CredentialID, model, imageUnits)
	if err != nil {
		var keyErr *service.APIKeyError
		if errors.As(err, &keyErr) {
			a.logCall(r.Context(), identity, "API Key 拒绝", r.Method, r.URL.Path, model, time.Now(), "failed", keyErr.Status, keyErr.Message, nil, requestAuditCapture(r.Context()))
			markRequestBusinessLogged(r)
			util.WriteJSON(w, keyErr.Status, keyErr.OpenAIError())
			return nil, false
		}
		util.WriteError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	*r = *r.WithContext(context.WithValue(r.Context(), apiKeyLeaseContextKey{}, lease))
	return func() { lease.Finish(0) }, true
}

func (a *App) finishAPIKeyUsage(r *http.Request, images int) {
	if lease, ok := r.Context().Value(apiKeyLeaseContextKey{}).(*service.APIKeyLease); ok {
		lease.Finish(images)
	}
}

// ── /api/keys ───────────────────────────────────────────────────────────────

func (a *App) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireIdentity(w, r, ""); !ok {
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/keys"))
	switch {
	case len(parts) == 0 && r.Method == http.MethodGet:
		util.WriteJSON(w, http.StatusOK, map[string]any{"items": a.apiKeys.List(), "summary": a.apiKeys.UsageSummary()})
	case len(parts) == 0 && r.Method == http.MethodPost:
		body, err := readJSONMap(r)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		item, raw, err := a.apiKeys.Create(body)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"item": item, "key": raw})
	case len(parts) == 1 && (r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut):
		body, err := readJSONMap(r)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		item, err := a.apiKeys.Update(parts[0], body)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"item": item})
	case len(parts) == 1 && r.Method == http.MethodDelete:
		if !a.apiKeys.Delete(parts[0]) {
			util.WriteError(w, http.StatusNotFound, "key not found")
			return
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	case len(parts) == 2 && parts[1] == "reset-usage" && r.Method == http.MethodPost:
		item, err := a.apiKeys.ResetUsage(parts[0])
		if err != nil {
			util.WriteError(w, http.StatusNotFound, err.Error())
			return
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"item": item})
	default:
		http.NotFound(w, r)
	}
}

// ── /api/recovery ───────────────────────────────────────────────────────────

func (a *App) handleRecovery(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireIdentity(w, r, ""); !ok {
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/recovery"))
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	switch {
	case parts[0] == "settings" && len(parts) == 1 && r.Method == http.MethodGet:
		util.WriteJSON(w, http.StatusOK, map[string]any{"settings": maskRecoverySettings(a.recovery.Settings())})
	case parts[0] == "settings" && len(parts) == 1 && r.Method == http.MethodPost:
		body, err := readJSONMap(r)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		current := a.recovery.Settings()
		if v, ok := body["outlook_api_key"]; ok {
			body["outlook_api_key"] = service.UnmaskSecret(util.Clean(v), current.OutlookAPIKey)
		}
		unmaskRecoveryCFMails(body, current)
		settings, err := a.recovery.UpdateSettings(body)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"settings": maskRecoverySettings(settings)})
	case parts[0] == "cloudflare-mails" && len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost:
		body, err := readJSONMap(r)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		result, err := a.recovery.ProbeRecoveryCFMail(util.Clean(body["id"]), &service.RecoveryCFMail{
			BaseURL:       util.Clean(body["base_url"]),
			AdminPassword: util.Clean(body["admin_password"]),
		})
		if err != nil {
			util.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		util.WriteJSON(w, http.StatusOK, result)
	case parts[0] == "auto-run" && len(parts) == 1 && r.Method == http.MethodPost:
		util.WriteJSON(w, http.StatusOK, map[string]any{"queued": a.recovery.RunAutoHeal()})
	case parts[0] == "jobs" && len(parts) == 1 && r.Method == http.MethodGet:
		util.WriteJSON(w, http.StatusOK, map[string]any{"items": a.recovery.ListJobs()})
	case parts[0] == "jobs" && len(parts) == 1 && r.Method == http.MethodPost:
		body, err := readJSONMap(r)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		emails := util.AsStringSlice(body["emails"])
		if text := util.Clean(body["emails_text"]); text != "" {
			emails = append(emails, text)
		}
		emails = append(emails, a.recovery.EmailsForAccountIDs(util.AsStringSlice(body["account_ids"]))...)
		job, err := a.recovery.CreateJob(util.Clean(body["name"]), emails, util.ToInt(body["concurrency"], 0), "manual")
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"item": a.recovery.GetJob(job.ID)})
	case parts[0] == "jobs" && len(parts) == 2 && r.Method == http.MethodGet:
		job := a.recovery.GetJob(parts[1])
		if job == nil {
			util.WriteError(w, http.StatusNotFound, "job not found")
			return
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"item": job})
	case parts[0] == "jobs" && len(parts) == 2 && r.Method == http.MethodDelete:
		if !a.recovery.DeleteJob(parts[1]) {
			util.WriteError(w, http.StatusNotFound, "job not found")
			return
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	case parts[0] == "jobs" && len(parts) == 3 && r.Method == http.MethodPost:
		body, _ := readJSONMap(r)
		if body == nil {
			body = map[string]any{}
		}
		itemIDs := util.AsStringSlice(body["item_ids"])
		switch parts[2] {
		case "retry":
			n, err := a.recovery.Retry(parts[1], itemIDs)
			if err != nil {
				util.WriteError(w, http.StatusNotFound, err.Error())
				return
			}
			util.WriteJSON(w, http.StatusOK, map[string]any{"retried": n, "item": a.recovery.GetJob(parts[1])})
		case "cancel":
			n, err := a.recovery.Cancel(parts[1], itemIDs)
			if err != nil {
				util.WriteError(w, http.StatusNotFound, err.Error())
				return
			}
			util.WriteJSON(w, http.StatusOK, map[string]any{"cancelled": n, "item": a.recovery.GetJob(parts[1])})
		case "codes":
			codes := map[string]string{}
			for k, v := range util.StringMap(body["codes"]) {
				codes[k] = util.Clean(v)
			}
			accepted, errs := a.recovery.SubmitCodes(parts[1], codes)
			util.WriteJSON(w, http.StatusOK, map[string]any{"accepted": accepted, "errors": errs, "item": a.recovery.GetJob(parts[1])})
		default:
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

// ── account detail / credential actions ─────────────────────────────────────

// handleAccountDetail serves /api/accounts/{id}/... routes. It returns false
// when the path is not an account-detail route.
func (a *App) handleAccountDetail(w http.ResponseWriter, r *http.Request) bool {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/accounts"))
	if len(parts) == 1 && parts[0] == "export" && r.Method == http.MethodPost {
		body, _ := readJSONMap(r)
		if body == nil || !util.ToBool(body["confirm"]) {
			util.WriteError(w, http.StatusBadRequest, "导出包含明文密码，需确认 confirm=true")
			return true
		}
		items := a.accounts.ExportAccountsByIDs(util.AsStringSlice(body["account_ids"]))
		a.logs.Add("导出账号凭证", map[string]any{"module": "accounts", "operation_type": "导出", "count": len(items)})
		if strings.EqualFold(util.Clean(body["format"]), "csv") {
			text, err := service.AccountsToCSV(items)
			if err != nil {
				util.WriteError(w, http.StatusInternalServerError, err.Error())
				return true
			}
			util.WriteJSON(w, http.StatusOK, map[string]any{"format": "csv", "content": text, "count": len(items)})
			return true
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"format": "json", "items": items, "count": len(items)})
		return true
	}
	if len(parts) == 1 && parts[0] == "import-credentials" && r.Method == http.MethodPost {
		body, err := readJSONMap(r)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, "invalid json body")
			return true
		}
		items, err := service.ParseAccountTransfer(util.Clean(body["format"]), util.Clean(body["content"]))
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return true
		}
		util.WriteJSON(w, http.StatusOK, a.accounts.ImportAccountCredentials(items))
		return true
	}
	if len(parts) < 2 || parts[0] != "detail" {
		return false
	}
	id := parts[1]
	action := ""
	if len(parts) > 2 {
		action = parts[2]
	}
	token := a.accounts.GetTokenByID(id)
	if token == "" {
		util.WriteError(w, http.StatusNotFound, "account not found")
		return true
	}
	writeDetail := func(nextID string) {
		detail, err := a.accounts.AccountDetail(nextID)
		if err != nil {
			util.WriteError(w, http.StatusNotFound, err.Error())
			return
		}
		creds := a.accounts.AccountCredentials(a.accounts.GetTokenByID(nextID))
		detail["mail_route"] = a.recovery.RouteForAccount(util.Clean(creds["email"]), creds)
		util.WriteJSON(w, http.StatusOK, map[string]any{"item": detail})
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		writeDetail(id)
	case action == "" && r.Method == http.MethodPost:
		body, err := readJSONMap(r)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, "invalid json body")
			return true
		}
		if _, err := a.accounts.UpdateAccountCredentials(id, body); err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return true
		}
		writeDetail(id)
	case action == "mail" && r.Method == http.MethodPost:
		creds := a.accounts.AccountCredentials(token)
		message, err := a.recovery.ReadLatestMail(util.Clean(creds["email"]), creds)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return true
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"mail": message})
	case action == "refresh" && r.Method == http.MethodPost:
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		newToken, err := a.accounts.RefreshAccountNow(ctx, token)
		if err != nil {
			util.WriteError(w, http.StatusBadGateway, err.Error())
			return true
		}
		writeDetail(util.SHA1Short(newToken, 16))
	case action == "token" && r.Method == http.MethodPost:
		body, err := readJSONMap(r)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, "invalid json body")
			return true
		}
		accessToken := util.Clean(body["access_token"])
		sessionToken := util.Clean(body["session_token"])
		if sessionJSON := util.Clean(body["session_json"]); sessionJSON != "" {
			parsed := map[string]any{}
			if err := json.Unmarshal([]byte(sessionJSON), &parsed); err != nil {
				util.WriteError(w, http.StatusBadRequest, "session_json 不是有效 JSON")
				return true
			}
			accessToken = firstNonEmpty(accessToken, util.Clean(parsed["accessToken"]), util.Clean(parsed["access_token"]))
			sessionToken = firstNonEmpty(sessionToken, util.Clean(parsed["sessionToken"]), util.Clean(parsed["session_token"]))
		}
		newToken, err := a.accounts.ReplaceAccountTokens(id, accessToken, sessionToken)
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return true
		}
		writeDetail(util.SHA1Short(newToken, 16))
	case (action == "relogin" || action == "otp-login") && r.Method == http.MethodPost:
		// Both run through the recovery queue so they are visible as jobs.
		creds := a.accounts.AccountCredentials(token)
		email := util.Clean(creds["email"])
		if email == "" {
			util.WriteError(w, http.StatusBadRequest, "账号没有邮箱，无法登录")
			return true
		}
		if action == "relogin" && util.Clean(creds["password"]) == "" {
			util.WriteError(w, http.StatusBadRequest, "账号没有保存密码")
			return true
		}
		job, err := a.recovery.CreateJob("单账号登录 "+email, []string{email}, 1, "manual")
		if err != nil {
			util.WriteError(w, http.StatusBadRequest, err.Error())
			return true
		}
		util.WriteJSON(w, http.StatusOK, map[string]any{"job": a.recovery.GetJob(job.ID)})
	default:
		http.NotFound(w, r)
	}
	return true
}

// ── dashboard ───────────────────────────────────────────────────────────────

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireIdentity(w, r, ""); !ok {
		return
	}
	keys := a.apiKeys.List()
	top := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		top = append(top, map[string]any{
			"id": k["id"], "name": k["name"], "key_prefix": k["key_prefix"], "enabled": k["enabled"],
			"today_requests": k["today_requests"], "today_images": k["today_images"],
			"used_requests": k["used_requests"], "used_images": k["used_images"],
			"daily_limit": k["daily_limit"], "total_limit": k["total_limit"], "image_limit": k["image_limit"],
			"last_used_at": k["last_used_at"],
		})
	}
	util.WriteJSON(w, http.StatusOK, map[string]any{
		"accounts": a.accounts.AccountStatusCounts(),
		"keys":     a.apiKeys.UsageSummary(),
		"key_list": top,
		"recovery": a.recovery.Summary(),
	})
}

// ── secret masking ──────────────────────────────────────────────────────────

func maskConfigSecrets(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	out := util.CopyMap(config)
	if v := util.Clean(out["sentinel_solver_key"]); v != "" {
		out["sentinel_solver_key"] = service.MaskSecret(v)
	}
	return out
}

// maskRegisterSecrets masks mail provider admin passwords and API keys in the
// register configuration returned to the panel.
func maskRegisterSecrets(register map[string]any) map[string]any {
	if register == nil {
		return nil
	}
	out := util.CopyMap(register)
	mail := util.CopyMap(util.StringMap(out["mail"]))
	if len(mail) == 0 {
		return out
	}
	providers := util.AsMapSlice(mail["providers"])
	masked := make([]any, 0, len(providers))
	for _, provider := range providers {
		next := util.CopyMap(provider)
		for _, field := range registerSecretFields {
			if v := util.Clean(next[field]); v != "" {
				next[field] = service.MaskSecret(v)
			}
		}
		masked = append(masked, next)
	}
	mail["providers"] = masked
	out["mail"] = mail
	return out
}

var registerSecretFields = []string{"admin_password", "api_key"}

// unmaskRegisterSecrets restores secrets the client echoed back masked.
func unmaskRegisterSecrets(body, current map[string]any) {
	mail := util.StringMap(body["mail"])
	if mail == nil {
		return
	}
	currentProviders := util.AsMapSlice(util.StringMap(current["mail"])["providers"])
	providers := util.AsMapSlice(mail["providers"])
	restored := make([]any, 0, len(providers))
	for i, provider := range providers {
		next := util.CopyMap(provider)
		for _, field := range registerSecretFields {
			incoming := util.Clean(next[field])
			if incoming == "" {
				continue
			}
			if i < len(currentProviders) {
				if v := util.Clean(currentProviders[i][field]); v != "" && service.MaskSecret(v) == incoming {
					next[field] = v
					continue
				}
			}
			for _, cur := range currentProviders {
				if v := util.Clean(cur[field]); v != "" && service.MaskSecret(v) == incoming {
					next[field] = v
					break
				}
			}
		}
		restored = append(restored, next)
	}
	mail["providers"] = restored
	body["mail"] = mail
}

func maskRecoverySettings(settings service.RecoverySettings) service.RecoverySettings {
	settings.OutlookAPIKey = service.MaskSecret(settings.OutlookAPIKey)
	entries := make([]service.RecoveryCFMail, len(settings.CloudflareMails))
	for i, e := range settings.CloudflareMails {
		e.AdminPassword = service.MaskSecret(e.AdminPassword)
		e.Domains = append([]string(nil), e.Domains...)
		entries[i] = e
	}
	settings.CloudflareMails = entries
	return settings
}

// unmaskRecoveryCFMails restores admin passwords the client echoed back
// masked, matching entries by id.
func unmaskRecoveryCFMails(body map[string]any, current service.RecoverySettings) {
	raw, ok := body["cloudflare_mails"]
	if !ok {
		return
	}
	stored := map[string]string{}
	for _, e := range current.CloudflareMails {
		stored[e.ID] = e.AdminPassword
	}
	list := util.AsMapSlice(raw)
	out := make([]any, 0, len(list))
	for _, item := range list {
		next := map[string]any{}
		for k, v := range item {
			next[k] = v
		}
		if pw, ok := stored[util.Clean(item["id"])]; ok {
			next["admin_password"] = service.UnmaskSecret(util.Clean(item["admin_password"]), pw)
		}
		out = append(out, next)
	}
	body["cloudflare_mails"] = out
}
