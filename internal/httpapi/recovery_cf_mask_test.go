package httpapi

import (
	"testing"

	"chatgpt2api/internal/service"
)

func TestRecoveryCFMailMaskAndUnmask(t *testing.T) {
	current := service.RecoverySettings{CloudflareMails: []service.RecoveryCFMail{
		{ID: "a", Name: "A", BaseURL: "https://a", AdminPassword: "secret-password-a", Domains: []string{"a.example"}, Enabled: true},
	}}
	masked := maskRecoverySettings(current)
	if masked.CloudflareMails[0].AdminPassword == "secret-password-a" || masked.CloudflareMails[0].AdminPassword == "" {
		t.Fatalf("not masked: %q", masked.CloudflareMails[0].AdminPassword)
	}
	if current.CloudflareMails[0].AdminPassword != "secret-password-a" {
		t.Fatal("masking mutated the source settings")
	}
	body := map[string]any{"cloudflare_mails": []any{
		map[string]any{"id": "a", "admin_password": masked.CloudflareMails[0].AdminPassword},
		map[string]any{"id": "", "admin_password": "brand-new"},
	}}
	unmaskRecoveryCFMails(body, current)
	list := body["cloudflare_mails"].([]any)
	if list[0].(map[string]any)["admin_password"] != "secret-password-a" || list[1].(map[string]any)["admin_password"] != "brand-new" {
		t.Fatalf("unmask = %#v", list)
	}
}
