package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"chatgpt2api/internal/backend"
	"chatgpt2api/internal/protocol"
	"chatgpt2api/internal/service"
	"chatgpt2api/internal/storage"
	"chatgpt2api/internal/util"
)

func TestCreationTaskFailureWritesCallLog(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	_, rawKey, err := app.auth.CreateAPIKey(service.AuthRoleAdmin, "frontend", service.AuthOwner{})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/creation-tasks/image-generations", strings.NewReader(`{"client_task_id":"task-log-test","prompt":"test image"}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("submit creation task status = %d body = %s", res.Code, res.Body.String())
	}

	var logs map[string]any
	var item map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet, "/api/logs", nil)
		req.Header.Set("Authorization", adminAuthHeader(t, app))
		res = httptest.NewRecorder()
		app.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("logs status = %d body = %s", res.Code, res.Body.String())
		}
		if err := json.Unmarshal(res.Body.Bytes(), &logs); err != nil {
			t.Fatalf("logs json: %v", err)
		}
		item = findLogBySummary(logItems(logs), "文生图调用失败")
		if item != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if item == nil {
		t.Fatalf("expected creation task failure to write a log event, got %#v", logs)
	}
	if _, ok := item["type"]; ok {
		t.Fatalf("log item should not expose type: %#v", item)
	}
	detail, _ := item["detail"].(map[string]any)
	if detail["endpoint"] != "/api/creation-tasks/image-generations" ||
		detail["path"] != "/api/creation-tasks/image-generations" ||
		detail["method"] != http.MethodPost ||
		detail["module"] != "creation-tasks" ||
		detail["outcome"] != "failed" {
		t.Fatalf("unexpected log detail: %#v", detail)
	}
	if _, ok := detail["status"].(float64); !ok {
		t.Fatalf("log status should use numeric HTTP-style status: %#v", detail)
	}
	if detail["key_name"] != "frontend" || detail["key_role"] != "admin" {
		t.Fatalf("call log did not include user key identity: %#v", detail)
	}
}

func TestLogsEndpointUsesDefaultLogView(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()
	if _, err := app.config.Update(map[string]any{"default_log_view": "business"}); err != nil {
		t.Fatalf("Update(default_log_view) error = %v", err)
	}
	if err := app.logs.Add("新增账号", map[string]any{"module": "accounts", "operation_type": "新增"}); err != nil {
		t.Fatalf("Add(business log) error = %v", err)
	}
	if err := app.logs.Add("GET /api/profile", map[string]any{"method": "GET", "path": "/api/profile", "module": "profile", "status": 200, "log_level": "info"}); err != nil {
		t.Fatalf("Add(noisy audit log) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logs status = %d body = %s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("logs json: %v", err)
	}
	if summaries := logPayloadSummaries(logItems(payload)); !reflect.DeepEqual(summaries, []string{"新增账号"}) {
		t.Fatalf("default logs summaries = %#v", summaries)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/logs?view=all", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logs all status = %d body = %s", res.Code, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("logs all json: %v", err)
	}
	if summaries := logPayloadSummaries(logItems(payload)); !reflect.DeepEqual(summaries, []string{"GET /api/profile", "新增账号"}) {
		t.Fatalf("all logs summaries = %#v", summaries)
	}
}

func TestChatCompletionsCallLogIncludesUpstreamAccountPreview(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	const fullToken = "secret-upstream-token-for-log-test"
	ctx, tracker := protocol.WithAccountUsageTracker(context.Background())
	tracker.Record(fullToken)
	app.logCall(ctx, service.Identity{ID: "user-1", Role: service.AuthRoleUser, Name: "frontend"}, "文本生成", http.MethodPost, "/v1/chat/completions", "gpt-5", time.Now(), "success", http.StatusOK, "", nil, auditRequestCapture{})

	logs := app.logs.Search(service.LogQuery{Limit: 10})
	item := findLogBySummary(logs, "文本生成调用完成")
	if item == nil {
		t.Fatalf("expected chat completions log, got %#v", logs)
	}
	detail := util.StringMap(item["detail"])
	if util.Clean(detail["upstream_account_id"]) == "" || util.Clean(detail["upstream_token_preview"]) == "" {
		t.Fatalf("log detail missing upstream singleton fields: %#v", detail)
	}
	accounts := util.AsMapSlice(detail["upstream_accounts"])
	if len(accounts) != 1 || accounts[0]["account_id"] != detail["upstream_account_id"] || accounts[0]["token_preview"] != detail["upstream_token_preview"] {
		t.Fatalf("log detail upstream accounts = %#v, detail = %#v", accounts, detail)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal log item: %v", err)
	}
	if strings.Contains(string(encoded), fullToken) {
		t.Fatalf("log JSON leaked full upstream token: %s", encoded)
	}
}

func TestRunLoggedChatTaskCreatesAccountUsageTrackerForLogs(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	const fullToken = "task-chat-upstream-token-for-log-test"
	app.engine.Accounts.AddAccounts([]string{fullToken})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := app.runLoggedChatTask(ctx, service.Identity{ID: "user-1", Role: service.AuthRoleUser, Name: "frontend"}, map[string]any{
		"model":    "gpt-5",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if err == nil {
		t.Fatal("runLoggedChatTask() error = nil, want canceled upstream error")
	}
	logs := app.logs.Search(service.LogQuery{Limit: 20})
	item := findLogBySummary(logs, "文本生成调用失败")
	if item == nil {
		t.Fatalf("expected failed chat task log, got %#v", logs)
	}
	detail := util.StringMap(item["detail"])
	if detail["endpoint"] != "/api/creation-tasks/chat-completions" || util.Clean(detail["upstream_account_id"]) == "" || util.Clean(detail["upstream_token_preview"]) == "" {
		t.Fatalf("chat task log detail missing upstream account fields: %#v", detail)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal log item: %v", err)
	}
	if strings.Contains(string(encoded), fullToken) {
		t.Fatalf("chat task log JSON leaked full upstream token: %s", encoded)
	}
}

func TestCreationTaskResponseImageRouteIsNotAnAdminTaskResource(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	_, rawKey, err := app.auth.CreateAPIKey(service.AuthRoleAdmin, "frontend", service.AuthOwner{})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	body := `{"client_task_id":"response-image-route","prompt":"生成封面","model":"gpt-5.5","size":"2048x2048","image_resolution":"2k","quality":"high","output_format":"jpeg","output_compression":42,"n":2,"images":["data:image/png;base64,cG5n"],"messages":[{"role":"user","content":"生成封面"}],"visibility":"public"}`
	req := httptest.NewRequest(http.MethodPost, "/api/creation-tasks/response-image-generations", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("response image creation task status = %d body = %s, want 404", res.Code, res.Body.String())
	}
}

func TestRunLoggedImageTaskLogsTextOutputAsFailure(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	identity := service.Identity{ID: "admin", Role: service.AuthRoleAdmin, Name: "Admin"}
	result, err := app.runLoggedImageTask(
		context.Background(),
		identity,
		map[string]any{"model": "gpt-image-2"},
		"/api/creation-tasks/image-generations",
		"文生图",
		func(context.Context, map[string]any) (map[string]any, error) {
			return map[string]any{"output_type": "text", "message": "模型返回文本", "data": []map[string]any{}}, nil
		},
	)
	if err != nil {
		t.Fatalf("runLoggedImageTask() error = %v", err)
	}
	if result["output_type"] != "text" || result["message"] != "模型返回文本" {
		t.Fatalf("runLoggedImageTask() result = %#v", result)
	}
	logs := app.logs.Search(service.LogQuery{Limit: 10})
	item := findLogBySummary(logs, "文生图调用失败")
	if item == nil {
		t.Fatalf("expected text-only image result to write failure log, got %#v", logs)
	}
	detail := util.StringMap(item["detail"])
	if detail["outcome"] != "failed" || util.ToInt(detail["status"], 0) != http.StatusBadGateway {
		t.Fatalf("failure log detail = %#v", detail)
	}
}

func TestRecordGeneratedImagesForPayloadStoresReusableRequestMetadata(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	rel := "2026/05/12/reusable.png"
	imagePath := filepath.Join(app.config.ImagesDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := writeHTTPTestPNG(imagePath); err != nil {
		t.Fatalf("writeHTTPTestPNG() error = %v", err)
	}

	app.recordGeneratedImagesForPayload(
		service.Identity{ID: "admin", Role: service.AuthRoleAdmin, Name: "Admin"},
		[]string{rel},
		service.ImageVisibilityPublic,
		map[string]any{
			"prompt":             "复用这个提示词",
			"model":              "gpt-image-2",
			"quality":            "high",
			"image_resolution":   "2k",
			"size":               "2048x2048",
			"output_format":      "jpeg",
			"output_compression": 42,
			"background":         "transparent",
			"moderation":         "low",
			"style":              "vivid",
			"partial_images":     2,
			"input_image_mask":   "mask-id",
			"images": []protocol.UploadedImage{
				{Filename: "source.png", ContentType: "image/png", Data: []byte("reference-bytes")},
			},
			"share_prompt_parameters": true,
			"share_reference_images":  true,
		},
	)

	list := app.images.ListImages("http://127.0.0.1:8000", "", "", service.ImageAccessScope{Public: true})
	items := list["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("ListImages() = %#v", list)
	}
	item := items[0]
	if item["prompt"] != "复用这个提示词" ||
		item["model"] != "gpt-image-2" ||
		item["quality"] != "high" ||
		item["resolution_preset"] != "2k" ||
		item["requested_size"] != "2048x2048" ||
		item["output_format"] != "jpeg" ||
		item["output_compression"] != 42 ||
		item["background"] != "transparent" ||
		item["moderation"] != "low" ||
		item["style"] != "vivid" ||
		item["partial_images"] != 2 ||
		item["input_image_mask"] != "mask-id" {
		t.Fatalf("reusable metadata = %#v", item)
	}
	referenceURLs, ok := item["reference_image_urls"].([]string)
	if !ok || len(referenceURLs) != 1 || !strings.Contains(referenceURLs[0], "/image-references/") {
		t.Fatalf("reference_image_urls = %#v", item["reference_image_urls"])
	}
	parsedReferenceURL, err := url.Parse(referenceURLs[0])
	if err != nil {
		t.Fatalf("parse reference url: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, parsedReferenceURL.RequestURI(), nil)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "reference-bytes" {
		t.Fatalf("public reference status/body = %d %q", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("reference Content-Type = %q, want image/png", got)
	}
}

func TestDirectImageGenerationDoesNotLimitAdminToken(t *testing.T) {
	t.Setenv("CHATGPT2API_USER_DEFAULT_CONCURRENT_LIMIT", "2")
	app := newTestApp(t)
	defer app.Close()

	app.engine.ImageTokenProvider = func(context.Context) (string, error) {
		return "test-token", nil
	}
	app.engine.ImageClientFactory = func(string) *backend.Client {
		return nil
	}

	var mu sync.Mutex
	active := 0
	maxActive := 0
	release := make(chan struct{})
	app.engine.StreamImageOutputsFunc = func(ctx context.Context, client *backend.Client, request protocol.ConversationRequest, index, total int) (<-chan protocol.ImageOutput, <-chan error) {
		out := make(chan protocol.ImageOutput)
		errCh := make(chan error, 1)
		go func() {
			defer close(out)
			defer close(errCh)
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			select {
			case <-release:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
			out <- protocol.ImageOutput{
				Kind:    "result",
				Model:   request.Model,
				Index:   index,
				Total:   total,
				Created: int64(index),
				Data:    []map[string]any{{"url": fmt.Sprintf("https://example.test/%d.png", index)}},
			}
			mu.Lock()
			active--
			mu.Unlock()
			errCh <- nil
		}()
		return out, errCh
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"prompt":"draw","model":"gpt-image-2","n":3,"response_format":"url"}`))
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.Handler().ServeHTTP(res, req)
	}()

	waitForHTTPTestCondition(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return maxActive >= 3
	})
	mu.Lock()
	gotMaxActive := maxActive
	mu.Unlock()
	if gotMaxActive != 3 {
		t.Fatalf("max concurrent admin image outputs = %d, want 3", gotMaxActive)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("admin image generation request did not finish")
	}
	if res.Code != http.StatusOK {
		t.Fatalf("admin image generation status = %d body = %s", res.Code, res.Body.String())
	}
}

func TestProtocolBillableUnitsBoundaryAndEquivalenceClasses(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		body     map[string]any
		want     int
	}{
		{
			name:     "image generation defaults to one",
			endpoint: "/v1/images/generations",
			body:     map[string]any{},
			want:     1,
		},
		{
			name:     "image generation zero clamps to one",
			endpoint: "/v1/images/generations",
			body:     map[string]any{"n": 0},
			want:     1,
		},
		{
			name:     "image generation negative clamps to one",
			endpoint: "/v1/images/generations",
			body:     map[string]any{"n": -3},
			want:     1,
		},
		{
			name:     "image generation upper bound",
			endpoint: "/v1/images/generations",
			body:     map[string]any{"n": 4},
			want:     4,
		},
		{
			name:     "image generation above upper bound clamps",
			endpoint: "/v1/images/generations",
			body:     map[string]any{"n": 5},
			want:     4,
		},
		{
			name:     "text chat is free even with n",
			endpoint: "/v1/chat/completions",
			body: map[string]any{
				"model":    "gpt-5",
				"n":        4,
				"messages": []any{map[string]any{"role": "user", "content": "hello"}},
			},
			want: 0,
		},
		{
			name:     "image chat defaults to one",
			endpoint: "/v1/chat/completions",
			body: map[string]any{
				"model":      "gpt-5",
				"modalities": []any{"image"},
				"messages":   []any{map[string]any{"role": "user", "content": "draw"}},
			},
			want: 1,
		},
		{
			name:     "image chat above upper bound clamps",
			endpoint: "/v1/chat/completions",
			body: map[string]any{
				"model":      "gpt-5",
				"modalities": []any{"image"},
				"n":          7,
				"messages":   []any{map[string]any{"role": "user", "content": "draw"}},
			},
			want: 4,
		},
		{
			name:     "text responses are free",
			endpoint: "/v1/responses",
			body: map[string]any{
				"model": "gpt-5",
				"input": "hello",
			},
			want: 0,
		},
		{
			name:     "responses image tool defaults to one",
			endpoint: "/v1/responses",
			body: map[string]any{
				"model": "gpt-image-2",
				"input": "draw",
				"tools": []any{map[string]any{"type": "image_generation"}},
			},
			want: 1,
		},
		{
			name:     "responses image tool choice uses n upper bound",
			endpoint: "/v1/responses",
			body: map[string]any{
				"model":       "gpt-image-2",
				"input":       "draw",
				"n":           4,
				"tool_choice": map[string]any{"type": "image_generation"},
			},
			want: 4,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := protocolBillableUnits(tc.endpoint, tc.body); got != tc.want {
				t.Fatalf("protocolBillableUnits(%q, %#v) = %d, want %d", tc.endpoint, tc.body, got, tc.want)
			}
		})
	}
}

func TestEmptyCollectionEndpointsReturnArrays(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	for _, tc := range []struct {
		name string
		path string
		keys []string
	}{
		{name: "accounts", path: "/api/accounts", keys: []string{"items"}},
		{name: "images", path: "/api/images", keys: []string{"items", "groups"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", adminAuthHeader(t, app))
			res := httptest.NewRecorder()
			app.Handler().ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("%s status = %d body = %s", tc.path, res.Code, res.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
				t.Fatalf("%s json: %v", tc.path, err)
			}
			for _, key := range tc.keys {
				items, ok := payload[key].([]any)
				if !ok || items == nil || len(items) != 0 {
					t.Fatalf("%s %q = %#v, want empty array", tc.path, key, payload[key])
				}
			}
		})
	}
}

func TestAccountToggleEnabledEndpoint(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	app.accounts.AddAccounts([]string{"token-1", "token-2"})
	app.accounts.UpdateAccount("token-1", map[string]any{"status": "正常"})
	app.accounts.UpdateAccount("token-2", map[string]any{"status": "限流"})

	accounts := app.accounts.ListAccounts()
	account1 := findHTTPItem(accounts, util.SHA1Short("token-1", 16))
	account2 := findHTTPItem(accounts, util.SHA1Short("token-2", 16))
	if account1 == nil || account2 == nil {
		t.Fatalf("created accounts missing: %#v", accounts)
	}
	account1ID := util.Clean(account1["id"])
	account2ID := util.Clean(account2["id"])

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/toggle-enabled", strings.NewReader(`{"account_ids":["`+account1ID+`","`+account2ID+`"],"enabled":false}`))
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("disable accounts status = %d body = %s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("disable accounts json: %v", err)
	}
	if util.ToInt(payload["updated"], -1) != 2 || util.ToInt(payload["skipped"], -1) != 0 {
		t.Fatalf("disable result = %#v", payload)
	}
	items := logItems(payload)
	updated2 := findHTTPItem(items, account2ID)
	if updated2 == nil || updated2["status"] != "限流" || updated2["enabled"] != false {
		t.Fatalf("disabled token-2 item = %#v in %#v", updated2, payload)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/accounts/toggle-enabled", strings.NewReader(`{"account_id":"`+account2ID+`","enabled":true}`))
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("enable account status = %d body = %s", res.Code, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("enable account json: %v", err)
	}
	if util.ToInt(payload["updated"], -1) != 1 || util.ToInt(payload["skipped"], -1) != 0 {
		t.Fatalf("enable result = %#v", payload)
	}
	items = logItems(payload)
	updated2 = findHTTPItem(items, account2ID)
	if updated2 == nil || updated2["status"] != "限流" || updated2["enabled"] != true {
		t.Fatalf("enabled token-2 item = %#v in %#v", updated2, payload)
	}
}

func TestRedactAccountPayloadCoversRefreshResults(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	identity := service.Identity{
		Role: service.AuthRoleUser,
		APIPermissions: []string{
			service.APIPermissionKey(http.MethodGet, "/api/accounts"),
			service.APIPermissionKey(http.MethodPost, "/api/accounts/refresh"),
		},
	}
	payload := map[string]any{
		"items": []map[string]any{{
			"id":           "account-1",
			"access_token": "token-1",
		}},
		"errors": []map[string]string{{
			"access_token": "token-2",
			"error":        "failed",
		}},
		"results": []map[string]any{{
			"access_token": "token-3",
			"success":      false,
			"message":      "failed",
		}},
	}

	app.redactAccountPayloadForIdentity(identity, payload)

	items := payload["items"].([]map[string]any)
	if _, ok := items[0]["access_token"]; ok {
		t.Fatalf("items should not expose access_token: %#v", items[0])
	}
	errors := payload["errors"].([]map[string]string)
	if _, ok := errors[0]["access_token"]; ok {
		t.Fatalf("errors should not expose access_token: %#v", errors[0])
	}
	if errors[0]["account_id"] != util.SHA1Short("token-2", 16) {
		t.Fatalf("error account_id = %#v, want hash", errors[0]["account_id"])
	}
	results := payload["results"].([]map[string]any)
	if _, ok := results[0]["access_token"]; ok {
		t.Fatalf("results should not expose access_token: %#v", results[0])
	}
	if results[0]["account_id"] != util.SHA1Short("token-3", 16) {
		t.Fatalf("result account_id = %#v, want hash", results[0]["account_id"])
	}
}

func TestImageThumbnailsAreGeneratedOnDemand(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	rel := "2026/04/29/sample.png"
	imagePath := filepath.Join(app.config.ImagesDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatalf("mkdir image dir: %v", err)
	}
	if err := writeHTTPTestPNG(imagePath); err != nil {
		t.Fatalf("write image: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/api/images status = %d body = %s", res.Code, res.Body.String())
	}
	var list map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatalf("/api/images json: %v", err)
	}
	items := logItems(list)
	if len(items) != 1 {
		t.Fatalf("/api/images items = %#v", list)
	}
	thumbnailURL, _ := items[0]["thumbnail_url"].(string)
	if !strings.Contains(thumbnailURL, "/image-thumbnails/") {
		t.Fatalf("thumbnail_url = %q, want lazy thumbnail route", thumbnailURL)
	}
	parsedThumbnailURL, err := url.Parse(thumbnailURL)
	if err != nil {
		t.Fatalf("parse thumbnail URL: %v", err)
	}
	if !strings.HasSuffix(parsedThumbnailURL.Path, ".jpg") {
		t.Fatalf("thumbnail path = %q, want .jpg suffix", parsedThumbnailURL.Path)
	}
	if parsedThumbnailURL.Query().Get("v") == "" {
		t.Fatalf("thumbnail URL = %q, want cache-busting query", thumbnailURL)
	}
	thumbPath := filepath.Join(app.config.ImageThumbnailsDir(), filepath.FromSlash(rel)+".jpg")
	if _, err := os.Stat(thumbPath); !os.IsNotExist(err) {
		t.Fatalf("/api/images should not create thumbnail synchronously, stat error = %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, parsedThumbnailURL.Path, nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("thumbnail status = %d body = %s", res.Code, res.Body.String())
	}
	if res.Body.Len() == 0 {
		t.Fatal("thumbnail body is empty")
	}
	if got := res.Header().Get("Cache-Control"); got != imageThumbnailCacheControl {
		t.Fatalf("thumbnail Cache-Control = %q, want %q", got, imageThumbnailCacheControl)
	}
	if got := res.Header().Get("Content-Type"); !strings.Contains(got, "image/jpeg") {
		t.Fatalf("thumbnail Content-Type = %q, want image/jpeg", got)
	}
	if _, err := os.Stat(thumbPath); err != nil {
		t.Fatalf("thumbnail was not created on demand: %v", err)
	}
}

func TestAuthSessionCookieLifecycle(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"`+testAdminUsername+`","password":"`+testAdminPassword+`"}`))
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", res.Code, res.Body.String())
	}
	cookie := findResponseCookie(res.Result(), authSessionCookieName)
	if cookie == nil || cookie.Value == "" || cookie.Path != "/" || !cookie.HttpOnly {
		t.Fatalf("login cookie = %#v", cookie)
	}
	if got := cookie.SameSite; got != http.SameSiteLaxMode {
		t.Fatalf("login cookie SameSite = %v, want Lax", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logout status = %d body = %s", res.Code, res.Body.String())
	}
	cleared := findResponseCookie(res.Result(), authSessionCookieName)
	if cleared == nil || cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("logout cookie = %#v", cleared)
	}
}

func TestLoginAllowsCredentialedLoopbackFrontend(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"`+testAdminUsername+`","password":"`+testAdminPassword+`"}`))
	req.Host = "127.0.0.1:8000"
	req.Header.Set("Origin", "http://localhost:5173")
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want frontend origin", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want true", got)
	}
	if cookie := findResponseCookie(res.Result(), authSessionCookieName); cookie == nil || cookie.Value == "" {
		t.Fatalf("login cookie = %#v", cookie)
	}
}

func TestCredentialedLoginPreflightAllowsContentType(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	req := httptest.NewRequest(http.MethodOptions, "/auth/login", nil)
	req.Host = "127.0.0.1:8000"
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want request origin", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want true", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Headers"); got != "content-type" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want content-type", got)
	}
}

func TestCredentialedImageVisibilityPreflightAllowsPatchAuthorization(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	req := httptest.NewRequest(http.MethodOptions, "/api/images/visibility", nil)
	req.Host = "127.0.0.1:8000"
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want request origin", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want true", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Methods"); got != http.MethodPatch {
		t.Fatalf("Access-Control-Allow-Methods = %q, want PATCH", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Headers"); got != "authorization,content-type" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want authorization,content-type", got)
	}
}

func TestImageThumbnailRejectsTraversal(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	outsideThumbnailRoot := filepath.Join(app.config.DataDir, "secret.png.jpg")
	if err := os.WriteFile(outsideThumbnailRoot, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside thumbnail root: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/image-thumbnails/../secret.png.jpg", nil)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("thumbnail traversal status = %d body = %q, want 404", res.Code, res.Body.String())
	}
}

func TestCreationTaskPollingDisablesCaching(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	_, rawKey, err := app.auth.CreateAPIKey(service.AuthRoleAdmin, "frontend", service.AuthOwner{})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/creation-tasks?ids=missing", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("creation task list status = %d body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := res.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
}

func TestModelsCallLogIncludesUserKeyName(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	_, rawKey, err := app.auth.CreateAPIKey(service.AuthRoleAdmin, "frontend", service.AuthOwner{})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("models status = %d body = %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logs status = %d body = %s", res.Code, res.Body.String())
	}
	var logs map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &logs); err != nil {
		t.Fatalf("logs json: %v", err)
	}
	items := logItems(logs)
	if len(items) == 0 {
		t.Fatalf("expected models call to write a log event, got %#v", logs)
	}
	item := findLogByDetails(items, map[string]any{
		"endpoint": "/v1/models",
		"outcome":  "success",
	})
	if item == nil {
		t.Fatalf("expected models call log event, got %#v", items)
	}
	if _, ok := item["type"]; ok {
		t.Fatalf("log item should not expose type: %#v", item)
	}
	detail, _ := item["detail"].(map[string]any)
	if detail["endpoint"] != "/v1/models" ||
		detail["path"] != "/v1/models" ||
		detail["method"] != http.MethodGet ||
		detail["status"] != float64(http.StatusOK) ||
		detail["outcome"] != "success" ||
		detail["key_name"] != "frontend" ||
		detail["auth_kind"] != service.AuthKindAPIKey ||
		detail["key_role"] != "admin" {
		t.Fatalf("models call log did not include user key identity: %#v", detail)
	}
	if _, ok := detail["session_name"]; ok {
		t.Fatalf("api key log should not include session_name: %#v", detail)
	}
}

func TestProtocolCallLogCapturesUnknownLengthRequestWithoutDuplicateAudit(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	_, rawKey, err := app.auth.CreateAPIKey(service.AuthRoleAdmin, "frontend", service.AuthOwner{})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	app.engine.ImageTokenProvider = func(context.Context) (string, error) {
		return "test-token", nil
	}
	app.engine.ImageClientFactory = func(string) *backend.Client {
		return nil
	}
	app.engine.StreamImageOutputsFunc = func(ctx context.Context, client *backend.Client, request protocol.ConversationRequest, index, total int) (<-chan protocol.ImageOutput, <-chan error) {
		out := make(chan protocol.ImageOutput, 1)
		errCh := make(chan error, 1)
		out <- protocol.ImageOutput{
			Kind:    "result",
			Model:   request.Model,
			Index:   index,
			Total:   total,
			Created: 123,
			Data:    []map[string]any{{"url": "https://example.test/image.png"}},
		}
		close(out)
		errCh <- nil
		close(errCh)
		return out, errCh
	}

	body := `{"prompt":"draw a cat","model":"gpt-image-2","n":1,"response_format":"url"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations?trace=1", io.NopCloser(strings.NewReader(body)))
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+rawKey)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("image generation status = %d body = %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logs status = %d body = %s", res.Code, res.Body.String())
	}
	var logs map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &logs); err != nil {
		t.Fatalf("logs json: %v", err)
	}
	items := logItems(logs)
	callLog := findLogByDetails(items, map[string]any{"endpoint": "/v1/images/generations", "outcome": "success"})
	if callLog == nil {
		t.Fatalf("expected image call log, got %#v", items)
	}
	detail, _ := callLog["detail"].(map[string]any)
	requestArgs, _ := detail["request_args"].(map[string]any)
	query, _ := requestArgs["query"].(map[string]any)
	requestBody, _ := requestArgs["body"].(map[string]any)
	if query["trace"] != "1" || requestBody["model"] != "gpt-image-2" || requestBody["prompt"] != "draw a cat" {
		t.Fatalf("request args not captured completely: %#v", requestArgs)
	}
	if detail["request_truncated"] != nil {
		t.Fatalf("small request should not be marked truncated: %#v", detail)
	}
	if auditLog := findHTTPAuditLogByPath(items, "/v1/images/generations"); auditLog != nil {
		t.Fatalf("protocol request should not also create generic audit log: %#v", auditLog)
	}
}

func TestAPIAuditLogCapturesRequestMetadata(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/settings?section=logging", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	req.Header.Set("User-Agent", "chatgpt2api-test")
	req.RemoteAddr = "203.0.113.10:12345"
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("settings status = %d body = %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/logs?username=admin&method=GET&status=200&summary=%2Fapi%2Fsettings&view=all", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("audit logs status = %d body = %s", res.Code, res.Body.String())
	}
	var logs map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &logs); err != nil {
		t.Fatalf("audit logs json: %v", err)
	}
	items := logItems(logs)
	if len(items) == 0 {
		t.Fatalf("expected audit log, got %#v", logs)
	}
	item := findLogByDetail(items, "path", "/api/settings")
	if item == nil {
		t.Fatalf("expected audit log for /api/settings, got %#v", items)
	}
	if _, ok := item["type"]; ok {
		t.Fatalf("log item should not expose type: %#v", item)
	}
	detail, _ := item["detail"].(map[string]any)
	if detail["method"] != http.MethodGet || detail["status"] != float64(http.StatusOK) || detail["log_level"] != "info" {
		t.Fatalf("unexpected audit detail = %#v", detail)
	}
	if detail["operation_type"] != "查询" || detail["subject_id"] != testAdminUsername || detail["user_agent"] != "chatgpt2api-test" {
		t.Fatalf("missing audit identity/request fields = %#v", detail)
	}
	if detail["username"] != "管理员" || detail["session_name"] != "登录会话" || detail["auth_kind"] != service.AuthKindSession {
		t.Fatalf("session audit detail should use username/session fields instead of token name: %#v", detail)
	}
	if _, ok := detail["key_name"]; ok {
		t.Fatalf("session audit detail should not expose 登录会话 as key_name: %#v", detail)
	}
	if _, ok := detail["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms not numeric in audit detail = %#v", detail)
	}
}

func TestCreationTaskSubmitLogsRequestAndPollingAvoidsGenericAuditNoise(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	_, rawKey, err := app.auth.CreateAPIKey(service.AuthRoleAdmin, "frontend", service.AuthOwner{})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/creation-tasks/image-generations", strings.NewReader(`{"client_task_id":"noise-test","prompt":"test image"}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("submit creation task status = %d body = %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/creation-tasks?ids=noise-test", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("poll creation task status = %d body = %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/logs?view=all", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logs status = %d body = %s", res.Code, res.Body.String())
	}
	var logs map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &logs); err != nil {
		t.Fatalf("logs json: %v", err)
	}
	items := logItems(logs)
	submitLog := findHTTPAuditLogByPath(items, "/api/creation-tasks/image-generations")
	if submitLog == nil {
		t.Fatalf("creation task submit should create a request log, got %#v", items)
	}
	detail, _ := submitLog["detail"].(map[string]any)
	requestArgs, _ := detail["request_args"].(map[string]any)
	if requestArgs["client_task_id"] != "noise-test" || requestArgs["prompt"] != "test image" {
		t.Fatalf("creation task submit request args = %#v", requestArgs)
	}
	if auditLog := findHTTPAuditLogByPath(items, "/api/creation-tasks"); auditLog != nil {
		t.Fatalf("creation task polling should not create generic audit log: %#v", auditLog)
	}
}

func TestLogGovernanceEndpointCleansOldLogs(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	backend, err := app.config.StorageBackend()
	if err != nil {
		t.Fatalf("StorageBackend() error = %v", err)
	}
	logStore, ok := backend.(storage.LogBackend)
	if !ok {
		t.Fatalf("storage backend %T does not implement LogBackend", backend)
	}
	for _, item := range []map[string]any{
		{"time": time.Now().AddDate(0, 0, -2).Format("2006-01-02 15:04:05"), "type": "event", "summary": "旧日志", "detail": map[string]any{"status": "success"}},
		{"time": time.Now().Format("2006-01-02 15:04:05"), "type": "event", "summary": "新日志", "detail": map[string]any{"status": 200}},
	} {
		if err := logStore.AppendLog(item); err != nil {
			t.Fatalf("AppendLog() error = %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/logs/governance", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("governance status = %d body = %s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("governance json: %v", err)
	}
	governance, _ := payload["governance"].(map[string]any)
	if governance["total"] != float64(2) {
		t.Fatalf("governance total = %#v, want 2 in %#v", governance["total"], payload)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/logs/governance", strings.NewReader(`{"retention_days":1}`))
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("cleanup status = %d body = %s", res.Code, res.Body.String())
	}
	payload = map[string]any{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("cleanup json: %v", err)
	}
	cleanup, _ := payload["cleanup"].(map[string]any)
	if cleanup["deleted"] != float64(1) || cleanup["remaining"] != float64(1) {
		t.Fatalf("cleanup result = %#v, want deleted 1 remaining 1", cleanup)
	}
}

func TestImageStorageGovernanceEndpointCleansThumbnails(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	rel := "2026/04/29/sample.png"
	imagePath := filepath.Join(app.config.ImagesDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatalf("mkdir image dir: %v", err)
	}
	if err := writeHTTPTestPNG(imagePath); err != nil {
		t.Fatalf("write image: %v", err)
	}
	app.images.RecordGeneratedImages([]string{rel}, "admin", "Admin", service.ImageVisibilityPrivate)
	app.images.EnsureThumbnails([]string{rel})
	thumbPath := filepath.Join(app.config.ImageThumbnailsDir(), filepath.FromSlash(rel)+".jpg")
	if _, err := os.Stat(thumbPath); err != nil {
		t.Fatalf("thumbnail was not created: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/images/storage-governance", nil)
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("storage governance status = %d body = %s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("storage governance json: %v", err)
	}
	governance, _ := payload["governance"].(map[string]any)
	if governance["images_count"] != float64(1) || governance["thumbnail_files"] != float64(1) {
		t.Fatalf("storage governance = %#v", governance)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/images/storage-governance", strings.NewReader(`{"action":"thumbnails"}`))
	req.Header.Set("Authorization", adminAuthHeader(t, app))
	res = httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("thumbnail cleanup status = %d body = %s", res.Code, res.Body.String())
	}
	payload = map[string]any{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("thumbnail cleanup json: %v", err)
	}
	cleanup, _ := payload["cleanup"].(map[string]any)
	if cleanup["deleted_thumbnails"] != float64(1) || cleanup["deleted_images"] != float64(0) {
		t.Fatalf("thumbnail cleanup = %#v", cleanup)
	}
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("image should remain after thumbnail cleanup: %v", err)
	}
	if _, err := os.Stat(thumbPath); !os.IsNotExist(err) {
		t.Fatalf("thumbnail still exists, stat error = %v", err)
	}
}

func logPayloadSummaries(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, util.Clean(item["summary"]))
	}
	return out
}

func logItems(payload map[string]any) []map[string]any {
	rawItems, _ := payload["items"].([]any)
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item, ok := raw.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

func findLogBySummary(items []map[string]any, summary string) map[string]any {
	for _, item := range items {
		if item["summary"] == summary {
			return item
		}
	}
	return nil
}

func findHTTPItem(items []map[string]any, id string) map[string]any {
	for _, item := range items {
		if item["id"] == id {
			return item
		}
	}
	return nil
}

func findHTTPBulkBillingResult(items []map[string]any, userID string) map[string]any {
	for _, item := range items {
		if item["user_id"] == userID {
			return item
		}
	}
	return nil
}

func findResponseCookie(res *http.Response, name string) *http.Cookie {
	for _, cookie := range res.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func assertCreationConcurrentLimit(t *testing.T, payload map[string]any, want int) {
	t.Helper()
	got, ok := payload["creation_concurrent_limit"].(float64)
	if !ok || got != float64(want) {
		t.Fatalf("creation_concurrent_limit = %#v, want %d in %#v", payload["creation_concurrent_limit"], want, payload)
	}
}

func findLogByDetail(items []map[string]any, key, value string) map[string]any {
	return findLogByDetails(items, map[string]any{key: value})
}

func findHTTPAuditLogByPath(items []map[string]any, path string) map[string]any {
	for _, item := range items {
		detail, _ := item["detail"].(map[string]any)
		if detail["path"] == path && detail["endpoint"] == nil {
			return item
		}
	}
	return nil
}

func findLogByDetails(items []map[string]any, values map[string]any) map[string]any {
	for _, item := range items {
		detail, _ := item["detail"].(map[string]any)
		matches := true
		for key, value := range values {
			if detail[key] != value {
				matches = false
				break
			}
		}
		if matches {
			return item
		}
	}
	return nil
}

const (
	testAdminUsername = "admin"
	testAdminPassword = "AdminPass123!"
)

func adminAuthHeader(t *testing.T, app *App) string {
	t.Helper()
	identity, token, err := app.auth.LoginPassword(testAdminUsername, testAdminPassword)
	if err != nil {
		t.Fatalf("admin LoginPassword() error = %v", err)
	}
	if identity == nil || identity.Role != service.AuthRoleAdmin || token == "" {
		t.Fatalf("admin LoginPassword() identity=%#v token=%q", identity, token)
	}
	return "Bearer " + token
}

func waitForHTTPTestCondition(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func newTestApp(t *testing.T) *App {
	return newTestAppWithBillingDefaults(t, "standard", "1000", "1000", "monthly")
}

func newTestAppWithBillingDefaults(t *testing.T, billingType, standardBalance, subscriptionQuota, subscriptionPeriod string) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CHATGPT2API_ROOT", root)
	t.Setenv("CHATGPT2API_ADMIN_USERNAME", testAdminUsername)
	t.Setenv("CHATGPT2API_ADMIN_PASSWORD", testAdminPassword)
	t.Setenv("CHATGPT2API_DEFAULT_BILLING_TYPE", billingType)
	t.Setenv("CHATGPT2API_DEFAULT_STANDARD_BALANCE", standardBalance)
	t.Setenv("CHATGPT2API_DEFAULT_SUBSCRIPTION_QUOTA", subscriptionQuota)
	t.Setenv("CHATGPT2API_DEFAULT_SUBSCRIPTION_PERIOD", subscriptionPeriod)
	unsetTestEnv(t, "CHATGPT2API_REGISTRATION_ENABLED")
	unsetTestEnv(t, "CHATGPT2API_DEFAULT_LOG_VIEW")
	t.Setenv("STORAGE_BACKEND", "sqlite")
	t.Setenv("DATABASE_URL", "")
	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	app.engine.ListModelsFunc = func(context.Context) (map[string]any, error) {
		return map[string]any{"object": "list", "data": []map[string]any{}}, nil
	}
	return app
}

func installHTTPTestImageStream(t *testing.T, app *App) {
	t.Helper()
	installHTTPTestImageStreamFunc(t, app, func(ctx context.Context, client *backend.Client, request protocol.ConversationRequest, index, total int) (<-chan protocol.ImageOutput, <-chan error) {
		return httpTestImageOutputStream(request, index)
	})
}

func installHTTPTestImageStreamFunc(t *testing.T, app *App, fn func(context.Context, *backend.Client, protocol.ConversationRequest, int, int) (<-chan protocol.ImageOutput, <-chan error)) {
	t.Helper()
	app.engine.ImageTokenProvider = func(context.Context) (string, error) {
		return "test-token", nil
	}
	app.engine.ImageClientFactory = func(string) *backend.Client {
		return nil
	}
	app.engine.StreamImageOutputsFunc = fn
}

func httpTestImageOutputStream(request protocol.ConversationRequest, index int) (<-chan protocol.ImageOutput, <-chan error) {
	out := make(chan protocol.ImageOutput, 1)
	errCh := make(chan error, 1)
	out <- protocol.ImageOutput{
		Kind:    "result",
		Model:   request.Model,
		Index:   index,
		Total:   request.N,
		Created: int64(index),
		Data: []map[string]any{{
			"url":      fmt.Sprintf("https://example.test/%d.png", index),
			"b64_json": fmt.Sprintf("image-%d", index),
		}},
	}
	close(out)
	errCh <- nil
	close(errCh)
	return out, errCh
}

func httpTestMessageOnlyImageOutputStream(request protocol.ConversationRequest, index int) (<-chan protocol.ImageOutput, <-chan error) {
	out := make(chan protocol.ImageOutput, 1)
	errCh := make(chan error, 1)
	out <- protocol.ImageOutput{
		Kind:    "message",
		Model:   request.Model,
		Index:   index,
		Total:   request.N,
		Created: int64(index),
		Text:    "text only",
	}
	close(out)
	errCh <- nil
	close(errCh)
	return out, errCh
}

func unsetTestEnv(t *testing.T, key string) {
	t.Helper()
	original, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%s): %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, original)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func writeHTTPTestPNG(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return encodeHTTPTestPNG(file)
}

func encodeHTTPTestPNG(file interface {
	Write([]byte) (int, error)
}) error {
	img := image.NewRGBA(image.Rect(0, 0, 12, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 12; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 180, A: 255})
		}
	}
	return png.Encode(file, img)
}

func TestProxySettingsApplySentinelSolverFieldsIndependently(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	login := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"`+testAdminUsername+`","password":"`+testAdminPassword+`"}`))
	loginRes := httptest.NewRecorder()
	app.Handler().ServeHTTP(loginRes, login)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", loginRes.Code, loginRes.Body.String())
	}
	cookie := findResponseCookie(loginRes.Result(), authSessionCookieName)
	if cookie == nil {
		t.Fatalf("login cookie missing: %s", loginRes.Body.String())
	}

	// 只提交 sentinel 字段、不提交 url/flaresolverr：三个分支必须各自独立生效。
	req := httptest.NewRequest(http.MethodPost, "/api/proxy", strings.NewReader(`{"sentinel_solver_url":"http://sentinel-solver.zeabur.internal:8080/","sentinel_solver_key":"solver-key"}`))
	req.AddCookie(cookie)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("update proxy status = %d body = %s", res.Code, res.Body.String())
	}
	if got := app.config.SentinelSolverURL(); got != "http://sentinel-solver.zeabur.internal:8080" {
		t.Fatalf("SentinelSolverURL() = %q", got)
	}
	if got := app.config.SentinelSolverKey(); got != "solver-key" {
		t.Fatalf("SentinelSolverKey() = %q", got)
	}
}
