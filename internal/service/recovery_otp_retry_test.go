package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFreshCodeSkipsRejectedCodes(t *testing.T) {
	now := time.Now()
	messages := []recoveryMailMessage{
		{Ref: "a", Code: "111111", ReceivedAt: now.Add(time.Second)},
		{Ref: "b", Code: "222222", ReceivedAt: now.Add(2 * time.Second)},
	}
	if got := freshCode(messages, now, map[string]struct{}{}); got != "222222" {
		t.Fatalf("freshCode() = %q, want newest", got)
	}
	seen := map[string]struct{}{recoverySeenCodeKey("222222"): {}}
	if got := freshCode(messages, now, seen); got != "111111" {
		t.Fatalf("freshCode() with rejected newest = %q", got)
	}
}

func TestRecoveryFetchCodeWaitsForLaterLoginMail(t *testing.T) {
	start := time.Now()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		emails := []any{
			map[string]any{"id": "m1", "subject": "Your ChatGPT code is 111111", "from": "noreply@tm.openai.com", "date": start.Add(time.Second).UTC().Format(time.RFC3339)},
		}
		if n > 1 {
			// The second send of the same attempt voids the first code.
			emails = append(emails, map[string]any{"id": "m2", "subject": "Your ChatGPT code is 222222", "from": "noreply@tm1.openai.com", "date": start.Add(2 * time.Second).UTC().Format(time.RFC3339)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"emails": emails}})
	}))
	defer server.Close()
	r := newTestRecovery(t, map[string]any{})
	r.UpdateSettings(map[string]any{"outlook_base_url": server.URL, "outlook_api_key": "ol-key"})
	code, err := r.defaultFetchCode(context.Background(), "someone@hotmail.com", nil, start, nil, 2*time.Second)
	if err != nil || code != "222222" {
		t.Fatalf("code = %q err = %v, want the later mail's code", code, err)
	}
}

func TestRecoveryAutoOTPRetriesNewerCodeAfterWrongCode(t *testing.T) {
	r := newTestRecovery(t, map[string]any{})
	r.UpdateSettings(map[string]any{"outlook_base_url": "https://outlook.invalid", "outlook_api_key": "ol-key"})
	r.startOTPFn = func(ctx context.Context, email string) (*recoverySession, error) {
		return &recoverySession{email: email, createdAt: time.Now()}, nil
	}
	var mu sync.Mutex
	var fetchSeen []map[string]struct{}
	r.fetchFn = func(ctx context.Context, email string, creds map[string]any, since time.Time, seen map[string]struct{}, wait time.Duration) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		snapshot := map[string]struct{}{}
		for k := range seen {
			snapshot[k] = struct{}{}
		}
		fetchSeen = append(fetchSeen, snapshot)
		if _, rejected := seen[recoverySeenCodeKey("111111")]; rejected {
			return "222222", nil
		}
		return "111111", nil
	}
	var validated []string
	r.validateFn = func(session *recoverySession, code string) (string, string, error) {
		mu.Lock()
		validated = append(validated, code)
		mu.Unlock()
		if code == "111111" {
			return "", "", errors.New(`OTP 验证失败: 401 - , detail={"error":{"code":"wrong_email_otp_code","message":"Wrong code. Please check it and try again."}}`)
		}
		return "new-at-" + code, "new-st", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	job, err := r.CreateJob("t", []string{"y@outlook.com"}, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	waitRecoveryItem(t, r, job.ID, RecoverySucceeded)
	mu.Lock()
	defer mu.Unlock()
	if len(validated) != 2 || validated[0] != "111111" || validated[1] != "222222" {
		t.Fatalf("validated = %#v", validated)
	}
	if len(fetchSeen) != 2 {
		t.Fatalf("fetch calls = %d", len(fetchSeen))
	}
}

func TestIsWrongOTPError(t *testing.T) {
	if !isWrongOTPError(errors.New(`401 {"code":"wrong_email_otp_code"}`)) {
		t.Fatal("wrong code not detected")
	}
	if isWrongOTPError(errors.New("network timeout")) || isWrongOTPError(nil) {
		t.Fatal("unrelated error detected as wrong code")
	}
}
