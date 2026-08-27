package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func captureAPIStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b), runErr
}

func TestPageCursorFromData(t *testing.T) {
	tok, kind := pageCursorFromData(map[string]any{"page_token": "n1"})
	if tok != "n1" || kind != "page_token" {
		t.Fatalf("got %q %q", tok, kind)
	}
	tok, kind = pageCursorFromData(map[string]any{"next_page_token": "n2"})
	if tok != "n2" || kind != "next_page_token" {
		t.Fatalf("got %q %q", tok, kind)
	}
	tok, kind = pageCursorFromData(map[string]any{"has_more": true})
	if tok != "" || kind != "missing" {
		t.Fatalf("missing cursor got %q %q", tok, kind)
	}
	_, kind = pageCursorFromData(map[string]any{"page_token": json.Number("1")})
	if kind != "nonstring" {
		t.Fatalf("nonstring kind = %q", kind)
	}
}

func TestMergeAPIPages_PreservesLargeInts(t *testing.T) {
	pages := []map[string]any{
		{
			"code": json.Number("0"),
			"msg":  "ok",
			"data": map[string]any{
				"items":      []any{map[string]any{"id": json.Number("9007199254740993")}},
				"has_more":   true,
				"page_token": "n1",
			},
		},
		{
			"code": json.Number("0"),
			"msg":  "ok",
			"data": map[string]any{
				"items":    []any{map[string]any{"id": json.Number("9007199254740994")}},
				"has_more": false,
			},
		},
	}
	merged := mergeAPIPages(pages)
	raw, err := marshalPreserveNumbers(merged)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "9007199254740993") || !strings.Contains(s, "9007199254740994") {
		t.Fatalf("大整数被截断: %s", s)
	}
	if strings.Contains(s, "page_token") {
		t.Fatalf("聚合结果不应保留 page_token: %s", s)
	}
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		t.Fatal(err)
	}
	data := obj["data"].(map[string]any)
	if data["has_more"] != false {
		t.Fatalf("has_more = %v, want false", data["has_more"])
	}
	items := data["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items len = %d", len(items))
	}
}

func TestRunAPI_PageAllTwoPages(t *testing.T) {
	isolateAPITestEnv(t)
	defer resetAPIFlags()

	calls := 0
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-fake","expire":7200}`)
			return
		}
		if r.URL.Path != "/open-apis/im/v1/chats" {
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		tok := r.URL.Query().Get("page_token")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"9007199254740993"}],"has_more":true,"page_token":"next-1"}}`)
		case "next-1":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"9007199254740994"}],"has_more":false}}`)
		default:
			t.Errorf("unexpected page_token %q", tok)
			_, _ = fmt.Fprint(w, `{"code":1,"msg":"bad token"}`)
		}
	})
	defer cleanup()

	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	apiFormat = "json"
	out, err := captureAPIStdout(t, func() error {
		return cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	})
	if err != nil {
		t.Fatalf("page-all: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if !strings.Contains(out, "9007199254740993") || !strings.Contains(out, "9007199254740994") {
		t.Fatalf("两页未聚合或大整数丢失:\n%s", out)
	}
	if strings.Contains(out, `"has_more": true`) || strings.Contains(out, `"has_more":true`) {
		t.Fatalf("自然结束应 has_more=false:\n%s", out)
	}
}

func TestRunAPI_PageAllEmptyCursorStops(t *testing.T) {
	isolateAPITestEnv(t)
	defer resetAPIFlags()
	calls := 0
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-fake","expire":7200}`)
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"1"}],"has_more":true,"page_token":""}}`)
	})
	defer cleanup()

	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	err := cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("空 cursor 应停止并报错, err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("空 cursor 后不应继续翻页, calls=%d", calls)
	}
}

func TestRunAPI_PageAllRepeatedCursorStops(t *testing.T) {
	isolateAPITestEnv(t)
	defer resetAPIFlags()
	calls := 0
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-fake","expire":7200}`)
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"1"}],"has_more":true,"page_token":"same"}}`)
	})
	defer cleanup()

	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	err := cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "重复游标") {
		t.Fatalf("重复 cursor 应停止并报错, err=%v", err)
	}
	if calls != 2 {
		t.Fatalf("重复 cursor 应在第二页发现, calls=%d", calls)
	}
}

func TestRunAPI_PageAllBusinessCode(t *testing.T) {
	isolateAPITestEnv(t)
	defer resetAPIFlags()
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-fake","expire":7200}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"code":99991679,"msg":"Unauthorized."}`)
	})
	defer cleanup()

	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	err := cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "99991679") {
		t.Fatalf("业务 code 应非零退出, err=%v", err)
	}
}

func TestRunAPI_UnofficialFullURLRejectedBeforeNetwork(t *testing.T) {
	isolateAPITestEnv(t)
	defer resetAPIFlags()
	hits := 0
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-fake","expire":7200}`)
			return
		}
		hits++
		http.Error(w, "should not be called", 500)
	})
	defer cleanup()
	cmd := newTestAPICmd()
	apiAs = "bot"
	err := cmd.RunE(cmd, []string{"GET", "https://evil.example/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "官方 OpenAPI host") {
		t.Fatalf("非官方完整 URL 应被拒绝, err=%v", err)
	}
	if hits != 0 {
		t.Fatalf("拒绝后不应发业务请求, hits=%d", hits)
	}
}

func TestRunAPI_PageAllDryRunNoNetwork(t *testing.T) {
	isolateAPITestEnv(t)
	defer resetAPIFlags()
	hits := 0
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
	})
	defer cleanup()
	cmd := newTestAPICmd()
	apiDryRun = true
	apiPageAll = true
	apiAs = "bot"
	_, err := captureAPIStdout(t, func() error {
		return cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatalf("dry-run 不应发请求, hits=%d", hits)
	}
}

func TestNewTestAPICmdHasPageFlags(t *testing.T) {
	cmd := newTestAPICmd()
	if cmd.Flags().Lookup("page-all") == nil || cmd.Flags().Lookup("page-limit") == nil {
		t.Fatal("测试命令缺少分页 flag")
	}
}
