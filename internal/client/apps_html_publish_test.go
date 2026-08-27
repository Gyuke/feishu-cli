package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const htmlPublishTestToken = "u-test-html-publish"

type htmlPublishAPICall struct {
	Method string
	Path   string
	Auth   string
	Body   string
	Host   string
}

type htmlPublishTOSCall struct {
	Method  string
	Host    string
	Auth    string
	CT      string
	Body    []byte
	Headers http.Header
}

type htmlPublishHarness struct {
	t         *testing.T
	mu        sync.Mutex
	seq       []string
	api       []htmlPublishAPICall
	tos       []htmlPublishTOSCall
	appType   string
	preStatus int
	preBody   string
	relStatus int
	relBody   string
	tosStatus int
	failApp   bool
	appCode   int
	appMsg    string
	openapi   *httptest.Server
	tosServer *httptest.Server
}

func newHTMLPublishHarness(t *testing.T) *htmlPublishHarness {
	t.Helper()
	h := &htmlPublishHarness{
		t:         t,
		appType:   "HTML",
		preStatus: http.StatusOK,
		relStatus: http.StatusOK,
		tosStatus: http.StatusOK,
		relBody:   `{"code":0,"msg":"ok","data":{"release_id":"rel_123","status":"publishing"}}`,
	}

	h.tosServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.seq = append(h.seq, "tos:"+r.Method)
		cloned := r.Header.Clone()
		h.tos = append(h.tos, htmlPublishTOSCall{
			Method:  r.Method,
			Host:    r.Host,
			Auth:    r.Header.Get("Authorization"),
			CT:      r.Header.Get("Content-Type"),
			Body:    raw,
			Headers: cloned,
		})
		h.mu.Unlock()
		w.WriteHeader(h.tosStatus)
	}))
	t.Cleanup(h.tosServer.Close)

	h.preBody = mustHTMLPublishJSON(t, map[string]any{
		"code": 0,
		"msg":  "ok",
		"data": map[string]any{
			"kvs": []any{
				map[string]any{"key": "upload_url", "value": h.tosServer.URL},
				map[string]any{"key": "tos_path", "value": "tos://bucket/key"},
			},
		},
	})

	h.openapi = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			t.Errorf("显式 User Token 时不应请求 tenant token，path=%s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.seq = append(h.seq, r.Method+" "+r.URL.Path)
		h.api = append(h.api, htmlPublishAPICall{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Body:   string(raw),
			Host:   r.Host,
		})
		h.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/open-apis/spark/v1/apps/app_x/pre_release":
			if h.preStatus != http.StatusOK {
				w.WriteHeader(h.preStatus)
			}
			_, _ = io.WriteString(w, h.preBody)
		case r.Method == http.MethodPost && r.URL.Path == "/open-apis/spark/v1/apps/app_x/releases":
			if h.relStatus != http.StatusOK {
				w.WriteHeader(h.relStatus)
			}
			_, _ = io.WriteString(w, h.relBody)
		case r.Method == http.MethodGet && r.URL.Path == "/open-apis/spark/v1/apps/app_x":
			if h.failApp {
				_, _ = io.WriteString(w, mustHTMLPublishJSON(t, map[string]any{
					"code": h.appCode,
					"msg":  h.appMsg,
				}))
				return
			}
			_, _ = io.WriteString(w, mustHTMLPublishJSON(t, map[string]any{
				"code": 0,
				"msg":  "ok",
				"data": map[string]any{
					"app": map[string]any{
						"app_id":   "app_x",
						"app_type": h.appType,
					},
				},
			}))
		default:
			t.Errorf("unexpected OpenAPI %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":1,"msg":"unexpected"}`)
		}
	}))
	t.Cleanup(h.openapi.Close)

	setupTestConfig(t, h.openapi.URL)
	return h
}

func (h *htmlPublishHarness) snapshot() (seq []string, api []htmlPublishAPICall, tos []htmlPublishTOSCall) {
	h.mu.Lock()
	defer h.mu.Unlock()
	seq = append([]string{}, h.seq...)
	api = append([]htmlPublishAPICall{}, h.api...)
	tos = append([]htmlPublishTOSCall{}, h.tos...)
	return seq, api, tos
}

func mustHTMLPublishJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSparkHTMLPublish_ThreeStageSuccess(t *testing.T) {
	h := newHTMLPublishHarness(t)
	tarball := []byte("fake-tarball-bytes")

	out, err := SparkHTMLPublish("app_x", tarball, htmlPublishTestToken)
	if err != nil {
		t.Fatalf("SparkHTMLPublish: %v", err)
	}
	if out["release_id"] != "rel_123" {
		t.Fatalf("release_id=%v, want rel_123", out["release_id"])
	}
	if _, ok := out["url"]; ok {
		t.Fatalf("不应再白名单泄漏 url，got %#v", out)
	}
	if _, ok := out["status"]; ok {
		t.Fatalf("不应泄漏 status 兄弟字段，got %#v", out)
	}

	seq, api, tos := h.snapshot()
	wantSeq := []string{
		"GET /open-apis/spark/v1/apps/app_x",
		"GET /open-apis/spark/v1/apps/app_x/pre_release",
		"tos:PUT",
		"POST /open-apis/spark/v1/apps/app_x/releases",
	}
	if strings.Join(seq, "|") != strings.Join(wantSeq, "|") {
		t.Fatalf("调用顺序 = %v, want %v", seq, wantSeq)
	}
	if len(api) != 3 {
		t.Fatalf("OpenAPI 调用数 = %d, want 3", len(api))
	}
	if len(tos) != 1 {
		t.Fatalf("TOS 调用数 = %d, want 1", len(tos))
	}

	openapiHost := strings.TrimPrefix(h.openapi.URL, "http://")
	tosHost := strings.TrimPrefix(h.tosServer.URL, "http://")
	for i, c := range api {
		if c.Host != openapiHost {
			t.Errorf("OpenAPI[%d] host = %q, want %q", i, c.Host, openapiHost)
		}
		if !strings.HasPrefix(c.Auth, "Bearer ") || !strings.Contains(c.Auth, htmlPublishTestToken) {
			t.Errorf("OpenAPI[%d] 应携带 User Token，Authorization=%q", i, c.Auth)
		}
	}
	if api[2].Method != http.MethodPost {
		t.Errorf("release-create method = %s", api[2].Method)
	}
	if !strings.Contains(api[2].Body, `"tos_path"`) || !strings.Contains(api[2].Body, "tos://bucket/key") {
		t.Errorf("release-create body 应含 tos_path，got %s", api[2].Body)
	}

	gotTOS := tos[0]
	if gotTOS.Method != http.MethodPut {
		t.Errorf("TOS method = %s, want PUT", gotTOS.Method)
	}
	if gotTOS.Host != tosHost {
		t.Errorf("TOS host = %q, want %q", gotTOS.Host, tosHost)
	}
	if gotTOS.CT != "application/gzip" {
		t.Errorf("TOS Content-Type = %q, want application/gzip", gotTOS.CT)
	}
	if string(gotTOS.Body) != string(tarball) {
		t.Errorf("TOS body = %q, want tarball bytes", gotTOS.Body)
	}
	if gotTOS.Auth != "" {
		t.Errorf("TOS 不得携带 Authorization，got %q", gotTOS.Auth)
	}
	for k, vs := range gotTOS.Headers {
		joined := strings.Join(vs, ",")
		if strings.EqualFold(k, "Authorization") || strings.Contains(joined, htmlPublishTestToken) || strings.Contains(strings.ToLower(joined), "bearer") {
			t.Errorf("TOS header %s=%q 泄漏了飞书凭证", k, joined)
		}
	}
}

func TestSparkHTMLPublish_AllowsModernHTML(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.appType = "MODERN_HTML"
	out, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err != nil {
		t.Fatalf("modern_html 应可发布: %v", err)
	}
	if out["release_id"] != "rel_123" {
		t.Fatalf("release_id=%v", out["release_id"])
	}
}

func TestSparkHTMLPublish_RejectsFullStackBeforeLaterSteps(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.appType = "FULL_STACK"

	_, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err == nil {
		t.Fatal("full_stack 应被拒绝")
	}
	if !strings.Contains(err.Error(), "full_stack") {
		t.Fatalf("应点名 app_type，got %v", err)
	}
	if !strings.Contains(err.Error(), "modern_html") {
		t.Fatalf("应给出恢复建议，got %v", err)
	}

	seq, _, tos := h.snapshot()
	if len(tos) != 0 {
		t.Fatalf("类型拒绝后不应 PUT TOS，tos=%v", tos)
	}
	for _, s := range seq {
		if strings.Contains(s, "pre_release") || strings.Contains(s, "releases") || strings.HasPrefix(s, "tos:") {
			t.Fatalf("类型拒绝后应中止，却调用了 %s（seq=%v）", s, seq)
		}
	}
	if len(seq) != 1 || seq[0] != "GET /open-apis/spark/v1/apps/app_x" {
		t.Fatalf("只应 GET app，got %v", seq)
	}
}

func TestSparkHTMLPublish_RejectsFrontend(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.appType = "FRONTEND"
	_, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err == nil || !strings.Contains(err.Error(), "frontend") {
		t.Fatalf("frontend 应被拒绝，got %v", err)
	}
	seq, _, tos := h.snapshot()
	if len(tos) != 0 {
		t.Fatalf("不应上传 TOS: %v", tos)
	}
	for _, s := range seq {
		if strings.Contains(s, "pre_release") || strings.HasPrefix(s, "tos:") {
			t.Fatalf("应中止于 app_type 校验，seq=%v", seq)
		}
	}
}

func TestSparkHTMLPublish_AppNotExistHint(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.failApp = true
	h.appCode = sparkErrCodeAppNotExist
	h.appMsg = "app not exist"

	_, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err == nil {
		t.Fatal("应用不存在应报错")
	}
	if !HasAPICode(err, sparkErrCodeAppNotExist) {
		t.Fatalf("应携带 400002577，got %v", err)
	}
	if !strings.Contains(err.Error(), "app_id") {
		t.Fatalf("应给出核对 app_id 的恢复建议，got %v", err)
	}
	seq, _, tos := h.snapshot()
	if len(tos) != 0 {
		t.Fatalf("不应上传 TOS: %v", tos)
	}
	for _, s := range seq {
		if strings.Contains(s, "pre_release") || strings.HasPrefix(s, "tos:") {
			t.Fatalf("应用不存在应中止，seq=%v", seq)
		}
	}
}

func TestSparkHTMLPublish_PreReleaseErrorStops(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.preBody = `{"code":99999,"msg":"internal server error"}`

	_, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err == nil {
		t.Fatal("pre_release 失败应报错")
	}
	if !HasAPICode(err, 99999) {
		t.Fatalf("应透出业务码，got %v", err)
	}
	seq, _, tos := h.snapshot()
	if len(tos) != 0 {
		t.Fatalf("pre_release 失败后不应 PUT TOS，tos=%v", tos)
	}
	for _, s := range seq {
		if s == "tos:PUT" || strings.Contains(s, "/releases") {
			t.Fatalf("应中止于 pre_release，seq=%v", seq)
		}
	}
}

func TestSparkHTMLPublish_MissingKVsStops(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.preBody = `{"code":0,"data":{"kvs":[]}}`

	_, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err == nil || !strings.Contains(err.Error(), "kvs") {
		t.Fatalf("空 kvs 应报错，got %v", err)
	}
	_, _, tos := h.snapshot()
	if len(tos) != 0 {
		t.Fatalf("缺 kvs 后不应 PUT TOS")
	}
}

func TestSparkHTMLPublish_TOSFailureStopsReleaseCreate(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.tosStatus = http.StatusInternalServerError

	_, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err == nil {
		t.Fatal("TOS 500 应报错")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("应含 HTTP 500，got %v", err)
	}
	if !strings.Contains(err.Error(), "pre_release") {
		t.Fatalf("5xx 应建议重试换新预签名 URL，got %v", err)
	}
	seq, _, tos := h.snapshot()
	if len(tos) != 1 {
		t.Fatalf("应 PUT 过 TOS 一次，got %d", len(tos))
	}
	for _, s := range seq {
		if strings.Contains(s, "/releases") {
			t.Fatalf("TOS 失败后不应 POST release-create，seq=%v", seq)
		}
	}
}

func TestSparkHTMLPublish_TOS4xxHint(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.tosStatus = http.StatusForbidden

	_, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("TOS 403 应报错，got %v", err)
	}
	if !strings.Contains(err.Error(), "Authorization") {
		t.Fatalf("4xx 应提示不要外送 Authorization，got %v", err)
	}
	seq, _, _ := h.snapshot()
	for _, s := range seq {
		if strings.Contains(s, "/releases") {
			t.Fatalf("TOS 4xx 后不应 POST release-create，seq=%v", seq)
		}
	}
}

func TestSparkHTMLPublish_ReleaseAppTypeErrorTranslated(t *testing.T) {
	h := newHTMLPublishHarness(t)
	h.relBody = `{"code":400000059,"msg":"app_type invalid"}`

	_, err := SparkHTMLPublish("app_x", []byte("tgz"), htmlPublishTestToken)
	if err == nil {
		t.Fatal("release-create app_type 拒绝应报错")
	}
	if !HasAPICode(err, sparkErrCodeAppTypeRejected) {
		t.Fatalf("应携带 400000059，got %v", err)
	}
	if !strings.Contains(err.Error(), "html-publish") {
		t.Fatalf("应翻译为可恢复 hint，got %v", err)
	}
}

func TestParsePreReleaseKVs(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		upload, tos, err := parsePreReleaseKVs(map[string]any{
			"kvs": []any{
				map[string]any{"key": "upload_url", "value": "https://tos.example/u"},
				map[string]any{"key": "tos_path", "value": "tos://b/k"},
				map[string]any{"key": "other", "value": "x"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if upload != "https://tos.example/u" || tos != "tos://b/k" {
			t.Fatalf("upload=%q tos=%q", upload, tos)
		}
	})
	t.Run("missing kvs", func(t *testing.T) {
		_, _, err := parsePreReleaseKVs(map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "kvs") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("missing fields", func(t *testing.T) {
		_, _, err := parsePreReleaseKVs(map[string]any{
			"kvs": []any{map[string]any{"key": "upload_url", "value": "https://x"}},
		})
		if err == nil || !strings.Contains(err.Error(), "tos_path") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestValidateTOSUploadURL(t *testing.T) {
	if err := validateTOSUploadURL("https://tos.example/obj"); err != nil {
		t.Fatalf("https 应通过: %v", err)
	}
	if err := validateTOSUploadURL("http://127.0.0.1:9/obj"); err != nil {
		t.Fatalf("http 测试 URL 应通过: %v", err)
	}
	if err := validateTOSUploadURL("file:///etc/passwd"); err == nil {
		t.Fatal("file:// 应拒绝")
	}
	if err := validateTOSUploadURL(":"); err == nil {
		t.Fatal("非法 URL 应拒绝")
	}
}

func TestSparkHTMLPublishHint(t *testing.T) {
	if sparkHTMLPublishHint(sparkErrCodeBuildFailed) == "" {
		t.Error("build-failed 应有 hint")
	}
	if sparkHTMLPublishHint(sparkErrCodeAppNotFound) == "" {
		t.Error("app-not-found 应有 hint")
	}
	if sparkHTMLPublishHint(sparkErrCodeAppNotExist) == "" {
		t.Error("app-not-exist 应有 hint")
	}
	if sparkHTMLPublishHint(sparkErrCodeAppTypeRejected) == "" {
		t.Error("app-type 应有 hint")
	}
	if sparkHTMLPublishHint(12345) != "" {
		t.Error("未知码不应有 hint")
	}
}

func TestSparkPathHelpers(t *testing.T) {
	if SparkAppGetPath("app_x") != "/open-apis/spark/v1/apps/app_x" {
		t.Fatalf("get = %s", SparkAppGetPath("app_x"))
	}
	if SparkPreReleasePath("app_x") != "/open-apis/spark/v1/apps/app_x/pre_release" {
		t.Fatalf("pre = %s", SparkPreReleasePath("app_x"))
	}
	if SparkReleaseCreatePath("app_x") != "/open-apis/spark/v1/apps/app_x/releases" {
		t.Fatalf("rel = %s", SparkReleaseCreatePath("app_x"))
	}
}
