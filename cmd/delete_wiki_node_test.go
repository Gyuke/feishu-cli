package cmd

import (
	"context"
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

// TestDeleteWikiNodeAllFailedReturnsError 验证所有状态轮询均失败时返回非零退出，并保留 task_id 及 resume 提示，且绝不泄露 token 字节
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
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
		_ = deleteWikiNodeCmd.Flags().Set("user-access-token", "")
		_ = deleteWikiNodeCmd.Flags().Set("as", "auto")
	}()

	secretToken := "u-secret-token-super-private-9999"
	_ = deleteWikiNodeCmd.Flags().Set("space-id", "sp-fail")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	_ = deleteWikiNodeCmd.Flags().Set("user-access-token", secretToken)
	_ = deleteWikiNodeCmd.Flags().Set("as", "user")

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"node-fail"})
	if err == nil {
		t.Fatal("状态查询全部失败时必须返回非零错误，绝不能返回 nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "task-failed-999") {
		t.Fatalf("错误信息必须保留 task_id (task-failed-999)，实际得到: %s", errMsg)
	}
	if !strings.Contains(errMsg, "feishu-cli drive task-result --scenario wiki_delete_node") {
		t.Fatalf("错误信息必须包含有效的 resume 查询提示命令，实际得到: %s", errMsg)
	}
	if !strings.Contains(errMsg, "--as user") {
		t.Fatalf("resume 命令必须携带身份标志 --as user，实际得到: %s", errMsg)
	}
	// 严防凭证泄漏：断言 token 字节绝不在错误输出中
	if strings.Contains(errMsg, secretToken) {
		t.Fatalf("严重违规：错误信息中泄漏了明文 token 字节！%s", errMsg)
	}
}

// TestDeleteWikiNodeTimeoutReturnsError 验证轮询超时（仍为 processing）时返回非零退出，保留 task_id 与 resume 提示且不泄露 token
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
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
		_ = deleteWikiNodeCmd.Flags().Set("user-access-token", "")
		_ = deleteWikiNodeCmd.Flags().Set("as", "auto")
	}()

	secretToken := "u-secret-token-timeout-check-8888"
	_ = deleteWikiNodeCmd.Flags().Set("space-id", "sp-timeout")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	_ = deleteWikiNodeCmd.Flags().Set("user-access-token", secretToken)
	_ = deleteWikiNodeCmd.Flags().Set("as", "user")

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"node-timeout"})
	if err == nil {
		t.Fatal("轮询超时仍未完成时必须返回非零错误，绝不能返回 nil 谎报成功")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "task-timeout-888") {
		t.Fatalf("错误信息必须保留 task_id (task-timeout-888)，实际得到: %s", errMsg)
	}
	if !strings.Contains(errMsg, "feishu-cli drive task-result --scenario wiki_delete_node") {
		t.Fatalf("错误信息必须包含有效 resume 命令，实际得到: %s", errMsg)
	}
	if strings.Contains(errMsg, secretToken) {
		t.Fatalf("严重违规：错误信息中泄漏了明文 token 字节！%s", errMsg)
	}
}

// TestDeleteWikiNodeCancelReturnsError 验证上下文取消时返回非零退出，保留 task_id 与 resume 提示且不泄露 token
func TestDeleteWikiNodeCancelReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.Method == "DELETE" && r.URL.Path == "/open-apis/wiki/v2/spaces/sp-cancel/nodes/node-cancel":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"task_id":"task-cancel-777"}}`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/tasks/"):
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"task":{"task_id":"task-cancel-777","simple_task_result":{"status":"processing"}}}}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initWikiNodeDeleteTestConfig(t, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	// 立即取消
	cancel()

	secretToken := "u-secret-token-cancel-check-7777"
	status, err := pollDeleteWikiNodeTask(ctx, "task-cancel-777", secretToken, "user")
	if err == nil {
		t.Fatal("取消时必须返回非零错误")
	}
	if status == nil {
		t.Fatal("status 不应为 nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "task-cancel-777") {
		t.Fatalf("错误信息必须保留 task_id (task-cancel-777)，得到: %s", errMsg)
	}
	if !strings.Contains(errMsg, "feishu-cli drive task-result --scenario wiki_delete_node") {
		t.Fatalf("错误信息必须保留有效 resume 命令，得到: %s", errMsg)
	}
	if strings.Contains(errMsg, secretToken) {
		t.Fatalf("严重违规：错误信息中泄漏了明文 token 字节！%s", errMsg)
	}
}

// TestDeleteWikiNodeURLValidation 验证严格的 URL 解析安全规则
func TestDeleteWikiNodeURLValidation(t *testing.T) {
	// 1. 拒绝非法第三方域名（防绕过）
	_, _, err1 := parseWikiDeleteInput("https://attacker.com/evil?redirect=/wiki/wikcnTarget", "wiki")
	if err1 == nil || !strings.Contains(err1.Error(), "不支持的域名") {
		t.Fatalf("非飞书域名应报错拒绝，实际得到: %v", err1)
	}

	// 2. 拒绝 userinfo 凭证嵌入
	_, _, err2 := parseWikiDeleteInput("https://user:pass@sample.feishu.cn/wiki/wikcnTarget", "wiki")
	if err2 == nil || !strings.Contains(err2.Error(), "用户信息") {
		t.Fatalf("包含 userinfo 的 URL 应被拒绝，实际得到: %v", err2)
	}

	// 3. 拒绝不支持的路径前缀
	_, _, err3 := parseWikiDeleteInput("https://sample.feishu.cn/evil_path/wikcnTarget", "wiki")
	if err3 == nil || !strings.Contains(err3.Error(), "不支持的 URL 路径") {
		t.Fatalf("不支持的路径前缀应被拒绝，实际得到: %v", err3)
	}

	// 4. 正确的 URL 路径安全提取与推断
	tok, objType, err4 := parseWikiDeleteInput("https://sample.feishu.cn/wiki/wikcnValidNode?extra=1#frag", "")
	if err4 != nil {
		t.Fatalf("合法飞书 wiki URL 应解析成功，但得到: %v", err4)
	}
	if tok != "wikcnValidNode" || objType != "wiki" {
		t.Fatalf("解析结果异常: tok=%q objType=%q", tok, objType)
	}
}

// TestDeleteWikiNodeSpaceIDValidation 验证显式传入非法 space-id 被拦截
func TestDeleteWikiNodeSpaceIDValidation(t *testing.T) {
	initWikiNodeDeleteTestConfig(t, "http://127.0.0.1:9999")
	_ = deleteWikiNodeCmd.Flags().Set("space-id", "sp/with/slash")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"wikcnDummy"})
	if err == nil || !strings.Contains(err.Error(), "--space-id") {
		t.Fatalf("非法 space-id 应被校验拒绝，实际得到: %v", err)
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
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"task_id":"task-escaped-123"}}`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/tasks/"):
			gotPollPath = r.URL.EscapedPath()
			_, _ = fmt.Fprint(w, `{
				"code":0,"msg":"ok",
				"data":{"task":{"task_id":"task-escaped-123","simple_task_result":{"status":"success"}}}
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
		_ = deleteWikiNodeCmd.Flags().Set("obj-type", "")
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
	}()

	_ = deleteWikiNodeCmd.Flags().Set("space-id", "sp-123")
	_ = deleteWikiNodeCmd.Flags().Set("obj-type", "wiki")
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")

	err := deleteWikiNodeCmd.RunE(deleteWikiNodeCmd, []string{"wikcnEscaped"})
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	wantDeletePath := "/open-apis/wiki/v2/spaces/sp-123/nodes/wikcnEscaped"
	if gotDeletePath != wantDeletePath {
		t.Fatalf("DELETE 路径转义异常: got %q, want %q", gotDeletePath, wantDeletePath)
	}
	wantPollPath := "/open-apis/wiki/v2/tasks/task-escaped-123"
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

// TestBuildWikiDeleteNodeResumeCmdPOSIXShellSafe 验证 taskID 含 $(), backtick, single quote, whitespace 时不可触发展开且完全安全
func TestBuildWikiDeleteNodeResumeCmdPOSIXShellSafe(t *testing.T) {
	testCases := []struct {
		taskID   string
		identity string
		want     string
	}{
		{
			taskID:   "$(touch /tmp/pwn)",
			identity: "user",
			want:     "feishu-cli drive task-result --scenario wiki_delete_node --task-id '$(touch /tmp/pwn)' --as user",
		},
		{
			taskID:   "task`rm -rf /`",
			identity: "bot",
			want:     "feishu-cli drive task-result --scenario wiki_delete_node --task-id 'task`rm -rf /`' --as bot",
		},
		{
			taskID:   "task'with'quote",
			identity: "user",
			want:     "feishu-cli drive task-result --scenario wiki_delete_node --task-id 'task'\\''with'\\''quote' --as user",
		},
		{
			taskID:   "$USER $HOME task",
			identity: "bot",
			want:     "feishu-cli drive task-result --scenario wiki_delete_node --task-id '$USER $HOME task' --as bot",
		},
	}

	for _, tc := range testCases {
		got := buildWikiDeleteNodeResumeCmd(tc.taskID, tc.identity)
		if got != tc.want {
			t.Errorf("buildWikiDeleteNodeResumeCmd(%q, %q) = %q, want %q", tc.taskID, tc.identity, got, tc.want)
		}
		// 严密断言：命令中不得使用双引号包裹 task_id
		if strings.Contains(got, `"`+tc.taskID+`"`) {
			t.Errorf("resume 命令禁止使用双引号包裹 task_id: %s", got)
		}
	}
}
