package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// tokenEndpointCapture 记录测试服务器收到的请求，供断言使用
type tokenEndpointCapture struct {
	contentType string
	formBody    string
	rawBody     string
}

// newMockTokenServer 启动一个模拟飞书 token 端点的 httptest server。
// respondWith 会作为 JSON body 返回；若为 nil 则返回空 JSON {}。
func newMockTokenServer(t *testing.T, statusCode int, respondWith map[string]any) (*httptest.Server, *tokenEndpointCapture) {
	t.Helper()
	capture := &tokenEndpointCapture{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.contentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		capture.rawBody = string(raw)
		capture.formBody = string(raw)

		w.WriteHeader(statusCode)
		if respondWith == nil {
			_, _ = w.Write([]byte("{}"))
			return
		}
		_ = json.NewEncoder(w).Encode(respondWith)
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func TestRefreshAccessToken_UsesFormURLEncoded(t *testing.T) {
	srv, cap := newMockTokenServer(t, http.StatusOK, map[string]any{
		"access_token":             "new-access",
		"refresh_token":            "new-refresh",
		"expires_in":               7200,
		"refresh_token_expires_in": 604800,
		"scope":                    "search:docs:read",
	})
	// 模拟 feishu 的 baseURL 末尾不带 /open-apis/authen/v2/oauth/token
	// RefreshAccessToken 会自己拼接该路径，所以测试 server 需要挂在根路径 + 该子路径
	// 简单做法：直接让 server 接受任意路径（newMockTokenServer 已如此）

	old := &TokenStore{
		RefreshToken:     "old-refresh",
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		Scope:            "old:scope",
	}

	fresh, err := RefreshAccessToken(old, "app-id", "app-secret", srv.URL)
	if err != nil {
		t.Fatalf("RefreshAccessToken error: %v", err)
	}

	if !strings.Contains(cap.contentType, "application/x-www-form-urlencoded") {
		t.Errorf("期望 Content-Type 为 form-urlencoded，实际 %q", cap.contentType)
	}
	// body 应是 form 编码（如 grant_type=refresh_token&refresh_token=old-refresh&...）
	wantFragments := []string{
		"grant_type=refresh_token",
		"refresh_token=old-refresh",
		"client_id=app-id",
		"client_secret=app-secret",
	}
	for _, f := range wantFragments {
		if !strings.Contains(cap.formBody, f) {
			t.Errorf("请求 body 缺少片段 %q；实际 body=%q", f, cap.formBody)
		}
	}
	if fresh.AccessToken != "new-access" {
		t.Errorf("AccessToken got %q want new-access", fresh.AccessToken)
	}
	if fresh.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken got %q want new-refresh", fresh.RefreshToken)
	}
	if fresh.Scope != "search:docs:read" {
		t.Errorf("Scope got %q want search:docs:read", fresh.Scope)
	}
}

// 核心兜底测试：响应缺 refresh_token 时必须复用 oldStore.RefreshToken
// 这是 issue #94 的根因——旧实现会把空字符串覆盖进去，下次过期就彻底失效
func TestRefreshAccessToken_FallbackReuseRefreshToken(t *testing.T) {
	srv, _ := newMockTokenServer(t, http.StatusOK, map[string]any{
		"access_token": "new-access",
		// 故意不返回 refresh_token
		"expires_in": 7200,
	})

	old := &TokenStore{
		RefreshToken:     "should-be-preserved",
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		Scope:            "old:scope",
	}
	fresh, err := RefreshAccessToken(old, "aid", "sec", srv.URL)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if fresh.RefreshToken != "should-be-preserved" {
		t.Errorf("RefreshToken 应复用原值 should-be-preserved，实际 %q", fresh.RefreshToken)
	}
}

// 响应缺 refresh_token_expires_in 时必须复用 oldStore.RefreshExpiresAt
func TestRefreshAccessToken_FallbackReuseRefreshExpiresAt(t *testing.T) {
	oldExpires := time.Now().Add(6 * 24 * time.Hour)

	srv, _ := newMockTokenServer(t, http.StatusOK, map[string]any{
		"access_token":  "new-access",
		"refresh_token": "new-refresh",
		"expires_in":    7200,
		// refresh_token_expires_in 为 0 / 缺失
	})
	old := &TokenStore{
		RefreshToken:     "old-refresh",
		RefreshExpiresAt: oldExpires,
	}
	fresh, err := RefreshAccessToken(old, "aid", "sec", srv.URL)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !fresh.RefreshExpiresAt.Equal(oldExpires) {
		t.Errorf("RefreshExpiresAt 应复用原值 %v，实际 %v", oldExpires, fresh.RefreshExpiresAt)
	}
}

// 响应缺 scope 时必须复用 oldStore.Scope
func TestRefreshAccessToken_FallbackReuseScope(t *testing.T) {
	srv, _ := newMockTokenServer(t, http.StatusOK, map[string]any{
		"access_token":  "new-access",
		"refresh_token": "new-refresh",
		"expires_in":    7200,
	})
	old := &TokenStore{
		RefreshToken: "old-refresh",
		Scope:        "old:scope a:b",
	}
	fresh, err := RefreshAccessToken(old, "aid", "sec", srv.URL)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if fresh.Scope != "old:scope a:b" {
		t.Errorf("Scope 应复用原值，实际 %q", fresh.Scope)
	}
}

// refresh_token_expires_in > 0 时应采用新值
func TestRefreshAccessToken_UpdateRefreshExpiresWhenProvided(t *testing.T) {
	srv, _ := newMockTokenServer(t, http.StatusOK, map[string]any{
		"access_token":             "new-access",
		"refresh_token":            "new-refresh",
		"expires_in":               7200,
		"refresh_token_expires_in": 3600, // 1h
	})
	old := &TokenStore{
		RefreshToken:     "old-refresh",
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}
	before := time.Now()
	fresh, err := RefreshAccessToken(old, "aid", "sec", srv.URL)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// 新过期时间应在当前时间 + 3600s 附近（误差几秒）
	delta := fresh.RefreshExpiresAt.Sub(before)
	if delta < 3500*time.Second || delta > 3700*time.Second {
		t.Errorf("RefreshExpiresAt 应为当前时间 + ~3600s，实际偏移 %v", delta)
	}
}

// oldStore 为 nil 或无 refresh_token 应快速失败
func TestRefreshAccessToken_RejectsNilOrEmpty(t *testing.T) {
	if _, err := RefreshAccessToken(nil, "aid", "sec", "https://example.com"); err == nil {
		t.Error("传入 nil 应该返回错误")
	}
	if _, err := RefreshAccessToken(&TokenStore{}, "aid", "sec", "https://example.com"); err == nil {
		t.Error("传入空 refresh_token 应该返回错误")
	}
}

// 端点返回 error 字段应传递错误
func TestRefreshAccessToken_PropagatesServerError(t *testing.T) {
	srv, _ := newMockTokenServer(t, http.StatusOK, map[string]any{
		"error":             "invalid_grant",
		"error_description": "refresh_token expired",
	})
	old := &TokenStore{RefreshToken: "x"}
	_, err := RefreshAccessToken(old, "aid", "sec", srv.URL)
	if err == nil {
		t.Fatal("期望 error 字段触发失败，但返回 nil")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("错误信息应包含 invalid_grant，实际 %v", err)
	}
}

// HTTP 非 200 应返回错误
func TestRefreshAccessToken_PropagatesHTTPError(t *testing.T) {
	srv, _ := newMockTokenServer(t, http.StatusInternalServerError, map[string]any{
		"message": "server error",
	})
	old := &TokenStore{RefreshToken: "x"}
	_, err := RefreshAccessToken(old, "aid", "sec", srv.URL)
	if err == nil {
		t.Fatal("期望 HTTP 500 触发失败")
	}
}

func TestRefreshAccessToken_RedactsSecretsInHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","access_token":"u-should-hide","refresh_token":"r-should-hide"}`))
	}))
	t.Cleanup(srv.Close)
	_, err := RefreshAccessToken(&TokenStore{RefreshToken: "x"}, "aid", "sec", srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "u-should-hide") || strings.Contains(err.Error(), "r-should-hide") {
		t.Fatalf("错误预览泄漏 token: %v", err)
	}
}
