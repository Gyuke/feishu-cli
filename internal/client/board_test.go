package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateBoardNodes_OverwriteSingleRequest 验证 overwrite 模式仅发单个 POST /nodes 请求且带 overwrite: true，
// 严禁先建后删（不发起任何 DELETE 请求）。
func TestCreateBoardNodes_OverwriteSingleRequest(t *testing.T) {
	type reqRecord struct {
		method string
		path   string
		query  string
		body   string
	}
	var requests []reqRecord

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		requests = append(requests, reqRecord{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			body:   string(bodyBytes),
		})

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"ids":["node_1","node_2"]}}`)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)

	nodesJSON := `[{"type":"sticky_note","x":100,"y":100,"content":"hello"}]`
	ids, err := CreateBoardNodes("wb_test_123", nodesJSON, CreateBoardNotesOptions{
		UserAccessToken: "u-test",
		Overwrite:       true,
	})
	if err != nil {
		t.Fatalf("CreateBoardNodes unexpected error: %v", err)
	}
	if len(ids) != 2 || ids[0] != "node_1" {
		t.Fatalf("ids mismatch: got %v", ids)
	}

	// 验证请求数量必须为 1（原子操作）
	if len(requests) != 1 {
		t.Fatalf("期望仅发出 1 个请求，实际发出 %d 个", len(requests))
	}

	req := requests[0]
	if req.method != http.MethodPost {
		t.Errorf("请求方法应为 POST，实际为 %s", req.method)
	}
	if req.path != "/open-apis/board/v1/whiteboards/wb_test_123/nodes" {
		t.Errorf("请求路径不符: %s", req.path)
	}

	var parsedBody struct {
		Overwrite bool              `json:"overwrite"`
		Nodes     []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(req.body), &parsedBody); err != nil {
		t.Fatalf("解析请求体 JSON 失败: %v, body: %s", err, req.body)
	}

	if !parsedBody.Overwrite {
		t.Errorf("请求体 overwrite 应为 true，实际为 false: %s", req.body)
	}
	if len(parsedBody.Nodes) != 1 {
		t.Errorf("请求体 nodes 数量应为 1，实际为 %d", len(parsedBody.Nodes))
	}
}

// TestCreateBoardNodes_FailClosedNoResidual 验证创建失败时立即返回错误且不发起后续操作。
func TestCreateBoardNodes_FailClosedNoResidual(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":1254030,"msg":"permission denied"}`)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)

	nodesJSON := `[{"type":"sticky_note","x":100,"y":100}]`
	_, err := CreateBoardNodes("wb_test_123", nodesJSON, CreateBoardNotesOptions{
		UserAccessToken: "u-test",
		Overwrite:       true,
	})
	if err == nil {
		t.Fatal("期望业务错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "1254030") || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("错误信息不含业务 code/msg: %v", err)
	}
	if len(paths) != 1 {
		t.Errorf("失败后不应发起额外请求，实际请求数: %d, 路径: %v", len(paths), paths)
	}
}

// TestCreateBoardNodes_ClientTokenValidation 验证 client_token 最小长度与格式校验。
func TestCreateBoardNodes_ClientTokenValidation(t *testing.T) {
	// 短 token (<10 字符) 本地拦截
	_, err := CreateBoardNodes("wb_test_123", `[{"type":"sticky_note"}]`, CreateBoardNotesOptions{
		UserAccessToken: "u-test",
		ClientToken:     "abc123", // 6 字符
	})
	if err == nil {
		t.Fatal("短 client_token (<10) 应报错")
	}
	if !strings.Contains(err.Error(), "至少为 10 个字符") {
		t.Errorf("错误信息应提示至少 10 个字符，得到: %v", err)
	}

	// 合法 token (≥10 字符)
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"ids":["node_1"]}}`)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)

	validToken := "6d99f59c-4d7d-4452-98d6-3d0556393cf6"
	_, err = CreateBoardNodes("wb_test_123", `[{"type":"sticky_note"}]`, CreateBoardNotesOptions{
		UserAccessToken: "u-test",
		ClientToken:     validToken,
	})
	if err != nil {
		t.Fatalf("合法 client_token 意外报错: %v", err)
	}
	if !strings.Contains(gotQuery, "client_token="+validToken) {
		t.Errorf("URL Query 缺少正确的 client_token: %s", gotQuery)
	}
}

// TestGetBoardNodes_FailClosedOnBusinessCode 验证 GetBoardNodes 在 HTTP 200 但业务 code != 0 时 fail closed。
func TestGetBoardNodes_FailClosedOnBusinessCode(t *testing.T) {
	// 业务错误场景
	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":99991663,"msg":"whiteboard not found"}`)
	}))
	t.Cleanup(srvErr.Close)
	setupTestConfig(t, srvErr.URL)

	_, err := GetBoardNodes("wb_test_123", "u-test")
	if err == nil {
		t.Fatal("业务 code != 0 时 GetBoardNodes 应报错 fail closed")
	}
	if !strings.Contains(err.Error(), "99991663") || !strings.Contains(err.Error(), "whiteboard not found") {
		t.Errorf("错误信息不符合预期: %v", err)
	}

	// 正常场景
	srvOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"nodes":{"n1":{"id":"n1","type":"sticky_note"}}}}`)
	}))
	t.Cleanup(srvOK.Close)
	setupTestConfig(t, srvOK.URL)

	raw, err := GetBoardNodes("wb_test_123", "u-test")
	if err != nil {
		t.Fatalf("正常响应 GetBoardNodes 意外报错: %v", err)
	}
	if !strings.Contains(string(raw), `"sticky_note"`) {
		t.Errorf("未返回预期节点数据: %s", string(raw))
	}
}

// TestGetBoardImage_FailClosedOnBusinessCode 验证 GetBoardImage 在 HTTP 200 返回 JSON 业务错误时 fail closed。
func TestGetBoardImage_FailClosedOnBusinessCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":1254030,"msg":"no permission"}`)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)

	tmpDir := t.TempDir()
	_, err := GetBoardImage("wb_test_123", tmpDir, "u-test")
	if err == nil {
		t.Fatal("返回业务错误 JSON 时 GetBoardImage 应报错")
	}
	if !strings.Contains(err.Error(), "1254030") || !strings.Contains(err.Error(), "no permission") {
		t.Errorf("错误应包含业务 code/msg，实际: %v", err)
	}
}

// TestImportDiagram_ParseModeAndOverwrite 验证 ImportDiagram 传递 parse_mode 与 overwrite。
func TestImportDiagram_ParseModeAndOverwrite(t *testing.T) {
	var gotBody string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		bodyBytes, _ := io.ReadAll(r.Body)
		gotBody = string(bodyBytes)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"node_id":"plantuml_node_1"}}`)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)

	diagramCode := "graph TD\nA-->B"
	res, _, err := ImportDiagram("wb_test_123", diagramCode, ImportDiagramOptions{
		SourceType:      "content",
		Syntax:          "mermaid",
		DiagramType:     "flowchart",
		Style:           "board",
		ParseMode:       1,
		Overwrite:       true,
		UserAccessToken: "u-test",
	})
	if err != nil {
		t.Fatalf("ImportDiagram 意外报错: %v", err)
	}
	if res.TicketID != "plantuml_node_1" {
		t.Errorf("TicketID 不符: %s", res.TicketID)
	}
	if gotPath != "/open-apis/board/v1/whiteboards/wb_test_123/nodes/plantuml" {
		t.Errorf("路径不符: %s", gotPath)
	}

	var reqData struct {
		PlantUMLCode string `json:"plant_uml_code"`
		SyntaxType   int    `json:"syntax_type"`
		StyleType    int    `json:"style_type"`
		DiagramType  int    `json:"diagram_type"`
		ParseMode    int    `json:"parse_mode"`
		Overwrite    bool   `json:"overwrite"`
	}
	if err := json.Unmarshal([]byte(gotBody), &reqData); err != nil {
		t.Fatalf("解析请求体失败: %v, body: %s", err, gotBody)
	}

	if reqData.ParseMode != 1 {
		t.Errorf("parse_mode 应为 1，得到 %d", reqData.ParseMode)
	}
	if !reqData.Overwrite {
		t.Errorf("overwrite 应为 true")
	}
	if reqData.SyntaxType != 2 {
		t.Errorf("syntax_type 应为 2 (mermaid)，得到 %d", reqData.SyntaxType)
	}
}
