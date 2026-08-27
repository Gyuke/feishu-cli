package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/viper"
)

func initDriveTaskResultTestConfig(t *testing.T, baseURL string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("app_id: cli_test\napp_secret: test_secret\nbase_url: %q\nuser_access_token: u-test-user\n", baseURL)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试配置失败: %v", err)
	}
	if err := config.Init(configPath); err != nil {
		t.Fatalf("初始化测试配置失败: %v", err)
	}
}

// TestDriveTaskResultWikiDeleteNodeScenario 验证 drive task-result 支持 --scenario wiki_delete_node 并正确轮询与上报状态
func TestDriveTaskResultWikiDeleteNodeScenario(t *testing.T) {
	pollCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/open-apis/wiki/v2/tasks/task-wiki-del-123":
			pollCalled = true
			if r.URL.Query().Get("task_type") != "delete_node" {
				http.Error(w, "task_type must be delete_node", http.StatusBadRequest)
				return
			}
			_, _ = fmt.Fprint(w, `{
				"code": 0, "msg": "ok",
				"data": {
					"task": {
						"task_id": "task-wiki-del-123",
						"simple_task_result": {
							"status": "success",
							"status_msg": "deleted"
						}
					}
				}
			}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initDriveTaskResultTestConfig(t, server.URL)

	_ = driveTaskResultCmd.Flags().Set("scenario", "wiki_delete_node")
	_ = driveTaskResultCmd.Flags().Set("task-id", "task-wiki-del-123")
	_ = driveTaskResultCmd.Flags().Set("user-access-token", "u-test-user")
	defer func() {
		_ = driveTaskResultCmd.Flags().Set("scenario", "")
		_ = driveTaskResultCmd.Flags().Set("task-id", "")
		_ = driveTaskResultCmd.Flags().Set("user-access-token", "")
	}()

	err := driveTaskResultCmd.RunE(driveTaskResultCmd, []string{})
	if err != nil {
		t.Fatalf("driveTaskResultCmd 运行失败: %v", err)
	}

	if !pollCalled {
		t.Fatal("未发起 wiki delete_node task 查询")
	}
}
