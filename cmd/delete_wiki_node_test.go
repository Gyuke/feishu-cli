package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/viper"
)

func initWikiNodeDeleteTestConfig(t *testing.T, baseURL string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("app_id: cli_test\napp_secret: test_secret\nbase_url: %q\n", baseURL)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试配置失败: %v", err)
	}
	if err := config.Init(configPath); err != nil {
		t.Fatalf("初始化测试配置失败: %v", err)
	}
}

// TestDeleteWikiNodePathBodyAndAsyncPoll 验证 Wiki 节点删除走正确的 OpenAPI 路径、Body 以及异步任务轮询
func TestDeleteWikiNodePathBodyAndAsyncPoll(t *testing.T) {
	deleteCalled := false
	var gotPath string
	var gotBody struct {
		ObjType         string `json:"obj_type"`
		IncludeChildren bool   `json:"include_children"`
	}
	pollCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.URL.Path == "/open-apis/wiki/v2/spaces/get_node":
			// 自动解析 space_id
			_, _ = fmt.Fprint(w, `{
				"code": 0,
				"msg": "ok",
				"data": {
					"node": {
						"space_id": "space-999",
						"node_token": "wikcnTestNode",
						"title": "测试节点",
						"obj_type": "docx"
					}
				}
			}`)
		case r.Method == "DELETE" && r.URL.Path == "/open-apis/wiki/v2/spaces/space-999/nodes/wikcnTestNode":
			deleteCalled = true
			gotPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			// 返回异步 task_id
			_, _ = fmt.Fprint(w, `{
				"code": 0,
				"msg": "ok",
				"data": {
					"task_id": "task-async-777"
				}
			}`)
		case r.Method == "GET" && r.URL.Path == "/open-apis/wiki/v2/tasks/task-async-777":
			pollCount++
			taskType := r.URL.Query().Get("task_type")
			if taskType != "delete_node" {
				http.Error(w, "task_type 必须为 delete_node", http.StatusBadRequest)
				return
			}
			if pollCount == 1 {
				// 第一次轮询中
				_, _ = fmt.Fprint(w, `{
					"code": 0,
					"msg": "ok",
					"data": {
						"task": {
							"task_id": "task-async-777",
							"simple_task_result": {
								"status": "processing"
							}
						}
					}
				}`)
			} else {
				// 第二次成功
				_, _ = fmt.Fprint(w, `{
					"code": 0,
					"msg": "ok",
					"data": {
						"task": {
							"task_id": "task-async-777",
							"simple_task_result": {
								"status": "success"
							}
						}
					}
				}`)
			}
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initWikiNodeDeleteTestConfig(t, server.URL)

	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	origAttempts := wikiDeleteNodePollAttempts
	origInterval := wikiDeleteNodePollInterval
	wikiDeleteNodePollAttempts = 3
	wikiDeleteNodePollInterval = 5 * time.Millisecond
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		wikiDeleteNodePollAttempts = origAttempts
		wikiDeleteNodePollInterval = origInterval
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"wikcnTestNode"})
	if err != nil {
		t.Fatalf("deleteWikiNodeCmd 运行失败: %v", err)
	}

	if !deleteCalled {
		t.Fatal("未调用 DELETE /wiki/v2/spaces/{space}/nodes/{node}")
	}
	wantPath := "/open-apis/wiki/v2/spaces/space-999/nodes/wikcnTestNode"
	if gotPath != wantPath {
		t.Fatalf("请求路径 = %q, 期望 %q", gotPath, wantPath)
	}
	if gotBody.ObjType != "wiki" {
		t.Fatalf("请求 Body 中的 obj_type = %q, 期望 wiki", gotBody.ObjType)
	}
	if !gotBody.IncludeChildren {
		t.Fatalf("请求 Body 中的 include_children = false, 期望 true")
	}
	if pollCount < 2 {
		t.Fatalf("异步任务轮询次数 = %d, 期望 >= 2", pollCount)
	}
}

// TestDeleteWikiNodeSyncCompletion 验证同步完成（无 task_id）时不进行任务轮询
func TestDeleteWikiNodeSyncCompletion(t *testing.T) {
	deleteCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.Method == "DELETE" && r.URL.Path == "/open-apis/wiki/v2/spaces/sp-1/nodes/wikcnSync":
			deleteCalled = true
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"task_id":""}}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initWikiNodeDeleteTestConfig(t, server.URL)

	_ = deleteWikiNodeCmd.Flags().Set("space-id", "sp-1")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"wikcnSync"})
	if err != nil {
		t.Fatalf("deleteWikiNodeCmd 运行失败: %v", err)
	}
	if !deleteCalled {
		t.Fatal("未发起同步删除请求")
	}
}

// TestDeleteWikiNodeAllFailedReturnsError 验证所有状态轮询均失败时返回非零退出，并保留 task_id 及 resume 提示
func TestDeleteWikiNodeAllFailedReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.Method == "DELETE" && r.URL.Path == "/open-apis/wiki/v2/spaces/sp-fail/nodes/node-fail":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"task_id":"task-failed-999"}}`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/tasks/"):
			// 模拟所有轮询均失败
			http.Error(w, "internal server error", http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initWikiNodeDeleteTestConfig(t, server.URL)

	origAttempts := wikiDeleteNodePollAttempts
	origInterval := wikiDeleteNodePollInterval
	wikiDeleteNodePollAttempts = 2
	wikiDeleteNodePollInterval = 1 * time.Millisecond
	defer func() {
		wikiDeleteNodePollAttempts = origAttempts
		wikiDeleteNodePollInterval = origInterval
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	_ = deleteWikiNodeCmd.Flags().Set("space-id", "sp-fail")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"node-fail"})
	if err == nil {
		t.Fatal("状态查询全部失败时必须返回非零错误，绝不能返回 nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "task-failed-999") {
		t.Fatalf("错误信息必须保留 task_id (task-failed-999)，实际得到: %s", errMsg)
	}
	if !strings.Contains(errMsg, "feishu-cli") {
		t.Fatalf("错误信息必须包含 resume 查询提示命令，实际得到: %s", errMsg)
	}
}

// TestDeleteWikiNodeTimeoutReturnsError 验证轮询超时（仍为 processing）时返回非零退出，并保留 task_id 与 resume 提示
func TestDeleteWikiNodeTimeoutReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.Method == "DELETE" && r.URL.Path == "/open-apis/wiki/v2/spaces/sp-timeout/nodes/node-timeout":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"task_id":"task-timeout-888"}}`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/tasks/"):
			// 模拟一直为 processing
			_, _ = fmt.Fprint(w, `{
				"code": 0,
				"msg": "ok",
				"data": {
					"task": {
						"task_id": "task-timeout-888",
						"simple_task_result": {
							"status": "processing"
						}
					}
				}
			}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initWikiNodeDeleteTestConfig(t, server.URL)

	origAttempts := wikiDeleteNodePollAttempts
	origInterval := wikiDeleteNodePollInterval
	wikiDeleteNodePollAttempts = 2
	wikiDeleteNodePollInterval = 1 * time.Millisecond
	defer func() {
		wikiDeleteNodePollAttempts = origAttempts
		wikiDeleteNodePollInterval = origInterval
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	_ = deleteWikiNodeCmd.Flags().Set("space-id", "sp-timeout")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"node-timeout"})
	if err == nil {
		t.Fatal("轮询超时仍未完成时必须返回非零错误，绝不能返回 nil 谎报成功")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "task-timeout-888") {
		t.Fatalf("错误信息必须保留 task_id (task-timeout-888)，实际得到: %s", errMsg)
	}
	if !strings.Contains(errMsg, "超时") && !strings.Contains(errMsg, "执行中") {
		t.Fatalf("错误信息必须说明超时或执行中状态，实际得到: %s", errMsg)
	}
}

// TestDeleteWikiNodeInvalidObjType 验证非法 obj-type 立即被拦截拒绝
func TestDeleteWikiNodeInvalidObjType(t *testing.T) {
	initWikiNodeDeleteTestConfig(t, "http://127.0.0.1:9999")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "not_exist_type")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"wikcnDummy"})
	if err == nil {
		t.Fatal("非法 obj-type 应当被拒绝，但返回了 nil")
	}
	if !strings.Contains(err.Error(), "不支持的 --obj-type") {
		t.Fatalf("错误信息应指出不支持的 obj-type，得到: %v", err)
	}
}

// TestDeleteWikiNodePathEscaped 验证 space_id、node_token 和 task_id 正确进行 PathEscape 转义
func TestDeleteWikiNodePathEscaped(t *testing.T) {
	var gotDeletePath, gotPollPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.Method == "DELETE":
			gotDeletePath = r.URL.EscapedPath()
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"task_id":"task id with space"}}`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/tasks/"):
			gotPollPath = r.URL.EscapedPath()
			_, _ = fmt.Fprint(w, `{
				"code":0,"msg":"ok",
				"data":{"task":{"task_id":"task id with space","simple_task_result":{"status":"success"}}}
			}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initWikiNodeDeleteTestConfig(t, server.URL)

	origAttempts := wikiDeleteNodePollAttempts
	origInterval := wikiDeleteNodePollInterval
	wikiDeleteNodePollAttempts = 2
	wikiDeleteNodePollInterval = 1 * time.Millisecond
	defer func() {
		wikiDeleteNodePollAttempts = origAttempts
		wikiDeleteNodePollInterval = origInterval
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	_ = deleteWikiNodeCmd.Flags().Set("space-id", "sp 123")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"wikcnEscaped"})
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	wantDeletePath := "/open-apis/wiki/v2/spaces/sp%20123/nodes/wikcnEscaped"
	if gotDeletePath != wantDeletePath {
		t.Fatalf("DELETE 路径转义异常: got %q, want %q", gotDeletePath, wantDeletePath)
	}
	wantPollPath := "/open-apis/wiki/v2/tasks/task%20id%20with%20space"
	if gotPollPath != wantPollPath {
		t.Fatalf("Task 轮询路径转义异常: got %q, want %q", gotPollPath, wantPollPath)
	}
}

// TestDeleteWikiNodeBareTokenRequiresObjType 验证裸 token 输入缺 --obj-type 时报错
func TestDeleteWikiNodeBareTokenRequiresObjType(t *testing.T) {
	initWikiNodeDeleteTestConfig(t, "http://127.0.0.1:9999")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"wikcnBareToken"})
	if err == nil {
		t.Fatal("裸 token 未指定 --obj-type 必须报错")
	}
	if !strings.Contains(err.Error(), "--obj-type 为必填项") {
		t.Fatalf("错误应提示 --obj-type 为必填项，实际得到: %v", err)
	}
}

// TestDeleteWikiNodeURLInfersObjTypeAndPassesObjType 验证完整 URL 自动推断 obj_type，且对 non-wiki token 向 get_node 发送 obj_type 参数
func TestDeleteWikiNodeURLInfersObjTypeAndPassesObjType(t *testing.T) {
	var gotGetNodeQuery string
	var gotDeleteBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.URL.Path == "/open-apis/wiki/v2/spaces/get_node":
			gotGetNodeQuery = r.URL.RawQuery
			_, _ = fmt.Fprint(w, `{
				"code": 0, "msg": "ok",
				"data": {"node": {"space_id": "sp-inferred", "node_token": "doxcnReal", "obj_type": "docx"}}
			}`)
		case r.Method == "DELETE" && r.URL.Path == "/open-apis/wiki/v2/spaces/sp-inferred/nodes/doxcnReal":
			_ = json.NewDecoder(r.Body).Decode(&gotDeleteBody)
			_, _ = fmt.Fprint(w, `{"code": 0, "msg": "ok", "data": {"task_id": ""}}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initWikiNodeDeleteTestConfig(t, server.URL)

	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"https://sample.feishu.cn/docx/doxcnReal"})
	if err != nil {
		t.Fatalf("URL 执行删除失败: %v", err)
	}

	// 验证 non-wiki docx token 向 get_node 传递了 obj_type=docx
	if !strings.Contains(gotGetNodeQuery, "obj_type=docx") {
		t.Fatalf("non-wiki docx URL 解析 space 时向 get_node 必须发送 obj_type=docx，实际 query: %q", gotGetNodeQuery)
	}
	if gotDeleteBody["obj_type"] != "docx" {
		t.Fatalf("DELETE 请求 body obj_type = %v, 期望 docx", gotDeleteBody["obj_type"])
	}
}
