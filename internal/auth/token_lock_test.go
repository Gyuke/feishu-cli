package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSaveToken_AtomicWriteKeepsOldFileOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	t.Cleanup(func() { tokenPathFunc = originalTokenPath })

	old := &TokenStore{AccessToken: "old-access", RefreshToken: "old-refresh", AppID: "cli_a"}
	if err := SaveToken(old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmpDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, 0700) })

	err := SaveToken(&TokenStore{AccessToken: "new-access", AppID: "cli_a"})
	if err == nil {
		t.Fatal("只读目录写入应失败")
	}

	_ = os.Chmod(tmpDir, 0700)
	got, err := LoadToken()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AccessToken != "old-access" {
		t.Fatalf("写失败后应保留旧文件，得到 %+v", got)
	}
}

func TestSaveToken_Permissions0600(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	t.Cleanup(func() { tokenPathFunc = originalTokenPath })

	if err := SaveToken(&TokenStore{AccessToken: "a", AppID: "cli_a"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("perm = %o, want 0600", perm)
	}
}

func TestBindLegacyTokenAndMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	t.Cleanup(func() { tokenPathFunc = originalTokenPath })

	unbound := &TokenStore{
		AccessToken:      "u-legacy",
		RefreshToken:     "r-legacy",
		ExpiresAt:        time.Now().Add(time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := SaveToken(unbound); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveUserAccessToken("", "", "cli_new", "sec", "")
	if err == nil || !errors.Is(err, ErrUnboundToken) {
		t.Fatalf("未绑定 token 即使 access 有效也必须 fail closed: %v", err)
	}

	expired := unbound
	expired.AccessToken = "u-expired"
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	if err := SaveToken(expired); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveUserAccessToken("", "", "cli_new", "sec", "http://127.0.0.1:1")
	if err == nil || !errors.Is(err, ErrUnboundToken) {
		t.Fatalf("过期未绑定 token 刷新应 fail closed: %v", err)
	}

	if err := BindLegacyToken("cli_new"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadToken()
	if loaded.AppID != "cli_new" || loaded.AccessToken != "u-expired" {
		t.Fatalf("绑定不得更换 token: %+v", loaded)
	}

	if err := BindLegacyToken("cli_other"); err == nil || !errors.Is(err, ErrAppMismatch) {
		t.Fatalf("绑定到其他 app 应 fail closed: %v", err)
	}

	_, err = ResolveUserAccessToken("", "", "cli_other", "sec", "")
	if err == nil || !errors.Is(err, ErrAppMismatch) {
		t.Fatalf("app mismatch 应 fail closed: %v", err)
	}
}

func TestConcurrentRefreshCommitsOnce(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	t.Cleanup(func() { tokenPathFunc = originalTokenPath })

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(80 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh-once",
			"refresh_token": "fresh-rt",
			"expires_in":    7200,
		})
	}))
	t.Cleanup(srv.Close)

	if err := SaveToken(&TokenStore{
		AccessToken:      "stale",
		RefreshToken:     "shared-rt",
		ExpiresAt:        time.Now().Add(-time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		AppID:            "aid",
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([]string, 8)
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok, err := ResolveUserAccessToken("", "", "aid", "sec", srv.URL)
			results[i] = tok
			errs[i] = err
		}(i)
	}
	wg.Wait()

	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("并发刷新应只提交一次，实际 %d 次请求", hits)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if results[i] != "fresh-once" {
			t.Fatalf("goroutine %d token = %q", i, results[i])
		}
	}
	raw, _ := os.ReadFile(tokenFile)
	var stored TokenStore
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "fresh-once" || stored.AppID != "aid" {
		t.Fatalf("落盘 token 不正确: %+v", stored)
	}
}

func TestLoadTokenFrom_RecoversWindowsBak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	bak := &TokenStore{AccessToken: "from-bak", RefreshToken: "r", AppID: "cli_a"}
	raw, _ := json.Marshal(bak)
	if err := os.WriteFile(path+".bak", raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTokenFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AccessToken != "from-bak" {
		t.Fatalf("应恢复 .bak，得到 %+v", got)
	}
}

func TestDeleteTokenRemovesBak(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	t.Cleanup(func() { tokenPathFunc = originalTokenPath })

	bak := &TokenStore{AccessToken: "from-bak", AppID: "cli_a"}
	raw, _ := json.Marshal(bak)
	if err := os.WriteFile(tokenFile+".bak", raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteToken(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTokenFrom(tokenFile)
	if err != nil || got != nil {
		t.Fatalf("logout 后不得从 .bak 复活: got=%+v err=%v", got, err)
	}
}

func TestSaveTokenRemovesStaleBak(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	t.Cleanup(func() { tokenPathFunc = originalTokenPath })
	if err := os.WriteFile(tokenFile+".bak", []byte(`{"access_token":"stale-bak"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveToken(&TokenStore{AccessToken: "new", AppID: "cli_a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenFile + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("成功 Save 后应删除陈旧 .bak: %v", err)
	}
}

func TestAtomicWrite_DirSyncFailureAfterRenameIsNonFatal(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	t.Cleanup(func() { tokenPathFunc = originalTokenPath })
	if err := SaveToken(&TokenStore{AccessToken: "old", AppID: "cli_a"}); err != nil {
		t.Fatal(err)
	}

	orig := dirSyncFn
	dirSyncFn = func(string) error { return errors.New("injected dir sync failure") }
	t.Cleanup(func() { dirSyncFn = orig })

	if err := SaveToken(&TokenStore{AccessToken: "new-committed", AppID: "cli_a"}); err != nil {
		t.Fatalf("rename 已提交后 dir fsync 失败不得当成未改动: %v", err)
	}
	got, err := LoadToken()
	if err != nil || got == nil || got.AccessToken != "new-committed" {
		t.Fatalf("新 token 应已提交: %+v err=%v", got, err)
	}
}

func TestConcurrentForceRefreshCommitsOnce(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.json")
	tokenPathFunc = func() (string, error) { return tokenFile, nil }
	t.Cleanup(func() { tokenPathFunc = originalTokenPath })

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(80 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "force-once",
			"refresh_token": "force-rt",
			"expires_in":    7200,
		})
	}))
	t.Cleanup(srv.Close)

	if err := SaveToken(&TokenStore{
		AccessToken:      "still-valid",
		RefreshToken:     "shared-rt",
		ExpiresAt:        time.Now().Add(time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		AppID:            "aid",
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := ForceRefreshLocalToken("aid", "sec", srv.URL)
			errs[i] = err
		}(i)
	}
	wg.Wait()
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("并发 force refresh 不得二次消耗新 refresh token，实际 %d 次请求", hits)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	loaded, _ := LoadToken()
	if loaded.AccessToken != "force-once" || loaded.RefreshToken != "force-rt" {
		t.Fatalf("落盘 %+v", loaded)
	}
}
