package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// TestDriveTaskResultLocalValidationPrecedesAuth 验证本地参数校验前置于身份解析，非法输入零网络、零 token 刷新
func TestDriveTaskResultLocalValidationPrecedesAuth(t *testing.T) {
	// 指向无效地址，确保如果有任何网络请求必然连接失败
	initDriveTaskResultTestConfig(t, "http://127.0.0.1:59999")

	// 1. 非法 scenario
	_ = driveTaskResultCmd.Flags().Set("scenario", "invalid_scenario")
	err1 := driveTaskResultCmd.RunE(driveTaskResultCmd, []string{})
	if err1 == nil {
		t.Fatal("非法 scenario 必须报错")
	}

	// 2. 缺少 task-id
	_ = driveTaskResultCmd.Flags().Set("scenario", "wiki_delete_node")
	_ = driveTaskResultCmd.Flags().Set("task-id", "")
	err2 := driveTaskResultCmd.RunE(driveTaskResultCmd, []string{})
	if err2 == nil {
		t.Fatal("缺少 task-id 必须报错")
	}

	// 3. 非法 task-id (路径穿越 ..)
	_ = driveTaskResultCmd.Flags().Set("task-id", "../task_escape")
	err3 := driveTaskResultCmd.RunE(driveTaskResultCmd, []string{})
	if err3 == nil {
		t.Fatal("非法 task-id 必须报错")
	}
}

// TestDriveTaskResultAsBotSupport 验证 drive task-result 支持 --as bot 模式走 Tenant Token 查询
func TestDriveTaskResultAsBotSupport(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-bot-token","expire":7200}`)
		case r.Method == "GET" && r.URL.Path == "/open-apis/wiki/v2/tasks/task-bot-123":
			gotAuth = r.Header.Get("Authorization")
			_, _ = fmt.Fprint(w, `{
				"code": 0, "msg": "ok",
				"data": {
					"task": {
						"task_id": "task-bot-123",
						"simple_task_result": {
							"status": "success"
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
	_ = driveTaskResultCmd.Flags().Set("task-id", "task-bot-123")
	_ = driveTaskResultCmd.Flags().Set("as", "bot")
	defer func() {
		_ = driveTaskResultCmd.Flags().Set("scenario", "")
		_ = driveTaskResultCmd.Flags().Set("task-id", "")
		_ = driveTaskResultCmd.Flags().Set("as", "auto")
	}()

	err := driveTaskResultCmd.RunE(driveTaskResultCmd, []string{})
	if err != nil {
		t.Fatalf("driveTaskResultCmd 运行失败: %v", err)
	}

	if !strings.HasPrefix(gotAuth, "Bearer t-") {
		t.Fatalf("Authorization = %q, 期望以 Tenant Token (Bearer t-...) 发起请求代表 Bot 身份，绝不使用 User Token", gotAuth)
	}
}
