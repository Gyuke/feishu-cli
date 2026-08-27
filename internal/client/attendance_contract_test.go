package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAttendanceUserTask_SelfQueryContract 验证考勤打卡本人自查路径（employee_no + 空 user_ids）与 User Token 支持
func TestAttendanceUserTask_SelfQueryContract(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotAuthHeader string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuthHeader = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"user_task_results":[{"result_id":"res_1","user_id":"u_self","day":20260501}]}}`)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	// 本人自查：employee_type=employee_no, user_ids=[]
	res, err := QueryAttendanceUserTasks("employee_no", []string{}, 20260501, 20260518, false, true, false, "u-user-token-123")
	if err != nil {
		t.Fatalf("QueryAttendanceUserTasks self query error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/open-apis/attendance/v1/user_tasks/query" {
		t.Errorf("path = %s, want /open-apis/attendance/v1/user_tasks/query", gotPath)
	}
	if !strings.Contains(gotQuery, "employee_type=employee_no") {
		t.Errorf("query %s 应包含 employee_type=employee_no", gotQuery)
	}
	if gotAuthHeader != "Bearer u-user-token-123" {
		t.Errorf("auth header = %s, want 'Bearer u-user-token-123'", gotAuthHeader)
	}

	// 验证 body 中的 user_ids 为空数组
	userIDs, ok := gotBody["user_ids"].([]any)
	if !ok {
		t.Errorf("body.user_ids 格式不正确: %v", gotBody["user_ids"])
	} else if len(userIDs) != 0 {
		t.Errorf("body.user_ids 应为空列表，got: %v", userIDs)
	}

	if len(res.UserTaskResults) != 1 {
		t.Fatalf("results count = %d, want 1", len(res.UserTaskResults))
	}
}

// TestAttendanceUserTask_SpecifiedUsersContract 验证指定员工 ID 查询打卡记录
func TestAttendanceUserTask_SpecifiedUsersContract(t *testing.T) {
	var gotQuery string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"user_task_results":[{"result_id":"res_1","user_id":"28471234","day":20260501}]}}`)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	res, err := QueryAttendanceUserTasks("employee_id", []string{"28471234"}, 20260501, 20260518, true, true, false)
	if err != nil {
		t.Fatalf("QueryAttendanceUserTasks error: %v", err)
	}

	if !strings.Contains(gotQuery, "employee_type=employee_id") {
		t.Errorf("query %s 应包含 employee_type=employee_id", gotQuery)
	}
	userIDs, ok := gotBody["user_ids"].([]any)
	if !ok || len(userIDs) != 1 || userIDs[0] != "28471234" {
		t.Errorf("body.user_ids = %v, want ['28471234']", gotBody["user_ids"])
	}
	if len(res.UserTaskResults) != 1 {
		t.Fatalf("results count = %d, want 1", len(res.UserTaskResults))
	}
}

// TestAttendanceUserTask_Validation 验证 employee_type 为 employee_id 时 user_ids 不能为空等边界校验
func TestAttendanceUserTask_Validation(t *testing.T) {
	setupTestConfig(t, "http://127.0.0.1:9999")

	// employee_id 且 user_ids 为空应报错
	_, err := QueryAttendanceUserTasks("employee_id", []string{}, 20260501, 20260518, false, true, false)
	if err == nil {
		t.Fatal("employee_id 且 user_ids 为空应报错")
	}
	if !strings.Contains(err.Error(), "user_ids 不能为空") {
		t.Errorf("错误提示应说明 user_ids 不能为空: %v", err)
	}
}

// TestAttendanceUserStats_Contract 验证考勤统计数据查询（legacy 路径，Tenant Token，必填 user_ids）
func TestAttendanceUserStats_Contract(t *testing.T) {
	var gotQuery string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"user_datas":[{"user_id":"28470001","name":"张三","datas":[{"code":"501","title":"出勤天数","value":"10"}]}]}}`)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	res, err := QueryAttendanceUserStats("employee_id", "daily", 20260501, 20260518, []string{"28470001"}, "", "zh", false, false)
	if err != nil {
		t.Fatalf("QueryAttendanceUserStats error: %v", err)
	}

	if !strings.Contains(gotQuery, "employee_type=employee_id") {
		t.Errorf("query %s 应包含 employee_type=employee_id", gotQuery)
	}
	if gotBody["stats_type"] != "daily" {
		t.Errorf("body.stats_type = %v, want daily", gotBody["stats_type"])
	}
	userIDs, ok := gotBody["user_ids"].([]any)
	if !ok || len(userIDs) != 1 || userIDs[0] != "28470001" {
		t.Errorf("body.user_ids = %v, want ['28470001']", gotBody["user_ids"])
	}
	if len(res.UserDatas) != 1 {
		t.Fatalf("user_datas count = %d, want 1", len(res.UserDatas))
	}
}
