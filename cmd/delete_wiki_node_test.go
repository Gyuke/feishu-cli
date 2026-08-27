package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("force", "false")
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
	_ = deleteWikiNodeCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteWikiNodeCmd.Flags().Set("space-id", "")
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
