package registry

import (
	"strconv"
	"strings"
)

// isNewer 判断 version a 是否比 b 新（用于 cache 不得覆盖更新的 embedded）。
// 双方都能解析为 semver 时按 major.minor.patch 比较；
// b 无法解析时只要 a 能解析就视为更新；a 无法解析则不能确认更新。
func isNewer(a, b string) bool {
	ap := parseSemver(a)
	bp := parseSemver(b)
	if ap == nil {
		return false
	}
	if bp == nil {
		return true
	}
	for i := 0; i < 3; i++ {
		if ap[i] > bp[i] {
			return true
		}
		if ap[i] < bp[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return nil
	}
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return nil
	}
	out := make([]int, 3)
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

// isOlder 判断 version a 是否严格早于 b。
// 与 isNewer 的区别在于「相等」的处理：cache 与 embedded 版本相同时不算过期，
// 应允许 overlay（官方 meta 端点的顶层 version 长期恒为 1.0.0）。
// 无法解析的 a 视为不可信（当作更旧），无法解析的 b 视为不设下限。
func isOlder(a, b string) bool {
	ap := parseSemver(a)
	bp := parseSemver(b)
	if ap == nil {
		return true
	}
	if bp == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return true
		}
		if ap[i] > bp[i] {
			return false
		}
	}
	return false
}
