package auth

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/riba2534/feishu-cli/internal/config"
)

func TestDefaultTATEndpoint(t *testing.T) {
	if got := DefaultTATEndpoint(""); got != "https://accounts.feishu.cn/oauth/v3/token" {
		t.Fatalf("feishu endpoint = %s", got)
	}
	if got := DefaultTATEndpoint("https://open.larksuite.com"); got != "https://accounts.larksuite.com/oauth/v3/token" {
		t.Fatalf("lark endpoint = %s", got)
	}
}

func TestDefaultTATEndpoint_LoopbackOverrideOnly(t *testing.T) {
	t.Setenv("FEISHU_TAT_ENDPOINT", "https://evil.example/oauth/v3/token")
	if got := DefaultTATEndpoint(""); got != "https://accounts.feishu.cn/oauth/v3/token" {
		t.Fatalf("远端覆盖必须忽略，得到 %s", got)
	}
	t.Setenv("FEISHU_TAT_ENDPOINT", "ftp://127.0.0.1:21/oauth/v3/token")
	if got := DefaultTATEndpoint(""); got != "https://accounts.feishu.cn/oauth/v3/token" {
		t.Fatalf("非 http(s) loopback 覆盖必须忽略，得到 %s", got)
	}
	t.Setenv("FEISHU_TAT_ENDPOINT", "http://127.0.0.1:9/oauth/v3/token")
	if got := DefaultTATEndpoint(""); got != "http://127.0.0.1:9/oauth/v3/token" {
		t.Fatalf("loopback 覆盖应生效，得到 %s", got)
	}
}

func TestFetchTenantAccessToken_ClientCredentialsContract(t *testing.T) {
	var (
		gotMethod string
		gotCT     string
		gotBody   string
		gotHost   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotHost = r.Host
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"access_token":"t-test-token","token_type":"Bearer","expires_in":7200}`))
	}))
	defer srv.Close()

	orig := TATEndpointFunc
	TATEndpointFunc = func(string) string { return srv.URL }
	t.Cleanup(func() { TATEndpointFunc = orig })

	token, err := FetchTenantAccessToken("cli_app", "secret_x", "https://open.feishu.cn")
	if err != nil {
		t.Fatalf("FetchTenantAccessToken: %v", err)
	}
	if token != "t-test-token" {
		t.Fatalf("token = %q", token)
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
	_ = gotHost
}

func TestFetchTenantAccessToken_HTTPAndBusinessErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"oauth invalid_client", 400, `{"error":"invalid_client","error_description":"The client secret is invalid.","code":20002}`, "invalid_client"},
		{"legacy code", 200, `{"code":99991663,"msg":"app secret invalid"}`, "99991663"},
		{"http 500", 500, `{"error":"server_error"}`, "暂时失败"},
		{"missing token", 200, `{"code":0}`, "access_token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			orig := TATEndpointFunc
			TATEndpointFunc = func(string) string { return srv.URL }
			t.Cleanup(func() { TATEndpointFunc = orig })

			_, err := FetchTenantAccessToken("cli_app", "secret_x", "https://open.feishu.cn")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q 应包含 %q", err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), "secret_x") {
				t.Fatalf("错误信息泄漏 secret: %v", err)
			}
		})
	}
}

func TestFetchTenantAccessToken_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"code":0,"access_token":"t-late"}`))
	}))
	defer srv.Close()
	orig := TATEndpointFunc
	TATEndpointFunc = func(string) string { return srv.URL }
	t.Cleanup(func() { TATEndpointFunc = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := fetchTenantAccessToken(ctx, config.NewHTTPClient(20*time.Millisecond), "cli_app", "secret_x", "https://open.feishu.cn")
	if err == nil {
		t.Fatal("超时应返回错误")
	}
}

func TestFetchTenantAccessToken_RedactsSecretsAndRejectsOversize(t *testing.T) {
	t.Run("redact", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"access_token=u-leak client_secret=s-leak"}`))
		}))
		t.Cleanup(srv.Close)
		orig := TATEndpointFunc
		TATEndpointFunc = func(string) string { return srv.URL }
		t.Cleanup(func() { TATEndpointFunc = orig })
		_, err := FetchTenantAccessToken("cli_app", "secret_x", "https://open.feishu.cn")
		if err == nil {
			t.Fatal("expected error")
		}
		if strings.Contains(err.Error(), "u-leak") || strings.Contains(err.Error(), "s-leak") {
			t.Fatalf("错误预览泄漏 secret: %v", err)
		}
		if !strings.Contains(err.Error(), "[REDACTED]") {
			t.Fatalf("应脱敏: %v", err)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(bytes.Repeat([]byte("x"), maxAuthResponseBytes+1))
		}))
		t.Cleanup(srv.Close)
		orig := TATEndpointFunc
		TATEndpointFunc = func(string) string { return srv.URL }
		t.Cleanup(func() { TATEndpointFunc = orig })
		_, err := FetchTenantAccessToken("cli_app", "secret_x", "https://open.feishu.cn")
		if err == nil || !strings.Contains(err.Error(), "1MiB") {
			t.Fatalf("超过 1MiB 必须报错: %v", err)
		}
	})
}
