package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chatgpt2api/internal/service"
)

func doJSON(t *testing.T, app *App, method, path, auth, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	payload := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &payload)
	return res, payload
}

func TestAdminOnlyAuthAndSPA(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	// Legacy user credentials can no longer reach the admin API.
	_, rawUserKey, err := app.auth.CreateAPIKey(service.AuthRoleUser, "legacy", service.AuthOwner{})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if res, _ := doJSON(t, app, http.MethodGet, "/api/accounts", "Bearer "+rawUserKey, ""); res.Code != http.StatusForbidden {
		t.Fatalf("user key on admin api status = %d", res.Code)
	}
	for _, path := range []string{"/api/admin/users", "/api/admin/roles", "/api/profile", "/auth/register"} {
		if res, _ := doJSON(t, app, http.MethodGet, path, adminAuthHeader(t, app), ""); res.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, res.Code)
		}
	}
	if res, _ := doJSON(t, app, http.MethodGet, "/health", "", ""); res.Code != http.StatusOK {
		t.Fatalf("/health status = %d", res.Code)
	}
	for _, path := range []string{"/", "/settings"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		app.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `<div id="root"></div>`) {
			t.Fatalf("%s status/body = %d %q", path, res.Code, res.Body.String())
		}
	}
}

func TestStandaloneAPIKeyLifecycleAndLimits(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()
	admin := adminAuthHeader(t, app)

	res, created := doJSON(t, app, http.MethodPost, "/api/keys", admin, `{"name":"client","key":"my-custom-key-1","daily_limit":1,"models":"gpt-4o"}`)
	if res.Code != http.StatusOK || created["key"] != "my-custom-key-1" {
		t.Fatalf("create key status = %d body = %#v", res.Code, created)
	}
	item := created["item"].(map[string]any)
	if _, leaked := item["key_hash"]; leaked {
		t.Fatalf("key hash leaked: %#v", item)
	}
	id := item["id"].(string)

	// Model whitelist.
	res, body := doJSON(t, app, http.MethodPost, "/v1/messages", "Bearer my-custom-key-1", `{"model":"claude-x"}`)
	if res.Code != http.StatusForbidden || body["error"].(map[string]any)["code"] != "model_not_allowed" {
		t.Fatalf("whitelist status = %d body = %#v", res.Code, body)
	}
	// x-api-key auth reaches validation for an allowed model; consumes the daily request.
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	req.Header.Set("x-api-key", "my-custom-key-1")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden || rec.Code == http.StatusTooManyRequests {
		t.Fatalf("allowed request status = %d body = %s", rec.Code, rec.Body.String())
	}
	res, body = doJSON(t, app, http.MethodPost, "/v1/messages", "Bearer my-custom-key-1", `{"model":"gpt-4o"}`)
	if res.Code != http.StatusTooManyRequests || body["error"].(map[string]any)["code"] != "daily_limit_exceeded" {
		t.Fatalf("daily limit status = %d body = %#v", res.Code, body)
	}
	// Reset usage then disable.
	if res, _ := doJSON(t, app, http.MethodPost, "/api/keys/"+id+"/reset-usage", admin, `{}`); res.Code != http.StatusOK {
		t.Fatalf("reset status = %d", res.Code)
	}
	if res, _ := doJSON(t, app, http.MethodPost, "/api/keys/"+id, admin, `{"enabled":false}`); res.Code != http.StatusOK {
		t.Fatalf("update status = %d", res.Code)
	}
	res, body = doJSON(t, app, http.MethodPost, "/v1/messages", "Bearer my-custom-key-1", `{"model":"gpt-4o"}`)
	if res.Code != http.StatusForbidden || body["error"].(map[string]any)["code"] != "api_key_disabled" {
		t.Fatalf("disabled status = %d body = %#v", res.Code, body)
	}
	// Unknown keys are rejected OpenAI-style.
	res, body = doJSON(t, app, http.MethodPost, "/v1/chat/completions", "Bearer nope", `{}`)
	if res.Code != http.StatusUnauthorized || body["error"] == nil {
		t.Fatalf("unknown key status = %d body = %#v", res.Code, body)
	}
}

func TestSettingsMaskSentinelSolverKey(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()
	admin := adminAuthHeader(t, app)
	if res, _ := doJSON(t, app, http.MethodPost, "/api/settings", admin, `{"sentinel_solver_key":"super-secret-solver-key"}`); res.Code != http.StatusOK {
		t.Fatalf("save status = %d", res.Code)
	}
	_, body := doJSON(t, app, http.MethodGet, "/api/settings", admin, "")
	masked := body["config"].(map[string]any)["sentinel_solver_key"]
	if masked == "super-secret-solver-key" || masked != service.MaskSecret("super-secret-solver-key") {
		t.Fatalf("masked = %#v", masked)
	}
	// Echoing the mask keeps the stored secret.
	if res, _ := doJSON(t, app, http.MethodPost, "/api/settings", admin, `{"sentinel_solver_key":"`+masked.(string)+`"}`); res.Code != http.StatusOK {
		t.Fatalf("resave status = %d", res.Code)
	}
	if got := app.config.SentinelSolverKey(); got != "super-secret-solver-key" {
		t.Fatalf("stored key = %q", got)
	}
}
