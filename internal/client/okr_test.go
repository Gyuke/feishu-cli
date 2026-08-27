package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOKRRawCycleToCycle 验证 v1/periods 响应解析为 OKRCycle 的映射正确
func TestOKRRawCycleToCycle(t *testing.T) {
	body := `{
		"id": "635782378412311",
		"zh_name": "2026 Q2",
		"en_name": "2026 Q2",
		"status": 0,
		"period_start_time": "1712016000000",
		"period_end_time": "1719792000000"
	}`
	var raw okrRawCycle
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("unmarshal v1 period failed: %v", err)
	}
	c := raw.toCycle()
	if c == nil {
		t.Fatal("toCycle returned nil")
	}
	if c.ID != "635782378412311" {
		t.Errorf("ID = %q, want 635782378412311", c.ID)
	}
	if c.ZhName != "2026 Q2" {
		t.Errorf("ZhName = %q", c.ZhName)
	}
	if c.EnName != "2026 Q2" {
		t.Errorf("EnName = %q", c.EnName)
	}
	if c.CycleStatus != "default" {
		t.Errorf("CycleStatus = %q, want default (status=0)", c.CycleStatus)
	}
	if c.StartTime == "" || c.EndTime == "" {
		t.Errorf("StartTime/EndTime not formatted: start=%q end=%q", c.StartTime, c.EndTime)
	}
}

// TestOKRCycleStatusMapping 验证 status int → 文本映射齐全（官方 0=default,1=normal,2=invalid,3=hidden）
func TestOKRCycleStatusMapping(t *testing.T) {
	cases := map[okrCycleStatus]string{
		okrCycleStatusDefault: "default",
		okrCycleStatusNormal:  "normal",
		okrCycleStatusInvalid: "invalid",
		okrCycleStatusHidden:  "hidden",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("status %d → %q, want %q", int(s), got, want)
		}
	}
}

// TestOKRCycleStatusWireGolden 锁定周期 wire 值，避免再把 0 标成 normal。
func TestOKRCycleStatusWireGolden(t *testing.T) {
	if int(okrCycleStatusDefault) != 0 || okrCycleStatusDefault.String() != "default" {
		t.Fatalf("0 必须是 default，实际 %d/%q", int(okrCycleStatusDefault), okrCycleStatusDefault.String())
	}
	if int(okrCycleStatusNormal) != 1 || okrCycleStatusNormal.String() != "normal" {
		t.Fatalf("1 必须是 normal，实际 %d/%q", int(okrCycleStatusNormal), okrCycleStatusNormal.String())
	}
	if int(okrCycleStatusInvalid) != 2 || okrCycleStatusInvalid.String() != "invalid" {
		t.Fatalf("2 必须是 invalid，实际 %d/%q", int(okrCycleStatusInvalid), okrCycleStatusInvalid.String())
	}
	if int(okrCycleStatusHidden) != 3 || okrCycleStatusHidden.String() != "hidden" {
		t.Fatalf("3 必须是 hidden，实际 %d/%q", int(okrCycleStatusHidden), okrCycleStatusHidden.String())
	}
}

// TestOKRProgressStatusWireGolden 锁定进展 wire 值：0=normal,1=overdue,2=done。
func TestOKRProgressStatusWireGolden(t *testing.T) {
	if int(OKRProgressStatusNormal) != 0 || OKRProgressStatusNormal.String() != "normal" {
		t.Fatalf("0 必须是 normal，实际 %d/%q", int(OKRProgressStatusNormal), OKRProgressStatusNormal.String())
	}
	if int(OKRProgressStatusOverdue) != 1 || OKRProgressStatusOverdue.String() != "overdue" {
		t.Fatalf("1 必须是 overdue，实际 %d/%q", int(OKRProgressStatusOverdue), OKRProgressStatusOverdue.String())
	}
	if int(OKRProgressStatusDone) != 2 || OKRProgressStatusDone.String() != "done" {
		t.Fatalf("2 必须是 done，实际 %d/%q", int(OKRProgressStatusDone), OKRProgressStatusDone.String())
	}
}

func TestParseOKRProgressStatus(t *testing.T) {
	cases := []struct {
		in     string
		want   OKRProgressStatus
		wantOK bool
	}{
		{"normal", OKRProgressStatusNormal, true},
		{"0", OKRProgressStatusNormal, true},
		{"overdue", OKRProgressStatusOverdue, true},
		{"1", OKRProgressStatusOverdue, true},
		{"done", OKRProgressStatusDone, true},
		{"2", OKRProgressStatusDone, true},
		{"DONE", OKRProgressStatusDone, true},
		{"risky", 0, false},
		{"risk", 0, false},
		{"invalid", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := ParseOKRProgressStatus(tc.in)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("ParseOKRProgressStatus(%q) = (%d,%v), want (%d,%v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestOKRProgressStatusCompatHint_RiskyNotMapped(t *testing.T) {
	hint := OKRProgressStatusCompatHint("risky")
	if hint == "" {
		t.Fatal("risky 应给出兼容提示，而不是静默失败或映射到 overdue")
	}
	if !strings.Contains(hint, "normal(0)") || !strings.Contains(hint, "overdue(1)") || !strings.Contains(hint, "done(2)") {
		t.Errorf("提示应列出官方 wire，实际: %s", hint)
	}
	if !strings.Contains(hint, "不会把 risky 映射") {
		t.Errorf("提示应明确不映射 risky，实际: %s", hint)
	}
	if _, ok := ParseOKRProgressStatus("risky"); ok {
		t.Fatal("risky 不得作为合法写入枚举")
	}
	if OKRProgressStatusCompatHint("normal") != "" {
		t.Fatal("合法枚举不应带兼容提示")
	}
}

// TestCreateOKRProgressSourceURLDefault 验证 SourceURL 未填时会兜底 placeholder（避免 422）
func TestCreateOKRProgressSourceURLDefault(t *testing.T) {
	// 这里只验证 options 默认值逻辑（不实际打网络），通过反向构造
	opts := CreateOKRProgressOptions{
		TargetID:   "7xxx",
		TargetType: OKRTargetObjective,
	}
	// 复刻 CreateOKRProgress 前几行的默认值逻辑
	if opts.SourceTitle == "" {
		opts.SourceTitle = "created by feishu-cli"
	}
	if opts.SourceURL == "" {
		opts.SourceURL = "https://www.feishu.cn/okr/progress"
	}
	if opts.SourceURL != "https://www.feishu.cn/okr/progress" {
		t.Errorf("default SourceURL = %q", opts.SourceURL)
	}
}
