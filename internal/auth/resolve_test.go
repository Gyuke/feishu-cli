package auth

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveUserAccessToken_FlagValue(t *testing.T) {
	token, err := ResolveUserAccessToken("flag-token", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "flag-token" {
		t.Errorf("got %q, want %q", token, "flag-token")
	}
}

func TestResolveUserAccessToken_EnvVar(t *testing.T) {
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "env-token")

	token, err := ResolveUserAccessToken("", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "env-token" {
		t.Errorf("got %q, want %q", token, "env-token")
	}
}

func TestResolveUserAccessToken_TokenFile(t *testing.T) {
	// Clear env var
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "")

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	// Save a valid token
	store := &TokenStore{
		AccessToken: "file-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	token, err := ResolveUserAccessToken("", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "file-token" {
		t.Errorf("got %q, want %q", token, "file-token")
	}
}

func TestResolveUserAccessToken_ConfigValue(t *testing.T) {
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "")

	// Point to nonexistent file
	tokenPathFunc = func() (string, error) { return filepath.Join(t.TempDir(), "nope.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	token, err := ResolveUserAccessToken("", "config-token", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "config-token" {
		t.Errorf("got %q, want %q", token, "config-token")
	}
}

func TestResolveUserAccessToken_AllEmpty(t *testing.T) {
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "")
	tokenPathFunc = func() (string, error) { return filepath.Join(t.TempDir(), "nope.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	_, err := ResolveUserAccessToken("", "", "", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveUserAccessToken_Priority(t *testing.T) {
	// Flag > env > file > config
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "env-token")

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	store := &TokenStore{
		AccessToken: "file-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	_ = SaveToken(store)

	// Flag wins
	token, _ := ResolveUserAccessToken("flag-token", "config-token", "", "", "")
	if token != "flag-token" {
		t.Errorf("got %q, want flag-token", token)
	}

	// Env wins when no flag
	token, _ = ResolveUserAccessToken("", "config-token", "", "", "")
	if token != "env-token" {
		t.Errorf("got %q, want env-token", token)
	}

	// File wins when no flag/env
	os.Unsetenv("FEISHU_USER_ACCESS_TOKEN")
	token, _ = ResolveUserAccessToken("", "config-token", "", "", "")
	if token != "file-token" {
		t.Errorf("got %q, want file-token", token)
	}
}

// ----- refreshIfStaleLocalToken -----

// TestRefreshIfStaleLocalToken_NotMatchingLocal: 显式 token 与本地 token.json 不一致时，
// 应直接返回 ("", false)，调用方继续使用原始 token。
func TestRefreshIfStaleLocalToken_NotMatchingLocal(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPathFunc = func() (string, error) { return filepath.Join(tmpDir, "token.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	// 本地 token 与传入的 explicit token 不一致
	store := &TokenStore{
		AccessToken:      "local-access",
		RefreshToken:     "local-refresh",
		ExpiresAt:        time.Now().Add(-1 * time.Hour), // 已过期
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	got, ok := refreshIfStaleLocalToken("some-other-token", "aid", "sec", "https://example.com")
	if ok {
		t.Errorf("expected ok=false when explicit token doesn't match local, got ok=true (got=%q)", got)
	}
	if got != "" {
		t.Errorf("expected empty string when not matching, got %q", got)
	}
}

// TestRefreshIfStaleLocalToken_AccessStillValid: explicit token 与本地一致且尚未过期，
// 不需要刷新，返回 ("", false) 让调用方使用原值。
func TestRefreshIfStaleLocalToken_AccessStillValid(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPathFunc = func() (string, error) { return filepath.Join(tmpDir, "token.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	store := &TokenStore{
		AccessToken:      "still-valid",
		RefreshToken:     "rt",
		ExpiresAt:        time.Now().Add(1 * time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	got, ok := refreshIfStaleLocalToken("still-valid", "aid", "sec", "https://example.com")
	if ok {
		t.Errorf("expected ok=false when access token still valid, got ok=true (got=%q)", got)
	}
	if got != "" {
		t.Errorf("expected empty string when not refreshing, got %q", got)
	}
}

// TestRefreshIfStaleLocalToken_RefreshExpired: explicit token 匹配但 access 与 refresh 都失效，
// 无能为力，返回 ("", false)。
func TestRefreshIfStaleLocalToken_RefreshExpired(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPathFunc = func() (string, error) { return filepath.Join(tmpDir, "token.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	store := &TokenStore{
		AccessToken:      "stale",
		RefreshToken:     "rt",
		ExpiresAt:        time.Now().Add(-1 * time.Hour),
		RefreshExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	got, ok := refreshIfStaleLocalToken("stale", "aid", "sec", "https://example.com")
	if ok {
		t.Errorf("expected ok=false when both tokens expired, got ok=true (got=%q)", got)
	}
}

// TestRefreshIfStaleLocalToken_Success: explicit token 匹配本地过期 access，且 refresh 仍有效，
// 走 RefreshAccessToken 并返回新 token。
func TestRefreshIfStaleLocalToken_Success(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPathFunc = func() (string, error) { return filepath.Join(tmpDir, "token.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	store := &TokenStore{
		AccessToken:      "stale-access",
		RefreshToken:     "valid-refresh",
		ExpiresAt:        time.Now().Add(-1 * time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		Scope:            "old:scope",
		AppID:            "aid",
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	srv, _ := newMockTokenServer(t, http.StatusOK, map[string]any{
		"access_token":  "fresh-access",
		"refresh_token": "fresh-refresh",
		"expires_in":    7200,
		"scope":         "old:scope",
	})

	got, ok := refreshIfStaleLocalToken("stale-access", "aid", "sec", srv.URL)
	if !ok {
		t.Fatalf("expected ok=true on successful refresh, got ok=false")
	}
	if got != "fresh-access" {
		t.Errorf("expected fresh-access, got %q", got)
	}

	// 验证 token.json 已更新
	updated, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken error: %v", err)
	}
	if updated.AccessToken != "fresh-access" {
		t.Errorf("token.json AccessToken got %q want fresh-access", updated.AccessToken)
	}
}

// TestRefreshIfStaleLocalToken_RefreshEndpointFails: 刷新端点返回错误时，
// 不更新 token.json，返回 ("", false) 让调用方降级使用原始 explicit token。
func TestRefreshIfStaleLocalToken_RefreshEndpointFails(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPathFunc = func() (string, error) { return filepath.Join(tmpDir, "token.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	store := &TokenStore{
		AccessToken:      "stale-access",
		RefreshToken:     "valid-refresh",
		ExpiresAt:        time.Now().Add(-1 * time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		AppID:            "aid",
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	srv, _ := newMockTokenServer(t, http.StatusInternalServerError, map[string]any{
		"error": "server_error",
	})

	got, ok := refreshIfStaleLocalToken("stale-access", "aid", "sec", srv.URL)
	if ok {
		t.Errorf("expected ok=false on refresh failure, got ok=true (got=%q)", got)
	}
	if got != "" {
		t.Errorf("expected empty string on failure, got %q", got)
	}

	// 确认 token.json 未变更
	unchanged, _ := LoadToken()
	if unchanged.AccessToken != "stale-access" {
		t.Errorf("expected token.json unchanged, got AccessToken=%q", unchanged.AccessToken)
	}
}

// ----- ForceRefreshLocalToken -----

// TestForceRefreshLocalToken_NoTokenFile: token.json 不存在时返回明确错误。
func TestForceRefreshLocalToken_NoTokenFile(t *testing.T) {
	tokenPathFunc = func() (string, error) { return filepath.Join(t.TempDir(), "nope.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	_, err := ForceRefreshLocalToken("aid", "sec", "https://example.com")
	if err == nil {
		t.Fatal("expected error when token.json missing, got nil")
	}
}

// TestForceRefreshLocalToken_NoRefreshToken: token.json 存在但缺 refresh_token。
func TestForceRefreshLocalToken_NoRefreshToken(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPathFunc = func() (string, error) { return filepath.Join(tmpDir, "token.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	store := &TokenStore{
		AccessToken: "only-access",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	_, err := ForceRefreshLocalToken("aid", "sec", "https://example.com")
	if err == nil {
		t.Fatal("expected error when refresh_token missing, got nil")
	}
}

// TestForceRefreshLocalToken_RefreshExpired: refresh_token 已过期时返回错误。
func TestForceRefreshLocalToken_RefreshExpired(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPathFunc = func() (string, error) { return filepath.Join(tmpDir, "token.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	store := &TokenStore{
		AccessToken:      "any",
		RefreshToken:     "rt",
		ExpiresAt:        time.Now().Add(1 * time.Hour),
		RefreshExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	_, err := ForceRefreshLocalToken("aid", "sec", "https://example.com")
	if err == nil {
		t.Fatal("expected error when refresh_token expired, got nil")
	}
}

// TestForceRefreshLocalToken_Success: 正常路径，调用 mock token server 完成刷新。
func TestForceRefreshLocalToken_Success(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPathFunc = func() (string, error) { return filepath.Join(tmpDir, "token.json"), nil }
	defer func() { tokenPathFunc = originalTokenPath }()

	store := &TokenStore{
		AccessToken:      "old-access",
		RefreshToken:     "valid-refresh",
		ExpiresAt:        time.Now().Add(1 * time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		Scope:            "scope",
		AppID:            "aid",
	}
	if err := SaveToken(store); err != nil {
		t.Fatalf("SaveToken error: %v", err)
	}

	srv, _ := newMockTokenServer(t, http.StatusOK, map[string]any{
		"access_token":  "force-fresh-access",
		"refresh_token": "force-fresh-refresh",
		"expires_in":    7200,
		"scope":         "scope",
	})

	got, err := ForceRefreshLocalToken("aid", "sec", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AccessToken != "force-fresh-access" {
		t.Errorf("AccessToken got %q want force-fresh-access", got.AccessToken)
	}

	// token.json 应同步更新
	updated, _ := LoadToken()
	if updated.AccessToken != "force-fresh-access" {
		t.Errorf("token.json AccessToken got %q want force-fresh-access", updated.AccessToken)
	}
}
