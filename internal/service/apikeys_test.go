package service

import (
	"testing"
	"time"
)

func TestAPIKeyLimitsAndDailyReset(t *testing.T) {
	s := NewAPIKeyService(newTestStorageBackend(t))
	now := time.Date(2026, 1, 1, 23, 59, 0, 0, apiKeyUsageZone)
	s.now = func() time.Time { return now }
	item, raw, err := s.Create(map[string]any{"name": "k", "daily_limit": 2, "image_limit": 3, "concurrency_limit": 1, "rpm_limit": 10})
	if err != nil || raw == "" {
		t.Fatalf("create: %v", err)
	}
	id := item["id"].(string)
	if s.Lookup(raw) == nil || s.Lookup(raw+"x") != nil {
		t.Fatalf("lookup mismatch")
	}
	lease, err := s.Admit(id, "gpt-image-2", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Admit(id, "", 0); err == nil {
		t.Fatalf("concurrency limit not enforced")
	}
	lease.Finish(2)
	if _, err := s.Admit(id, "", 2); err == nil {
		t.Fatalf("image limit not enforced")
	}
	l2, err := s.Admit(id, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	l2.Finish(1)
	if _, err := s.Admit(id, "", 0); err == nil {
		t.Fatalf("daily limit not enforced")
	}
	now = now.Add(2 * time.Minute) // next day in Asia/Shanghai
	if _, err := s.Admit(id, "", 0); err != nil {
		t.Fatalf("daily counter not reset: %v", err)
	}
	// Persisted across reloads.
	s.Flush()
}

func TestMigrateLegacyKeysKeepsHash(t *testing.T) {
	s := NewAPIKeyService(newTestStorageBackend(t))
	n := s.MigrateLegacyKeys([]map[string]any{
		{"kind": AuthKindAPIKey, "role": AuthRoleUser, "key_hash": "abc", "key": "sk-legacy-123456", "name": "u"},
		{"kind": AuthKindAPIKey, "role": AuthRoleAdmin, "key_hash": "def"},
		{"kind": AuthKindSession, "role": AuthRoleUser, "key_hash": "ghi"},
	})
	if n != 1 || s.MigrateLegacyKeys([]map[string]any{{"kind": AuthKindAPIKey, "role": AuthRoleUser, "key_hash": "abc"}}) != 0 {
		t.Fatalf("migrated = %d", n)
	}
}

func TestSecretBoxRoundTripAndAccountMigration(t *testing.T) {
	SetSecretKey("test-secret")
	t.Cleanup(func() { SetSecretKey("") })
	enc, err := EncryptSecret("hunter2")
	if err != nil || !IsEncryptedSecret(enc) {
		t.Fatalf("encrypt = %q %v", enc, err)
	}
	if plain, err := DecryptSecret(enc); err != nil || plain != "hunter2" {
		t.Fatalf("decrypt = %q %v", plain, err)
	}
	if UnmaskSecret(MaskSecret("abcdefghijkl"), "abcdefghijkl") != "abcdefghijkl" || UnmaskSecret("new", "abcdefghijkl") != "new" {
		t.Fatalf("unmask mismatch")
	}
	backend := newTestStorageBackend(t)
	_ = backend.SaveAccounts([]map[string]any{{"access_token": "at-1", "email": "a@b.c", "password": "plain-pass"}})
	accounts := NewAccountService(backend, testAccountConfig{}, nil, NewLogService(backend))
	if !accounts.MigrateAccountSecrets() {
		t.Fatalf("expected migration")
	}
	stored, _ := backend.LoadAccounts()
	if !IsEncryptedSecret(stored[0]["password"].(string)) {
		t.Fatalf("stored password not sealed: %#v", stored[0]["password"])
	}
	if accounts.AccountCredentials("at-1")["password"] != "plain-pass" {
		t.Fatalf("in-memory password not plaintext")
	}
}

func TestAccountCredentialImportExport(t *testing.T) {
	accounts := newTestAccountService(t)
	accounts.AddAccounts([]string{"at-1"})
	accounts.UpdateAccount("at-1", map[string]any{"email": "a@b.c"})
	items, err := ParseAccountTransfer("", "email,password,access_token,mail_provider\na@b.c,pw1,,gptmail\nn@b.c,pw2,at-2,\nbad,,,\nz@b.c,pw3,,\n")
	if err != nil {
		t.Fatal(err)
	}
	res := accounts.ImportAccountCredentials(items)
	if res["added"] != 1 || res["updated"] != 1 || res["skipped"] != 2 {
		t.Fatalf("import result = %#v", res)
	}
	exported := accounts.ExportAccountsByIDs(nil)
	csvText, err := AccountsToCSV(exported)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseAccountTransfer("csv", csvText)
	if err != nil || len(back) != 2 {
		t.Fatalf("roundtrip = %d %v", len(back), err)
	}
	if accounts.AccountCredentials("at-1")["password"] != "pw1" {
		t.Fatalf("password not imported")
	}
}
