package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riba2534/feishu-cli/internal/auth"
	"github.com/riba2534/feishu-cli/internal/config"
)

func TestAuthToken_AsBotConflictsWithUserAccessToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FEISHU_APP_ID", "cli_app")
	t.Setenv("FEISHU_APP_SECRET", "secret_x")
	t.Setenv("FEISHU_BASE_URL", "https://open.feishu.cn")
	config.SetBotFlagCredentials("", "")
	if err := os.MkdirAll(filepath.Join(home, ".feishu-cli"), 0700); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runCLI(t, "auth", "token", "--as", "bot", "--user-access-token", "u-explicit")
	if err == nil {
		t.Fatal("应拒绝 --as bot 与 --user-access-token 同时使用")
	}
	msg := err.Error() + stderr
	if !strings.Contains(msg, "--as bot") || !strings.Contains(msg, "--user-access-token") {
		t.Fatalf("错误应说明冲突: %s", msg)
	}
}

func TestAuthToken_AsBotUsesOAuthV3ClientCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FEISHU_APP_ID", "cli_app")
	t.Setenv("FEISHU_APP_SECRET", "secret_x")
	t.Setenv("FEISHU_BASE_URL", "https://open.feishu.cn")
	config.SetBotFlagCredentials("", "")
	if err := os.MkdirAll(filepath.Join(home, ".feishu-cli"), 0700); err != nil {
		t.Fatal(err)
	}

	var (
		gotMethod string
		gotCT     string
		gotBody   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"code":0,"access_token":"t-bot-token","token_type":"Bearer","expires_in":7200}`))
	}))
	t.Cleanup(srv.Close)

	orig := auth.TATEndpointFunc
	auth.TATEndpointFunc = func(string) string { return srv.URL }
	t.Cleanup(func() { auth.TATEndpointFunc = orig })

	stdout, _, err := runCLI(t, "auth", "token", "--as", "bot")
	if err != nil {
		t.Fatalf("auth token --as bot: %v", err)
	}
	if strings.TrimSpace(stdout) != "t-bot-token" {
		t.Fatalf("stdout = %q", stdout)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s", gotMethod)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q", gotCT)
	}
	for _, want := range []string{"grant_type=client_credentials", "client_id=cli_app", "client_secret=secret_x"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body missing %q: %s", want, gotBody)
		}
	}
}

func TestAuthToken_BindLegacyApp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FEISHU_APP_ID", "cli_app")
	t.Setenv("FEISHU_APP_SECRET", "secret_x")
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "")
	config.SetBotFlagCredentials("", "")

	dir := filepath.Join(home, ".feishu-cli")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store := auth.TokenStore{
		AccessToken:      "u-legacy-token",
		RefreshToken:     "r-legacy",
		ExpiresAt:        time.Now().Add(time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}
	raw, _ := json.MarshalIndent(store, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "token.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCLI(t, "auth", "token", "--bind-legacy-app", "--as", "user")
	if err != nil {
		t.Fatalf("bind-legacy-app: %v / %s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "u-legacy-token" {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "cli_app") {
		t.Fatalf("stderr 应提示绑定 app_id: %s", stderr)
	}
	loaded, err := auth.LoadToken()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AppID != "cli_app" || loaded.AccessToken != "u-legacy-token" {
		t.Fatalf("绑定结果不正确: %+v", loaded)
	}
}
