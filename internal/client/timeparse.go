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

// unixSecondsUpperBound 纯数字输入按「秒」解释的上限（约公元 5138 年）。
// 超过它的全数字输入几乎必然是毫秒时间戳：本仓库多处以毫秒为单位
// （search_enrich.go 的 time.UnixMilli、mail.go 的 AfterTime // Unix 毫秒），
// 用户粘错单位很常见。若按秒解释，13 位毫秒值会变成公元 5 万多年，
// 服务端要么报 field validation failed，要么静默返回空结果集。
const unixSecondsUpperBound int64 = 1 << 37

// ParseTimeInput 解析 RFC3339 / YYYY-MM-DD / Unix 秒（13 位以上按毫秒识别）。
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
		// 用 startOfCalendarDay 而非直接构造 00:00：在午夜发生 DST 跳变的时区里
		// 零点可能不存在，直接构造会把日号归一化到前一天，导致整天查错。
		if endOfDay {
			y, m, d := t.Date()
			return time.Date(y, m, d, 23, 59, 59, 0, t.Location()), nil
		}
		dy, dm, dd := t.Date()
		return startOfCalendarDay(dy, dm, dd, t.Location()), nil
	}
	if isAllDigits(input) {
		n, err := strconv.ParseInt(input, 10, 64)
		if err == nil && n > 0 {
			// 超出秒量级的数值按毫秒解释，避免 13 位毫秒值被当成秒（公元 5 万多年）
			if n > unixSecondsUpperBound {
				return time.UnixMilli(n), nil
			}
			return time.Unix(n, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间 %q（支持 RFC3339 / YYYY-MM-DD / Unix 秒或毫秒）", input)
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

// startOfCalendarDay 返回日历日 (y, m, d) 在 loc 中**实际存在的最早时刻**。
//
// 直接用 time.Date(y, m, d, 0, 0, 0, 0, loc) 不安全：在午夜发生 DST 跳变的时区
// （如 America/Sao_Paulo 2018-11-04，00:00 不存在），Go 会把它归一化到前一天 23:00，
// 日号随之退一天，agenda / event-search 会整天查错并静默返回空结果。
//
// 注意必须传入**目标日期分量**，不能传一个已被归一化过的 time.Time——那样日号已经错了。
func startOfCalendarDay(y int, m time.Month, d int, loc *time.Location) time.Time {
	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if my, mm, md := midnight.Date(); my == y && mm == m && md == d {
		return midnight
	}
	// 午夜被归一化到别的日期：逐小时找该日实际存在的最早时刻
	earliest := time.Date(y, m, d, 12, 0, 0, 0, loc)
	for h := 0; h < 12; h++ {
		cand := time.Date(y, m, d, h, 0, 0, 0, loc)
		if cy, cm, cd := cand.Date(); cy == y && cm == m && cd == d {
			earliest = cand
			break
		}
	}
	return earliest
}

// parseCalendarDay 解析 YYYY-MM-DD 并返回其日历日分量（不受 DST 归一化影响）。
// 直接取 ParseInLocation 结果的 Date() 不可靠：午夜不存在时它已被归一化到前一天。
func parseCalendarDay(s string, loc *time.Location) (int, time.Month, int, error) {
	if _, err := time.ParseInLocation("2006-01-02", s, loc); err != nil {
		return 0, 0, 0, err
	}
	var y, mm, d int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d-%d-%d", &y, &mm, &d); err != nil {
		return 0, 0, 0, fmt.Errorf("日期格式应为 YYYY-MM-DD: %q", s)
	}
	return y, time.Month(mm), d, nil
}

// ParseAgendaDateRange 把 YYYY-MM-DD 转成本地日历窗口。
// 缺省结束日=起始日当天；结束时刻=次日当地午夜减 1 秒（DST 安全，包含端）。
//
// start/end 都基于**日历日期分量**计算，不对已被 DST 归一化的时刻做算术：
// 否则在午夜跳变时区（America/Sao_Paulo 2018-11-04）会算出倒挂区间（实测 dur=-1s），
// agenda 静默返回空结果且 exit 0。
func ParseAgendaDateRange(startDateStr, endDateStr string, now time.Time) (time.Time, time.Time, error) {
	loc := now.Location()

	startY, startM, startD := now.Date()
	if startDateStr != "" {
		y, m, d, err := parseCalendarDay(startDateStr, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("解析起始日期失败（格式应为 YYYY-MM-DD）: %w", err)
		}
		startY, startM, startD = y, m, d
	}

	endY, endM, endD := startY, startM, startD
	if endDateStr != "" {
		y, m, d, err := parseCalendarDay(endDateStr, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("解析结束日期失败（格式应为 YYYY-MM-DD）: %w", err)
		}
		endY, endM, endD = y, m, d
	}

	// 先按日历日先后校验顺序（用与时区无关的正午时刻比较，避免 DST 干扰）
	startNoon := time.Date(startY, startM, startD, 12, 0, 0, 0, loc)
	endNoon := time.Date(endY, endM, endD, 12, 0, 0, 0, loc)
	if startNoon.After(endNoon) {
		return time.Time{}, time.Time{}, fmt.Errorf("起始日期不能晚于结束日期")
	}

	start := startOfCalendarDay(startY, startM, startD, loc)
	// end = 结束日次日的最早时刻 - 1 秒
	nextY, nextM, nextD := endNoon.AddDate(0, 0, 1).Date()
	end := startOfCalendarDay(nextY, nextM, nextD, loc).Add(-time.Second)
	if !end.After(start) {
		// 兜底：极端时区规则下仍倒挂时退化为结束日 23:59:59，绝不返回倒挂区间
		end = time.Date(endY, endM, endD, 23, 59, 59, 0, loc)
	}
	return start, end, nil
}
