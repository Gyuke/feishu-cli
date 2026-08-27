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
	merged, err := mergeAPIPages(pages, false, "items")
	if err != nil {
		t.Fatal(err)
	}
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

func TestMergeAPIPages_TruncatedPreservesCursor(t *testing.T) {
	pages := []map[string]any{
		{
			"code": json.Number("0"),
			"data": map[string]any{
				"items":      []any{map[string]any{"id": "1"}},
				"has_more":   true,
				"page_token": "next-keep",
			},
		},
	}
	merged, err := mergeAPIPages(pages, true, "items")
	if err != nil {
		t.Fatal(err)
	}
	data := merged["data"].(map[string]any)
	if data["has_more"] != true {
		t.Fatalf("has_more=%v", data["has_more"])
	}
	if data["page_token"] != "next-keep" {
		t.Fatalf("应保留续翻游标, got %v", data["page_token"])
	}
	if data["truncated"] != true {
		t.Fatalf("truncated=%v", data["truncated"])
	}
	if data["page_count"] != 1 {
		t.Fatalf("page_count=%v", data["page_count"])
	}
}

func TestRunAPI_PageLimitPreservesResumeCursor(t *testing.T) {
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
		_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"1"}],"has_more":true,"page_token":"resume-me"}}`)
	})
	defer cleanup()
	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageLimit = 1
	apiPageDelayMs = 0
	apiFormat = "json"
	out, err := captureAPIStdout(t, func() error {
		return cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	})
	if err != nil {
		t.Fatalf("truncated page-all: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1", calls)
	}
	if !strings.Contains(out, `"resume-me"`) {
		t.Fatalf("截断时应保留续翻 cursor:\n%s", out)
	}
	if !strings.Contains(out, `"truncated"`) || !strings.Contains(out, `"page_count"`) {
		t.Fatalf("截断应标记 truncated/page_count:\n%s", out)
	}
}

func TestRunAPI_PageAllSeedsInitialToken(t *testing.T) {
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
		_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"1"}],"has_more":true,"page_token":"abc"}}`)
	})
	defer cleanup()
	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	apiParams = `{"page_token":"abc"}`
	err := cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "重复游标") {
		t.Fatalf("初始 page_token 重复应停止, err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("应避免第二次重复请求, calls=%d", calls)
	}
}

func TestRunAPI_PageAllAmbiguousArraysError(t *testing.T) {
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
		_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"alpha":[{"id":1}],"beta":[{"id":2}],"has_more":true,"page_token":"n1"}}`)
	})
	defer cleanup()
	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	err := cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "多个未知列表字段") {
		t.Fatalf("多数组应报错而非猜测, err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("不得继续翻页, calls=%d", calls)
	}
}

func TestMergeAPIPages_MismatchDoesNotSkip(t *testing.T) {
	pages := []map[string]any{
		{"data": map[string]any{"items": []any{map[string]any{"id": "1"}}, "has_more": true}},
		{"data": map[string]any{"records": []any{map[string]any{"id": "2"}}, "has_more": false}},
	}
	if _, err := mergeAPIPages(pages, false, "items"); err == nil {
		t.Fatal("字段不一致不得静默跳过第 2 页")
	}
}

func TestRunAPI_PageAllMismatchedFieldError(t *testing.T) {
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
		if r.URL.Query().Get("page_token") == "" {
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"1"}],"has_more":true,"page_token":"n1"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"records":[{"id":"2"}],"has_more":false}}`)
	})
	defer cleanup()
	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	err := cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "列表字段") {
		t.Fatalf("两页字段不一致应非零退出, err=%v", err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d want 2", calls)
	}
}

func TestRunAPI_PageAllTerminalMissingFieldError(t *testing.T) {
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
		if r.URL.Query().Get("page_token") == "" {
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"1"}],"has_more":true,"page_token":"n1"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"has_more":false}}`)
	})
	defer cleanup()
	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	err := cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "列表字段") {
		t.Fatalf("终页缺少列表字段应非零退出, err=%v", err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d want 2", calls)
	}
}

func TestRunAPI_PageAllNoArrayHasMoreError(t *testing.T) {
	isolateAPITestEnv(t)
	defer resetAPIFlags()
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-fake","expire":7200}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"name":"x","has_more":true,"page_token":"n1"}}`)
	})
	defer cleanup()
	cmd := newTestAPICmd()
	apiAs = "bot"
	apiPageAll = true
	apiPageDelayMs = 0
	err := cmd.RunE(cmd, []string{"GET", "/open-apis/im/v1/chats"})
	if err == nil || !strings.Contains(err.Error(), "没有可识别的列表数组") {
		t.Fatalf("无数组 has_more 应报错, err=%v", err)
	}
}

func TestNewTestAPICmdHasPageFlags(t *testing.T) {
	cmd := newTestAPICmd()
	if cmd.Flags().Lookup("page-all") == nil || cmd.Flags().Lookup("page-limit") == nil {
		t.Fatal("测试命令缺少分页 flag")
	}
}

// TestParseHasMoreFlag_TolerantTypes 验证 has_more 的类型宽容解析。
// 回归防护：曾用 data["has_more"].(bool) 严格断言，端点返回 "true" 或 1 时
// 会在第 1 页静默停止翻页并清空 truncated 标记，调用方无法区分
// 「只有一页」与「被截断」。
func TestParseHasMoreFlag_TolerantTypes(t *testing.T) {
	cases := []struct {
		name   string
		input  any
		want   bool
		wantOK bool
	}{
		{"bool true", true, true, true},
		{"bool false", false, false, true},
		{"json.Number 1", json.Number("1"), true, true},
		{"json.Number 0", json.Number("0"), false, true},
		{"float64 1", float64(1), true, true},
		{"float64 0", float64(0), false, true},
		{"string true", "true", true, true},
		{"string TRUE", "TRUE", true, true},
		{"string 1", "1", true, true},
		{"string false", "false", false, true},
		{"string 0", "0", false, true},
		{"string 空", "", false, true},
		{"字段缺失", nil, false, false},
		{"无法解释的字符串", "maybe", false, false},
		{"对象", map[string]any{}, false, false},
	}
	for _, c := range cases {
		got, gotOK := parseHasMoreFlag(c.input)
		if got != c.want || gotOK != c.wantOK {
			t.Errorf("%s: parseHasMoreFlag(%#v) = (%v, %v), want (%v, %v)",
				c.name, c.input, got, gotOK, c.want, c.wantOK)
		}
	}
}

// TestPageCursorFromData_SkipsEmptyKey 验证 page_token 为空时继续查 next_page_token。
// 回归防护：曾在第一个「存在但为空」的 key 上就返回，导致同时输出
// page_token:"" 与有效 next_page_token 的端点在第 1 页中止翻页，后续页全丢。
func TestPageCursorFromData_SkipsEmptyKey(t *testing.T) {
	cases := []struct {
		name     string
		data     map[string]any
		wantTok  string
		wantKind string
	}{
		{"page_token 为空回落 next_page_token", map[string]any{"page_token": "", "next_page_token": "cur2"}, "cur2", "next_page_token"},
		{"page_token 有值优先", map[string]any{"page_token": "cur1", "next_page_token": "cur2"}, "cur1", "page_token"},
		{"仅 next_page_token", map[string]any{"next_page_token": "cur3"}, "cur3", "next_page_token"},
		{"两者皆空", map[string]any{"page_token": "", "next_page_token": ""}, "", "empty"},
		{"仅空白字符视为空", map[string]any{"page_token": "   "}, "", "empty"},
		{"都不存在", map[string]any{"items": []any{}}, "", "missing"},
		{"nil data", nil, "", "missing"},
		{"非字符串 fail-closed", map[string]any{"page_token": 123}, "", "nonstring"},
	}
	for _, c := range cases {
		gotTok, gotKind := pageCursorFromData(c.data)
		if gotTok != c.wantTok || gotKind != c.wantKind {
			t.Errorf("%s: pageCursorFromData = (%q, %q), want (%q, %q)",
				c.name, gotTok, gotKind, c.wantTok, c.wantKind)
		}
	}
}
