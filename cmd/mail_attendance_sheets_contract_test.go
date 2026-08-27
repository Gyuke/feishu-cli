package cmd

import (
	"strings"
	"testing"
)

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

// TestAttendanceQuery_EmployeeTypeEnum 验证考勤 employee_type 仅支持 employee_id 和 employee_no
func TestAttendanceQuery_EmployeeTypeEnum(t *testing.T) {
	// 验证 flag 存在且默认值为 employee_id
	taskFlag := attendanceUserTaskQueryCmd.Flags().Lookup("employee-type")
	if taskFlag == nil || taskFlag.DefValue != "employee_id" {
		t.Errorf("attendance user-task query employee-type default = %v, want employee_id", taskFlag)
	}
	statsFlag := attendanceUserStatsQueryCmd.Flags().Lookup("employee-type")
	if statsFlag == nil || statsFlag.DefValue != "employee_id" {
		t.Errorf("attendance user-stats query employee-type default = %v, want employee_id", statsFlag)
	}

	// 验证 user-access-token flag 注册
	if attendanceUserTaskQueryCmd.Flags().Lookup("user-access-token") == nil {
		t.Error("attendance user-task query should have --user-access-token flag")
	}
	if attendanceUserStatsQueryCmd.Flags().Lookup("user-access-token") == nil {
		t.Error("attendance user-stats query should have --user-access-token flag")
	}
}
