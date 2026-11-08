package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRouteMailChannelByDomain(t *testing.T) {
	mail := map[string]any{"providers": []any{
		map[string]any{"type": "cloudflare_temp_email", "enable": true, "api_base": "https://cf.example", "domain": []string{"cf-a.test"}},
		map[string]any{"type": "cloudflare_temp_email", "enable": true, "api_base": "https://cf2.example", "domain": []string{"cf-b.test"}},
		map[string]any{"type": "gptmail", "enable": true, "api_key": "k"},
	}}
	settings := defaultRecoverySettings()
	settings.OutlookBaseURL = "https://outlook.example"
	settings.OutlookAPIKey = "key"
	cases := []struct {
		email, channel, ref string
	}{
		{"a@cf-a.test", MailChannelCloudflare, "cloudflare_temp_email#1"},
		{"a@CF-B.test", MailChannelCloudflare, "cloudflare_temp_email#2"},
		{"a@outlook.com", MailChannelOutlook, "outlookEmail"},
		{"a@hotmail.co.uk", MailChannelOutlook, "outlookEmail"},
		{"a@outlook.jp", MailChannelOutlook, "outlookEmail"},
		{"a@live.com", MailChannelGPTMail, "gptmail#3"},
		{"a@random.dev", MailChannelGPTMail, "gptmail#3"},
	}
	for _, c := range cases {
		route := RouteMailChannel(c.email, mail, settings, "", "")
		if route.Channel != c.channel || route.EntryRef != c.ref || !route.Ready {
			t.Fatalf("%s routed to %#v, want %s/%s", c.email, route, c.channel, c.ref)
		}
	}
	settings.OutlookDomains = append(settings.OutlookDomains, "live.com")
	if route := RouteMailChannel("a@live.com", mail, settings, "", ""); route.Channel != MailChannelOutlook {
		t.Fatalf("configured live.com route = %#v", route)
	}
	if route := RouteMailChannel("a@random.dev", mail, settings, MailChannelOutlook, ""); route.Channel != MailChannelOutlook || !route.Override {
		t.Fatalf("override route = %#v", route)
	}
	// Unconfigured routed channel reports a reason and never falls back.
	settings.OutlookAPIKey = ""
	if route := RouteMailChannel("a@outlook.com", mail, settings, "", ""); route.Channel != MailChannelOutlook || route.Ready || route.Reason == "" {
		t.Fatalf("unconfigured outlook route = %#v", route)
	}
	noGPT := map[string]any{"providers": []any{mail["providers"].([]any)[0]}}
	if route := RouteMailChannel("a@random.dev", noGPT, settings, "", ""); route.Channel != MailChannelGPTMail || route.Ready {
		t.Fatalf("missing gptmail route = %#v", route)
	}
}

func newTestRecovery(t *testing.T, mail map[string]any) *AccountRecovery {
	t.Helper()
	r := NewAccountRecovery(newTestAccountService(t), nil, nil)
	r.mailConfigFn = func() map[string]any { return mail }
	r.fetchInterval = 50 * time.Millisecond
	return r
}

func cfMail(id int, created time.Time, code string) map[string]any {
	return map[string]any{
		"id": id, "address": "user@cf.test", "subject": "Your ChatGPT code is " + code,
		"text": "Your ChatGPT code is " + code, "created_at": created.UTC().Format("2006-01-02 15:04:05"),
	}
}

func TestRecoveryFetchCodeCloudflareJWTAndAdminFallback(t *testing.T) {
	start := time.Now()
	var jwtCalls, adminCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/mails":
			atomic.AddInt32(&jwtCalls, 1)
			if req.Header.Get("Authorization") != "Bearer good-jwt" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"expired"}`))
				return
			}
			// One stale code then a fresh one.
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{
				cfMail(2, start.Add(2*time.Second), "222222"),
				cfMail(1, start.Add(-10*time.Minute), "111111"),
			}})
		case "/admin/mails":
			atomic.AddInt32(&adminCalls, 1)
			if req.Header.Get("x-admin-auth") != "admin-pass" || req.URL.Query().Get("address") != "user@cf.test" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{cfMail(9, start.Add(3*time.Second), "333333")}})
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	mail := map[string]any{"request_timeout": 2, "providers": []any{
		map[string]any{"type": "cloudflare_temp_email", "enable": true, "api_base": server.URL, "admin_password": "admin-pass", "domain": []string{"cf.test"}},
	}}
	r := newTestRecovery(t, mail)

	code, err := r.defaultFetchCode(context.Background(), "user@cf.test", map[string]any{"mail_provider": "cloudflare_temp_email", "mail_ref": "cloudflare_temp_email#1", "mail_token": "good-jwt"}, start, nil, time.Second)
	if err != nil || code != "222222" {
		t.Fatalf("jwt fetch code = %q err = %v", code, err)
	}
	if atomic.LoadInt32(&adminCalls) != 0 {
		t.Fatalf("admin API used although JWT worked")
	}
	code, err = r.defaultFetchCode(context.Background(), "user@cf.test", map[string]any{"mail_token": "expired-jwt"}, start, nil, time.Second)
	if err != nil || code != "333333" {
		t.Fatalf("admin fallback code = %q err = %v", code, err)
	}
	// Only stale codes -> timeout with a clear reason.
	_, err = r.defaultFetchCode(context.Background(), "user@cf.test", map[string]any{"mail_token": "good-jwt"}, start.Add(time.Hour), nil, 200*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "cloudflare_temp_email") {
		t.Fatalf("stale-only err = %v", err)
	}
}

func TestRecoveryFetchCodeGPTMail(t *testing.T) {
	start := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-API-Key") != "gpt-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case req.URL.Path == "/api/emails":
			if req.URL.Query().Get("email") != "user@gm.test" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"emails": []any{
				map[string]any{"id": "m2", "subject": "Your ChatGPT code is 444555", "timestamp": start.Add(time.Second).Unix()},
			}}})
		case strings.HasPrefix(req.URL.Path, "/api/email/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "m2", "subject": "Your ChatGPT code is 444555", "content": "Your ChatGPT code is 444555", "timestamp": start.Add(time.Second).Unix()}})
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	mail := map[string]any{"providers": []any{map[string]any{"type": "gptmail", "enable": true, "api_key": "gpt-key"}}}
	r := newTestRecovery(t, mail)
	r.newProvider = func(mailConfig map[string]any, name, ref string) (registerMailboxProvider, error) {
		entry, err := selectRegisterMailEntry(mailConfig, name, ref)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			return http.DefaultTransport.RoundTrip(req)
		})}
		return &registerGPTMailProvider{registerHTTPMailProvider: registerHTTPMailProvider{client: client, conf: registerMailSettingsFromConfig(mailConfig)}, entry: entry}, nil
	}
	code, err := r.defaultFetchCode(context.Background(), "user@gm.test", map[string]any{"mail_provider": "gptmail"}, start, nil, time.Second)
	if err != nil || code != "444555" {
		t.Fatalf("gptmail code = %q err = %v", code, err)
	}
	if _, err := r.defaultFetchCode(context.Background(), "user@gm.test", nil, start.Add(time.Hour), nil, 150*time.Millisecond); err == nil {
		t.Fatalf("expected timeout for stale gptmail code")
	}
}

func TestRecoveryFetchCodeOutlookEmail(t *testing.T) {
	start := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/external/emails" || req.Header.Get("X-API-Key") != "ol-key" || req.URL.Query().Get("folder") != "all" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"emails": []any{
			map[string]any{"id": "x1", "subject": "Newsletter 123456", "from": "news@shop.test", "date": start.Add(time.Second).UTC().Format(time.RFC3339)},
			map[string]any{"id": "x2", "subject": "Your ChatGPT code is 987654", "from": map[string]any{"emailAddress": map[string]any{"address": "noreply@tm.openai.com"}}, "date": start.Add(2 * time.Second).UTC().Format(time.RFC3339)},
		}}})
	}))
	defer server.Close()
	r := newTestRecovery(t, map[string]any{})
	r.UpdateSettings(map[string]any{"outlook_base_url": server.URL, "outlook_api_key": "ol-key"})
	code, err := r.defaultFetchCode(context.Background(), "someone@hotmail.com", nil, start, nil, time.Second)
	if err != nil || code != "987654" {
		t.Fatalf("outlook code = %q err = %v", code, err)
	}
}

func TestRecoveryJobAwaitsCodeWhenChannelUnconfigured(t *testing.T) {
	r := newTestRecovery(t, map[string]any{})
	r.startOTPFn = func(ctx context.Context, email string) (*recoverySession, error) {
		return &recoverySession{email: email, createdAt: time.Now()}, nil
	}
	var validated string
	r.validateFn = func(session *recoverySession, code string) (string, string, error) {
		validated = code
		return "new-at-" + code, "new-st", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	job, err := r.CreateJob("t", []string{"x@outlook.com"}, 2, "manual")
	if err != nil {
		t.Fatal(err)
	}
	item := waitRecoveryItem(t, r, job.ID, RecoveryAwaitingCode)
	if item["channel"] != MailChannelOutlook || !strings.Contains(item["error"].(string), "outlookEmail") {
		t.Fatalf("awaiting item = %#v", item)
	}
	if n, errs := r.SubmitCodes(job.ID, map[string]string{"x@outlook.com": "135790"}); n != 1 || len(errs) != 0 {
		t.Fatalf("submit = %d %#v", n, errs)
	}
	waitRecoveryItem(t, r, job.ID, RecoverySucceeded)
	if validated != "135790" {
		t.Fatalf("validated code = %q", validated)
	}
	if token := r.accounts.AccountTokenByEmail("x@outlook.com"); token != "new-at-135790" {
		t.Fatalf("recovered token = %q", token)
	}
}

func waitRecoveryItem(t *testing.T, r *AccountRecovery, jobID, status string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job := r.GetJob(jobID)
		items, _ := job["items"].([]any)
		if len(items) > 0 {
			item := items[0].(map[string]any)
			if item["status"] == status {
				return item
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s never reached %s: %#v", jobID, status, r.GetJob(jobID))
	return nil
}
