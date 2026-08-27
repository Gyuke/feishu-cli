package client

import (
	"testing"
	"time"
)

// TestParseTimeInput_SecondsAndMilliseconds 验证纯数字输入按数量级区分 Unix 秒与毫秒。
// 回归防护：曾无条件走 time.Unix(n, 0)，13 位毫秒值被当成秒，
// 得到公元 56971 年，导致 calendar event-search / search messages 拿到无意义时间边界。
func TestParseTimeInput_SecondsAndMilliseconds(t *testing.T) {
	want := time.Unix(1735689600, 0) // 2025-01-01T00:00:00Z

	cases := []struct {
		name  string
		input string
	}{
		{"Unix 秒", "1735689600"},
		{"Unix 毫秒（同一时刻）", "1735689600000"},
	}
	for _, tc := range cases {
		got, err := ParseTimeInput(tc.input, false)
		if err != nil {
			t.Fatalf("%s: ParseTimeInput(%q) 报错: %v", tc.name, tc.input, err)
		}
		if !got.Equal(want) {
			t.Errorf("%s: ParseTimeInput(%q) = %s，want %s", tc.name, tc.input, got.UTC(), want.UTC())
		}
		if y := got.Year(); y < 1970 || y > 3000 {
			t.Errorf("%s: 年份 %d 明显越界（单位识别错误）", tc.name, y)
		}
	}
}

// TestParseTimeInput_TextFormats 验证文本格式解析不受数字分支影响
func TestParseTimeInput_TextFormats(t *testing.T) {
	if _, err := ParseTimeInput("2025-01-01T00:00:00Z", false); err != nil {
		t.Errorf("RFC3339 应可解析: %v", err)
	}

	start, err := ParseTimeInput("2025-01-01", false)
	if err != nil {
		t.Fatalf("日期粒度应可解析: %v", err)
	}
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 {
		t.Errorf("endOfDay=false 应对齐 00:00:00，得到 %s", start)
	}

	end, err := ParseTimeInput("2025-01-01", true)
	if err != nil {
		t.Fatalf("日期粒度应可解析: %v", err)
	}
	if end.Hour() != 23 || end.Minute() != 59 || end.Second() != 59 {
		t.Errorf("endOfDay=true 应对齐 23:59:59，得到 %s", end)
	}

	for _, bad := range []string{"", "   ", "not-a-time", "0"} {
		if _, err := ParseTimeInput(bad, false); err == nil {
			t.Errorf("非法输入 %q 应报错", bad)
		}
	}
}

// TestParseAgendaDateRange_MidnightDSTNoInvertedWindow 验证午夜 DST 跳变时区不产生倒挂/异常区间。
// 回归防护：America/Sao_Paulo 2018-11-04 的 00:00 不存在，time.Date 会归一化到前一天 23:00。
// 旧实现用归一化后的时刻做 Day()+1 运算，得到 start=23:00 / end=前一天 22:59:59（dur=-1s），
// agenda 静默返回空结果且 exit 0。
func TestParseAgendaDateRange_MidnightDSTNoInvertedWindow(t *testing.T) {
	zones := []string{"America/Sao_Paulo", "Asia/Shanghai", "America/New_York", "Australia/Lord_Howe"}
	dates := []string{"2018-11-03", "2018-11-04", "2018-02-17", "2018-02-18"}

	for _, tz := range zones {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Skipf("时区库不可用: %v", err)
		}
		now := time.Date(2018, 11, 3, 12, 0, 0, 0, loc)
		for _, d := range dates {
			start, end, err := ParseAgendaDateRange(d, d, now)
			if err != nil {
				t.Errorf("%s %s: 不应报错: %v", tz, d, err)
				continue
			}
			if !end.After(start) {
				t.Errorf("%s %s: 区间倒挂 start=%s end=%s", tz, d, start, end)
			}
			if dur := end.Sub(start); dur < 20*time.Hour || dur > 26*time.Hour {
				t.Errorf("%s %s: 单日区间长度异常 %v（start=%s end=%s）", tz, d, dur, start, end)
			}
			// start 必须落在请求的日历日内（不得被 DST 归一化甩到前一天）
			if got := start.Format("2006-01-02"); got != d {
				t.Errorf("%s %s: start 落在 %s，应仍在请求日内", tz, d, got)
			}
		}

		// 多日区间同样不得倒挂
		start, end, err := ParseAgendaDateRange("2018-11-03", "2018-11-05", now)
		if err != nil {
			t.Errorf("%s: 多日区间报错: %v", tz, err)
		} else if !end.After(start) {
			t.Errorf("%s: 多日区间倒挂 start=%s end=%s", tz, start, end)
		}
	}

	// start > end 仍须报错
	loc, _ := time.LoadLocation("Asia/Shanghai")
	if _, _, err := ParseAgendaDateRange("2018-11-05", "2018-11-03", time.Date(2018, 11, 3, 12, 0, 0, 0, loc)); err == nil {
		t.Error("起始晚于结束应报错")
	}
}
