package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func recoveryCFSettings() RecoverySettings {
	s := defaultRecoverySettings()
	s.OutlookBaseURL = "https://outlook.example"
	s.OutlookAPIKey = "key"
	s.CloudflareMails = normalizeRecoveryCFMails([]RecoveryCFMail{
		{ID: "a", Name: "CF A", BaseURL: "https://cf-a.example/", AdminPassword: "pw-a", Domains: []string{"@Mail-A.Example", " x.test\ny.test "}, Enabled: true},
		{ID: "b", Name: "CF B", BaseURL: "https://cf-b.example", AdminPassword: "pw-b", Domains: []string{"mail-b.example", "*.wild.example"}, Enabled: true},
		{ID: "c", Name: "CF off", BaseURL: "https://cf-c.example", Domains: []string{"off.example"}, Enabled: false},
	})
	return s
}

func TestRouteRecoveryCFEntries(t *testing.T) {
	legacy := map[string]any{"providers": []any{
		map[string]any{"type": "cloudflare_temp_email", "enable": true, "api_base": "https://reg.example", "domain": []string{"legacy.test", "mail-a.example"}},
		map[string]any{"type": "gptmail", "enable": true, "api_key": "k"},
	}}
	s := recoveryCFSettings()
	cases := []struct {
		email, channel, ref, name, source string
	}{
		{"u@mail-a.example", MailChannelCloudflare, "recovery_cf:a", "CF A", MailRouteSourceRecovery}, // recovery wins over legacy
		{"u@Y.TEST", MailChannelCloudflare, "recovery_cf:a", "CF A", MailRouteSourceRecovery},
		{"u@mail-b.example", MailChannelCloudflare, "recovery_cf:b", "CF B", MailRouteSourceRecovery},
		{"u@deep.sub.wild.example", MailChannelCloudflare, "recovery_cf:b", "CF B", MailRouteSourceRecovery},
		{"u@legacy.test", MailChannelCloudflare, "cloudflare_temp_email#1", "https://reg.example", MailRouteSourceRegister},
		{"u@outlook.com", MailChannelOutlook, "outlookEmail", "outlookEmail", ""},
		{"u@off.example", MailChannelGPTMail, "gptmail#2", "gptmail#2", MailRouteSourceRegister},  // disabled entry ignored
		{"u@wild.example", MailChannelGPTMail, "gptmail#2", "gptmail#2", MailRouteSourceRegister}, // wildcard needs a subdomain
	}
	for _, c := range cases {
		r := RouteMailChannel(c.email, legacy, s, "", "")
		if !r.Ready || r.Channel != c.channel || r.EntryRef != c.ref || r.EntryName != c.name || r.Source != c.source {
			t.Fatalf("%s -> %#v, want %s/%s/%s/%s", c.email, r, c.channel, c.ref, c.name, c.source)
		}
	}
	if r := RouteMailChannel("u@legacy.test", legacy, s, "", ""); r.Reason != legacyCFRouteReason {
		t.Fatalf("legacy reason = %q", r.Reason)
	}
	// Override to CF on an uncovered domain resolves the stored recovery ref.
	if r := RouteMailChannel("u@random.dev", legacy, s, MailChannelCloudflare, "recovery_cf:b"); !r.Ready || !r.Override || r.EntryRef != "recovery_cf:b" {
		t.Fatalf("override via stored ref = %#v", r)
	}
	// Override to CF without any matching entry stays on CF, not ready.
	if r := RouteMailChannel("u@random.dev", legacy, s, MailChannelCloudflare, ""); r.Ready || r.Channel != MailChannelCloudflare || r.Reason == "" {
		t.Fatalf("override without entry = %#v", r)
	}
	// Override away from CF on a recovery-covered domain.
	if r := RouteMailChannel("u@mail-b.example", legacy, s, MailChannelOutlook, ""); r.Channel != MailChannelOutlook || !r.Override {
		t.Fatalf("override to outlook = %#v", r)
	}
	// Exact suffix beats a wildcard on another entry.
	s.CloudflareMails = append(s.CloudflareMails, RecoveryCFMail{ID: "d", Name: "CF D", BaseURL: "https://d", Domains: []string{"a.wild.example"}, Enabled: true})
	if r := RouteMailChannel("u@a.wild.example", legacy, s, "", ""); r.EntryRef != "recovery_cf:d" {
		t.Fatalf("exact vs wildcard = %#v", r)
	}
}

func TestRecoveryCFNormalizeAndValidate(t *testing.T) {
	s := recoveryCFSettings()
	if got := strings.Join(s.CloudflareMails[0].Domains, ","); got != "mail-a.example,x.test,y.test" {
		t.Fatalf("normalized domains = %s", got)
	}
	if s.CloudflareMails[0].BaseURL != "https://cf-a.example" {
		t.Fatalf("base url = %s", s.CloudflareMails[0].BaseURL)
	}
	if err := validateRecoveryCFMails(s.CloudflareMails); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	dup := normalizeRecoveryCFMails([]RecoveryCFMail{
		{Name: "A", BaseURL: "https://a", Domains: []string{"same.example"}, Enabled: true},
		{Name: "B", BaseURL: "https://b", Domains: []string{"SAME.example"}, Enabled: true},
	})
	if dup[0].ID == "" || dup[0].ID == dup[1].ID {
		t.Fatalf("ids not generated: %#v", dup)
	}
	if err := validateRecoveryCFMails(dup); err == nil || !strings.Contains(err.Error(), "same.example") {
		t.Fatalf("duplicate domain err = %v", err)
	}
	if err := validateRecoveryCFMails([]RecoveryCFMail{{ID: "x", Name: "X", Enabled: true, Domains: []string{"x.example"}}}); err == nil {
		t.Fatal("enabled entry without base url accepted")
	}

	r := NewAccountRecovery(newTestAccountService(t), nil, nil)
	if _, err := r.UpdateSettings(map[string]any{"cloudflare_mails": []any{
		map[string]any{"name": "A", "base_url": "https://a", "domains": []string{"d.example"}, "enabled": true},
		map[string]any{"name": "B", "base_url": "https://b", "domains": []string{"d.example"}, "enabled": true},
	}}); err == nil {
		t.Fatal("UpdateSettings accepted duplicate domain")
	}
	if len(r.Settings().CloudflareMails) != 0 {
		t.Fatal("rejected settings were applied")
	}
	got, err := r.UpdateSettings(map[string]any{"cloudflare_mails": []any{
		map[string]any{"name": "A", "base_url": "https://a", "admin_password": "pw", "domains": []string{"d.example"}, "enabled": true},
	}})
	if err != nil || len(got.CloudflareMails) != 1 || got.CloudflareMails[0].ID == "" {
		t.Fatalf("UpdateSettings = %#v, %v", got, err)
	}
	// Unrelated update keeps the entries.
	got, _ = r.UpdateSettings(map[string]any{"otp_wait_seconds": 120})
	if len(got.CloudflareMails) != 1 || got.CloudflareMails[0].AdminPassword != "pw" {
		t.Fatalf("entries lost on partial update: %#v", got.CloudflareMails)
	}
}

func TestRecoverySettingsSealRoundTrip(t *testing.T) {
	SetSecretKey("unit-test-key")
	defer SetSecretKey("")
	s := recoveryCFSettings()
	sealed := sealRecoverySettings(s)
	for i, e := range sealed.CloudflareMails {
		if s.CloudflareMails[i].AdminPassword != "" && (!IsEncryptedSecret(e.AdminPassword) || strings.Contains(e.AdminPassword, s.CloudflareMails[i].AdminPassword)) {
			t.Fatalf("entry %d not sealed: %q", i, e.AdminPassword)
		}
	}
	if !IsEncryptedSecret(sealed.OutlookAPIKey) {
		t.Fatal("outlook key not sealed")
	}
	if s.CloudflareMails[0].AdminPassword != "pw-a" {
		t.Fatal("seal mutated the in-memory settings")
	}
	raw, _ := json.Marshal(sealed)
	if strings.Contains(string(raw), "pw-a") || strings.Contains(string(raw), "pw-b") {
		t.Fatal("plaintext password in stored JSON")
	}
	var decoded RecoverySettings
	_ = json.Unmarshal(raw, &decoded)
	opened := openRecoverySettings(decoded)
	if opened.CloudflareMails[0].AdminPassword != "pw-a" || opened.CloudflareMails[1].AdminPassword != "pw-b" || opened.OutlookAPIKey != "key" {
		t.Fatalf("round trip failed: %#v", opened.CloudflareMails)
	}
	// Legacy plaintext documents still load.
	if openRecoverySettings(s).CloudflareMails[0].AdminPassword != "pw-a" {
		t.Fatal("plaintext passthrough failed")
	}
	if m := MaskSecret("pw-a"); UnmaskSecret(m, "pw-a") != "pw-a" || UnmaskSecret("new-pass", "pw-a") != "new-pass" {
		t.Fatal("mask/unmask round trip")
	}
}

func TestRecoveryFetchCodeRecoveryCFAdmin(t *testing.T) {
	start := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/admin/mails" || req.Header.Get("x-admin-auth") != "pw-r" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if req.URL.Query().Get("address") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "count": 7})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{cfMail(9, start.Add(2*time.Second), "246810")}})
	}))
	defer server.Close()
	r := newTestRecovery(t, map[string]any{"providers": []any{}})
	if _, err := r.UpdateSettings(map[string]any{"cloudflare_mails": []any{
		map[string]any{"id": "r1", "name": "Recovery CF", "base_url": server.URL, "admin_password": "pw-r", "domains": []string{"cf.test"}, "enabled": true},
	}}); err != nil {
		t.Fatal(err)
	}
	route := r.RouteForAccount("user@cf.test", nil)
	if !route.Ready || route.EntryName != "Recovery CF" || route.Source != MailRouteSourceRecovery {
		t.Fatalf("route = %#v", route)
	}
	code, err := r.defaultFetchCode(t.Context(), "user@cf.test", map[string]any{}, start, nil, time.Second)
	if err != nil || code != "246810" {
		t.Fatalf("code = %q, err = %v", code, err)
	}
	// Probe with the masked password resolves to the stored one.
	if res, err := r.ProbeRecoveryCFMail("r1", &RecoveryCFMail{BaseURL: server.URL, AdminPassword: MaskSecret("pw-r")}); err != nil || res["count"] != 7 {
		t.Fatalf("probe = %#v, %v", res, err)
	}
	if _, err := r.ProbeRecoveryCFMail("r1", &RecoveryCFMail{BaseURL: server.URL, AdminPassword: "wrong"}); err == nil {
		t.Fatal("probe with wrong password succeeded")
	}
}
