package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/riba2534/feishu-cli/internal/config"
)

// setupCmdTestConfig 辅助函数：初始化测试配置并将 base_url 指向 mock 服务器
func setupCmdTestConfig(t *testing.T, baseURL string) {
	t.Helper()
	os.Unsetenv("FEISHU_APP_ID")
	os.Unsetenv("FEISHU_APP_SECRET")
	os.Unsetenv("FEISHU_USER_ACCESS_TOKEN")
	os.Unsetenv("FEISHU_PROFILE")
	tmpDir := t.TempDir()
	configFile := tmpDir + "/config.yaml"
	content := fmt.Sprintf("app_id: \"cli_test_app\"\napp_secret: \"test_secret\"\nbase_url: \"%s\"\n", baseURL)
	if err := os.WriteFile(configFile, []byte(content), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	if err := config.Init(configFile); err != nil {
		t.Fatalf("初始化配置失败: %v", err)
	}
}

// TestSheetProtectCmd_HiddenAndUnsupported 验证 sheet protect/unprotect 处于隐藏状态且 fail-closed
func TestSheetProtectCmd_HiddenAndUnsupported(t *testing.T) {
	if !sheetProtectCmd.Hidden {
		t.Error("sheetProtectCmd should be hidden")
	}
	if !sheetUnprotectCmd.Hidden {
		t.Error("sheetUnprotectCmd should be hidden")
	}

	err := sheetProtectCmd.RunE(sheetProtectCmd, []string{"sht_token", "sheet_1"})
	if err == nil {
		t.Fatal("sheetProtectCmd should fail with error (fail-closed)")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("sheetProtectCmd error = %v, want mentioning unsupported", err)
	}

	err = sheetUnprotectCmd.RunE(sheetUnprotectCmd, []string{"sht_token", "p1"})
	if err == nil {
		t.Fatal("sheetUnprotectCmd should fail with error (fail-closed)")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("sheetUnprotectCmd error = %v, want mentioning unsupported", err)
	}
}

// TestAttendanceQuery_FlagsAndEnums 验证考勤 employee_type 仅支持 employee_id 和 employee_no
func TestAttendanceQuery_FlagsAndEnums(t *testing.T) {
	taskFlag := attendanceUserTaskQueryCmd.Flags().Lookup("employee-type")
	if taskFlag == nil || taskFlag.DefValue != "employee_id" {
		t.Errorf("attendance user-task query employee-type default = %v, want employee_id", taskFlag)
	}
	statsFlag := attendanceUserStatsQueryCmd.Flags().Lookup("employee-type")
	if statsFlag == nil || statsFlag.DefValue != "employee_id" {
		t.Errorf("attendance user-stats query employee-type default = %v, want employee_id", statsFlag)
	}

	// 验证 attendance user-task query 支持 --as 和 --user-access-token
	if attendanceUserTaskQueryCmd.Flags().Lookup("as") == nil {
		t.Error("attendance user-task query should have --as flag")
	}
	if attendanceUserTaskQueryCmd.Flags().Lookup("user-access-token") == nil {
		t.Error("attendance user-task query should have --user-access-token flag")
	}

	// 验证 attendance user-stats query 保持 legacy 路径，不暴露 --user-access-token
	if attendanceUserStatsQueryCmd.Flags().Lookup("user-access-token") != nil {
		t.Error("attendance user-stats query should NOT have --user-access-token flag in legacy path")
	}
}

// TestMailRead_AsBotRejectsMailboxMeBeforeNetwork 验证 Mail 读操作在 --as bot 时在发起网络请求前拒绝 mailbox=me
func TestMailRead_AsBotRejectsMailboxMeBeforeNetwork(t *testing.T) {
	networkCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		networkCalled = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{}}`)
	}))
	defer srv.Close()
	setupCmdTestConfig(t, srv.URL)

	// 1. mail triage: --as bot, default mailbox=me
	_ = mailTriageCmd.Flags().Set("as", "bot")
	_ = mailTriageCmd.Flags().Set("mailbox", "me")
	err := mailTriageCmd.RunE(mailTriageCmd, []string{})
	if err == nil {
		t.Fatal("mail triage --as bot with mailbox=me should return error")
	}
	if !strings.Contains(err.Error(), "不支持 mailbox=\"me\"") {
		t.Errorf("error = %v, want mentioning mailbox=me rejection", err)
	}
	if networkCalled {
		t.Error("before network: 不应发起任何网络请求")
	}

	// 2. mail messages: --as bot, default mailbox=me
	networkCalled = false
	_ = mailMessagesCmd.Flags().Set("as", "bot")
	_ = mailMessagesCmd.Flags().Set("mailbox", "me")
	_ = mailMessagesCmd.Flags().Set("message-ids", "msg_1")
	err = mailMessagesCmd.RunE(mailMessagesCmd, []string{})
	if err == nil {
		t.Fatal("mail messages --as bot with mailbox=me should return error")
	}
	if !strings.Contains(err.Error(), "不支持 mailbox=\"me\"") {
		t.Errorf("error = %v", err)
	}
	if networkCalled {
		t.Error("before network: 不应发起任何网络请求")
	}

	// 3. mail thread: --as bot, default mailbox=me
	networkCalled = false
	_ = mailThreadCmd.Flags().Set("as", "bot")
	_ = mailThreadCmd.Flags().Set("mailbox", "me")
	_ = mailThreadCmd.Flags().Set("thread-id", "th_1")
	err = mailThreadCmd.RunE(mailThreadCmd, []string{})
	if err == nil {
		t.Fatal("mail thread --as bot with mailbox=me should return error")
	}
	if !strings.Contains(err.Error(), "不支持 mailbox=\"me\"") {
		t.Errorf("error = %v", err)
	}
	if networkCalled {
		t.Error("before network: 不应发起任何网络请求")
	}

	// 4. mail message: --as bot, default mailbox=me
	networkCalled = false
	_ = mailMessageCmd.Flags().Set("as", "bot")
	_ = mailMessageCmd.Flags().Set("mailbox", "me")
	_ = mailMessageCmd.Flags().Set("message-id", "m1")
	err = mailMessageCmd.RunE(mailMessageCmd, []string{})
	if err == nil {
		t.Fatal("mail message --as bot with mailbox=me should return error")
	}
	if !strings.Contains(err.Error(), "不支持 mailbox=\"me\"") {
		t.Errorf("error = %v", err)
	}
	if networkCalled {
		t.Error("before network: 不应发起任何网络请求")
	}
}

// TestMailRead_AsBotWithExplicitMailbox 验证 Mail 读操作在 --as bot 指定具体邮箱时正常以 Tenant Token 访问
func TestMailRead_AsBotWithExplicitMailbox(t *testing.T) {
	var gotAuthHeader string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"t-mock-tenant-token","expire":7200}`)
			return
		}
		gotAuthHeader = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[],"has_more":false}}`)
	}))
	defer srv.Close()
	setupCmdTestConfig(t, srv.URL)

	_ = mailTriageCmd.Flags().Set("as", "bot")
	_ = mailTriageCmd.Flags().Set("mailbox", "shared@example.com")
	_ = mailTriageCmd.Flags().Set("user-access-token", "")
	err := mailTriageCmd.RunE(mailTriageCmd, []string{})
	if err != nil {
		t.Fatalf("mail triage --as bot with explicit mailbox error: %v", err)
	}

	if !strings.HasPrefix(gotAuthHeader, "Bearer t-") {
		t.Errorf("auth header = %q, want Bearer t-...", gotAuthHeader)
	}
	if !strings.Contains(gotPath, "shared@example.com") {
		t.Errorf("path = %q, want containing shared@example.com", gotPath)
	}
}

// TestMailRead_AsUserWithMailboxMe 验证 Mail 读操作在 --as user 时支持 mailbox=me 并使用 User Token
func TestMailRead_AsUserWithMailboxMe(t *testing.T) {
	var gotAuthHeader string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[],"has_more":false}}`)
	}))
	defer srv.Close()
	setupCmdTestConfig(t, srv.URL)

	_ = mailTriageCmd.Flags().Set("as", "user")
	_ = mailTriageCmd.Flags().Set("mailbox", "me")
	_ = mailTriageCmd.Flags().Set("user-access-token", "u-explicit-user-token")
	err := mailTriageCmd.RunE(mailTriageCmd, []string{})
	if err != nil {
		t.Fatalf("mail triage --as user with mailbox=me error: %v", err)
	}

	if gotAuthHeader != "Bearer u-explicit-user-token" {
		t.Errorf("auth header = %q, want 'Bearer u-explicit-user-token'", gotAuthHeader)
	}
	if !strings.Contains(gotPath, "/open-apis/mail/v1/user_mailboxes/me/messages") {
		t.Errorf("path = %q, want me", gotPath)
	}
}

// TestAttendanceUserTask_AsBotAndUser 验证 attendance user-task query 的 --as bot 与 --as user 以及 corrupt token fail-closed
func TestAttendanceUserTask_AsBotAndUser(t *testing.T) {
	var gotAuthHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"t-att-tenant","expire":7200}`)
			return
		}
		gotAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"user_task_results":[]}}`)
	}))
	defer srv.Close()
	setupCmdTestConfig(t, srv.URL)

	// 1. --as bot + 指定工号
	_ = attendanceUserTaskQueryCmd.Flags().Set("as", "bot")
	_ = attendanceUserTaskQueryCmd.Flags().Set("employee-type", "employee_no")
	_ = attendanceUserTaskQueryCmd.Flags().Set("user-ids", "10001")
	_ = attendanceUserTaskQueryCmd.Flags().Set("start", "2026-05-01")
	_ = attendanceUserTaskQueryCmd.Flags().Set("end", "2026-05-18")
	_ = attendanceUserTaskQueryCmd.Flags().Set("user-access-token", "")

	err := attendanceUserTaskQueryCmd.RunE(attendanceUserTaskQueryCmd, []string{})
	if err != nil {
		t.Fatalf("attendance user-task query --as bot error: %v", err)
	}
	if !strings.HasPrefix(gotAuthHeader, "Bearer t-") {
		t.Errorf("auth header = %q, want 'Bearer t-...'", gotAuthHeader)
	}

	// 2. --as user + 本人自查（无需传 user-ids）
	gotAuthHeader = ""
	_ = attendanceUserTaskQueryCmd.Flags().Set("as", "user")
	_ = attendanceUserTaskQueryCmd.Flags().Set("user-ids", "")
	_ = attendanceUserTaskQueryCmd.Flags().Set("employee-type", "employee_no")
	_ = attendanceUserTaskQueryCmd.Flags().Set("user-access-token", "u-att-user")

	err = attendanceUserTaskQueryCmd.RunE(attendanceUserTaskQueryCmd, []string{})
	if err != nil {
		t.Fatalf("attendance user-task query --as user error: %v", err)
	}
	if gotAuthHeader != "Bearer u-att-user" {
		t.Errorf("auth header = %q, want 'Bearer u-att-user'", gotAuthHeader)
	}

	// 3. corrupt-token 测试：auto / user 模式下若配置了非法 token 文件或 refresh 失败，必须 fail-closed 报错，绝不静默切 Bot
	os.Setenv("FEISHU_PROFILE", "corrupt_profile_not_exist")
	_ = attendanceUserTaskQueryCmd.Flags().Set("as", "user")
	_ = attendanceUserTaskQueryCmd.Flags().Set("user-access-token", "")
	err = attendanceUserTaskQueryCmd.RunE(attendanceUserTaskQueryCmd, []string{})
	if err == nil {
		t.Fatal("corrupt / missing user token under --as user must return error (fail-closed)")
	}
	os.Unsetenv("FEISHU_PROFILE")
}
