package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// ParseTimeInput 解析 RFC3339 / YYYY-MM-DD / Unix 秒。
// endOfDay 仅对日期粒度生效：结束日对齐到当天 23:59:59。
func ParseTimeInput(input string, endOfDay bool) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return time.Time{}, fmt.Errorf("时间为空")
	}
	for _, f := range []string{time.RFC3339, "2006-01-02T15:04Z07:00", "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(f, input); err == nil {
			return t, nil
		}
	}
	for _, f := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(f, input, time.Local); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", input, time.Local); err == nil {
		if endOfDay {
			return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location()), nil
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()), nil
	}
	if isAllDigits(input) {
		n, err := strconv.ParseInt(input, 10, 64)
		if err == nil && n > 0 {
			return time.Unix(n, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间 %q（支持 RFC3339 / YYYY-MM-DD / Unix 秒）", input)
}

// ParseSearchEventTimeRange 按官方 search_event 语义展开 --start/--end。
// 单边缺省补同一天边界；start>end 报错。
func ParseSearchEventTimeRange(startInput, endInput string) (string, string, error) {
	startInput = strings.TrimSpace(startInput)
	endInput = strings.TrimSpace(endInput)
	if startInput == "" && endInput == "" {
		return "", "", nil
	}
	var startT, endT time.Time
	var err error
	if startInput != "" {
		startT, err = ParseTimeInput(startInput, false)
		if err != nil {
			return "", "", fmt.Errorf("解析开始时间失败: %w", err)
		}
	}
	if endInput != "" {
		endT, err = ParseTimeInput(endInput, true)
		if err != nil {
			return "", "", fmt.Errorf("解析结束时间失败: %w", err)
		}
	}
	if startInput == "" {
		t := endT.In(time.Local)
		startT = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
	if endInput == "" {
		t := startT.In(time.Local)
		endT = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
	}
	if startT.After(endT) {
		return "", "", fmt.Errorf("开始时间不能晚于结束时间")
	}
	return startT.Format(time.RFC3339), endT.Format(time.RFC3339), nil
}

// ParseAgendaDateRange 把 YYYY-MM-DD 转成本地日历窗口。
// 缺省结束日=起始日当天；结束时刻=次日当地午夜减 1 秒（DST 安全，包含端）。
func ParseAgendaDateRange(startDateStr, endDateStr string, now time.Time) (time.Time, time.Time, error) {
	loc := now.Location()
	var start time.Time
	if startDateStr != "" {
		t, err := time.ParseInLocation("2006-01-02", startDateStr, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("解析起始日期失败（格式应为 YYYY-MM-DD）: %w", err)
		}
		start = t
	} else {
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	}

	var endDay time.Time
	if endDateStr != "" {
		t, err := time.ParseInLocation("2006-01-02", endDateStr, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("解析结束日期失败（格式应为 YYYY-MM-DD）: %w", err)
		}
		endDay = t
	} else {
		endDay = start
	}
	if start.After(endDay) {
		return time.Time{}, time.Time{}, fmt.Errorf("起始日期不能晚于结束日期")
	}
	end := time.Date(endDay.Year(), endDay.Month(), endDay.Day()+1, 0, 0, 0, 0, loc).Add(-time.Second)
	return start, end, nil
}
