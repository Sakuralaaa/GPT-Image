package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeOutlookEmail struct {
	mu       sync.Mutex
	started  []map[string]any
	claims   int
	reports  map[string]map[string]any
	messages []any
	moved    []map[string]any
	tagged   []map[string]any
	tagNames []string
	groups   []map[string]any
}

func (f *fakeOutlookEmail) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login", "/api/csrf-token", "/api/groups", "/api/tags", "/api/accounts/tags":
			f.session(t, w, r)
			return
		}
		if r.Header.Get("X-API-Key") != "oe-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "bad key"})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		var body map[string]any
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		switch r.URL.Path {
		case "/api/external/projects/start":
			f.started = append(f.started, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/external/projects/gpt-image/claim-random":
			f.claims++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
				"email": "pool@outlook.com", "account_id": 12, "claim_token": "pclm_x",
			}})
		case "/api/external/projects/gpt-image/complete-success", "/api/external/projects/gpt-image/complete-failed", "/api/external/projects/gpt-image/release":
			if f.reports == nil {
				f.reports = map[string]map[string]any{}
			}
			f.reports[r.URL.Path] = body
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/external/accounts":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "accounts": []any{
				map[string]any{"id": 12, "group_id": 3, "group_name": "微软"},
			}})
		case "/api/external/accounts/batch-update-group":
			f.moved = append(f.moved, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "updated_count": 1})
		case "/api/external/emails":
			if r.URL.Query().Get("email") != "pool@outlook.com" {
				t.Errorf("emails query = %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"emails": f.messages}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *fakeOutlookEmail) session(t *testing.T, w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if r.URL.Path == "/login" {
		if body["password"] != "web-pass" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "密码错误"})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "s1", Secure: true, HttpOnly: true})
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		return
	}
	if c, err := r.Cookie("session"); err != nil || c.Value != "s1" {
		w.WriteHeader(http.StatusFound)
		return
	}
	switch {
	case r.URL.Path == "/api/csrf-token":
		_ = json.NewEncoder(w).Encode(map[string]any{"csrf_token": "csrf1"})
	case r.Header.Get("X-CSRFToken") != "csrf1" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "csrf"})
	case r.URL.Path == "/api/tags" && r.Method == http.MethodGet:
		tags := []any{map[string]any{"id": 1, "name": "other"}}
		for i, name := range f.tagNames {
			tags = append(tags, map[string]any{"id": 10 + i, "name": name})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "tags": tags})
	case r.URL.Path == "/api/tags":
		f.tagNames = append(f.tagNames, body["name"].(string))
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "tag": map[string]any{"id": 9 + len(f.tagNames), "name": body["name"]}})
	case r.URL.Path == "/api/accounts/tags":
		f.tagged = append(f.tagged, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	case r.URL.Path == "/api/groups" && r.Method == http.MethodGet:
		groups := []any{map[string]any{"id": 3, "name": "微软"}}
		for _, g := range f.groups {
			groups = append(groups, g)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "groups": groups})
	case r.URL.Path == "/api/groups":
		id := 20 + len(f.groups)
		f.groups = append(f.groups, map[string]any{"id": id, "name": body["name"], "parent_id": body["parent_id"]})
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "group_id": id})
	}
}

func outlookEmailMailConfig(base string) map[string]any {
	return map[string]any{"wait_timeout": 2, "wait_interval": 1, "providers": []any{
		map[string]any{"type": MailProviderOutlookEmail, "enable": true, "api_base": base, "api_key": "oe-key"},
	}}
}

func TestOutlookEmailProviderLeasesMailboxAndReadsFreshCode(t *testing.T) {
	fake := &fakeOutlookEmail{}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	cfg := outlookEmailMailConfig(server.URL)
	now := time.Now()
	fake.messages = []any{
		map[string]any{"id": "old", "subject": "Your ChatGPT code is 111111", "from": "noreply@tm.openai.com", "date": now.Add(-10 * time.Minute).UTC().Format(time.RFC3339)},
		map[string]any{"id": "new", "subject": "Your ChatGPT code is 222222", "from": "noreply@tm.openai.com", "date": now.Add(2 * time.Second).UTC().Format(time.RFC3339)},
	}
	mailbox, err := createRegisterMailbox(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if mailbox["address"] != "pool@outlook.com" || mailbox["provider"] != MailProviderOutlookEmail || mailbox["token"] == "" {
		t.Fatalf("mailbox = %#v", mailbox)
	}
	if len(fake.started) != 1 || fake.started[0]["project_key"] != "gpt-image" || fake.claims != 1 {
		t.Fatalf("start = %#v claims = %d", fake.started, fake.claims)
	}
	code, err := waitRegisterCode(context.Background(), cfg, mailbox)
	if err != nil || code != "222222" {
		t.Fatalf("code = %q err = %v", code, err)
	}
	reportRegisterMailboxResult(cfg, mailbox, context.Background(), nil)
	got := fake.reports["/api/external/projects/gpt-image/complete-success"]
	if got == nil || got["claim_token"] != "pclm_x" || int(got["account_id"].(float64)) != 12 {
		t.Fatalf("reports = %#v", fake.reports)
	}
}

func TestOutlookEmailProviderReportsFailureAndRelease(t *testing.T) {
	fake := &fakeOutlookEmail{}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	cfg := outlookEmailMailConfig(server.URL)
	mailbox, err := createRegisterMailbox(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	reportRegisterMailboxResult(cfg, mailbox, context.Background(), errors.New("boom"))
	if fake.reports["/api/external/projects/gpt-image/complete-failed"]["detail"] != "boom" {
		t.Fatalf("failed report = %#v", fake.reports)
	}
	mailbox2, _ := createRegisterMailbox(cfg, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reportRegisterMailboxResult(cfg, mailbox2, ctx, ctx.Err())
	if fake.reports["/api/external/projects/gpt-image/release"] == nil {
		t.Fatalf("release not reported: %#v", fake.reports)
	}
}

func TestRouteOutlookEmailRegisteredAccountUsesRegisterEntry(t *testing.T) {
	cfg := outlookEmailMailConfig("https://oe.example")
	settings := defaultRecoverySettings()
	route := RouteMailChannel("someone@gmail.com", cfg, settings, "", MailProviderOutlookEmail+"#1")
	if route.Channel != MailChannelOutlook || !route.Ready || route.EntryRef != MailProviderOutlookEmail+"#1" {
		t.Fatalf("route = %#v", route)
	}
}

func TestOutlookEmailScopeByGroupNameAndMoveToDoneGroup(t *testing.T) {
	fake := &fakeOutlookEmail{}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	cfg := outlookEmailMailConfig(server.URL)
	entry := cfg["providers"].([]any)[0].(map[string]any)
	entry["groups"] = "微软"
	entry["done_name"] = "已注册GPT"

	// Without the login password an empty done group cannot be found.
	mailbox, err := createRegisterMailbox(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if ids, _ := fake.started[0]["group_ids"].([]any); len(ids) != 1 || int(ids[0].(float64)) != 3 {
		t.Fatalf("start scope = %#v", fake.started[0])
	}
	reportRegisterMailboxResult(cfg, mailbox, context.Background(), nil)
	if len(fake.moved) != 0 {
		t.Fatalf("moved without resolvable group: %#v", fake.moved)
	}

	// With the password the group is created under the scope group once.
	entry["login_password"] = "web-pass"
	for i := 0; i < 2; i++ {
		mailbox, err := createRegisterMailbox(cfg, "")
		if err != nil {
			t.Fatal(err)
		}
		reportRegisterMailboxResult(cfg, mailbox, context.Background(), nil)
	}
	if len(fake.groups) != 1 || fake.groups[0]["name"] != "已注册GPT" || int(fake.groups[0]["parent_id"].(float64)) != 3 {
		t.Fatalf("groups = %#v", fake.groups)
	}
	if len(fake.moved) != 2 || int(fake.moved[1]["group_id"].(float64)) != 20 {
		t.Fatalf("moved = %#v", fake.moved)
	}
	if len(fake.tagNames) != 1 || fake.tagNames[0] != "已注册GPT" {
		t.Fatalf("tag created = %#v", fake.tagNames)
	}
	if len(fake.tagged) != 2 || int(fake.tagged[1]["tag_id"].(float64)) != 10 || fake.tagged[1]["action"] != "add" {
		t.Fatalf("tagged = %#v", fake.tagged)
	}

	// Failed registrations stay in place; numeric IDs skip lookups.
	entry["done_name"] = "7"
	mailbox, _ = createRegisterMailbox(cfg, "")
	reportRegisterMailboxResult(cfg, mailbox, context.Background(), errors.New("x"))
	if len(fake.moved) != 2 || len(fake.tagged) != 2 {
		t.Fatalf("failed run marked: %#v %#v", fake.moved, fake.tagged)
	}
	mailbox, _ = createRegisterMailbox(cfg, "")
	reportRegisterMailboxResult(cfg, mailbox, context.Background(), nil)
	if int(fake.moved[2]["group_id"].(float64)) != 7 || len(fake.tagNames) != 1 {
		t.Fatalf("numeric move = %#v", fake.moved[2])
	}
}
