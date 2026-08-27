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

	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/viper"
)

func initDeleteBlocksTestConfig(t *testing.T, baseURL string) {
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

// TestDeleteAllBlocksPaginatesAndDeletesTrueTotal 验证 doc delete --all 遍历完整分页并删除全部子块，不谎报
func TestDeleteAllBlocksPaginatesAndDeletesTrueTotal(t *testing.T) {
	getChildrenCalls := 0
	deleteCalled := false
	gotStartIndex := -1
	gotEndIndex := -1

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case strings.HasSuffix(r.URL.Path, "/children") && r.Method == "GET":
			getChildrenCalls++
			pageToken := r.URL.Query().Get("page_token")
			if pageToken == "" {
				// 第一页：返回 2 个块，并且 has_more=true
				_, _ = fmt.Fprint(w, `{
					"code": 0,
					"msg": "ok",
					"data": {
						"items": [{"block_id":"b1"},{"block_id":"b2"}],
						"has_more": true,
						"page_token": "page-2"
					}
				}`)
			} else if pageToken == "page-2" {
				// 第二页：返回 1 个块，has_more=false
				_, _ = fmt.Fprint(w, `{
					"code": 0,
					"msg": "ok",
					"data": {
						"items": [{"block_id":"b3"}],
						"has_more": false
					}
				}`)
			} else {
				http.Error(w, "invalid page token: "+pageToken, http.StatusBadRequest)
			}
		case strings.Contains(r.URL.Path, "batch_delete") && r.Method == "DELETE":
			deleteCalled = true
			var body struct {
				StartIndex int `json:"start_index"`
				EndIndex   int `json:"end_index"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotStartIndex = body.StartIndex
			gotEndIndex = body.EndIndex
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok"}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initDeleteBlocksTestConfig(t, server.URL)

	_ = deleteBlocksCmd.Flags().Set("all", "true")
	_ = deleteBlocksCmd.Flags().Set("force", "true")
	defer func() {
		_ = deleteBlocksCmd.Flags().Set("all", "false")
		_ = deleteBlocksCmd.Flags().Set("force", "false")
	}()
	err := deleteBlocksCmd.RunE(deleteBlocksCmd, []string{"doc-123", "parent-456"})
	if err != nil {
		t.Fatalf("deleteBlocksCmd 执行失败: %v", err)
	}

	if getChildrenCalls != 2 {
		t.Fatalf("预期分页获取 2 次，实际调用 %d 次（说明未正确分页拉取全量子块）", getChildrenCalls)
	}
	if !deleteCalled {
		t.Fatal("未发起 batch_delete 删除调用")
	}
	if gotStartIndex != 0 || gotEndIndex != 3 {
		t.Fatalf("删除范围预期 [0, 3)（包含跨页的全部 3 个子块），实际得到 [%d, %d)", gotStartIndex, gotEndIndex)
	}
}
