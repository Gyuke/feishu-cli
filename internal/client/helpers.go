package client

import (
	"strings"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

// firstString 返回可变参数字符串切片的第一个元素，空切片返回空字符串。
func firstString(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

// resolveTimeout 返回可变参数中第一个正值超时，否则返回默认值 def。
func resolveTimeout(def time.Duration, timeout []time.Duration) time.Duration {
	if len(timeout) > 0 && timeout[0] > 0 {
		return timeout[0]
	}
	return def
}

// UserTokenOption 当 userAccessToken 非空时返回请求选项，否则返回 nil（回退到 tenant token）
func UserTokenOption(userAccessToken string) []larkcore.RequestOptionFunc {
	if userAccessToken != "" {
		return []larkcore.RequestOptionFunc{larkcore.WithUserAccessToken(userAccessToken)}
	}
	return nil
}

// resolveTokenOpts 根据是否提供 userAccessToken 选择请求使用的 token 类型和请求选项。
func resolveTokenOpts(userAccessToken string) (larkcore.AccessTokenType, []larkcore.RequestOptionFunc) {
	if userAccessToken != "" {
		return larkcore.AccessTokenTypeUser, UserTokenOption(userAccessToken)
	}
	return larkcore.AccessTokenTypeTenant, nil
}

// StringVal 安全解引用字符串指针，nil 返回空字符串
func StringVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// BoolVal 安全解引用布尔指针，nil 返回 false
func BoolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

// IntVal 安全解引用 int 指针，nil 返回 0
func IntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// Int64Val 安全解引用 int64 指针，nil 返回 0
func Int64Val(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// IsRateLimitError 判断错误是否为频率限制错误（基于结构化 API/HTTP 码或明确限流短语，词边界安全）
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	if HasAPICode(err, 99991400) || HasAPICode(err, 429) || HasHTTPStatus(err, 429) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "frequency limit") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests")
}

// IsRetryableError 判断错误是否可重试（服务端临时错误或限流，基于结构化 API/HTTP 码或明确短语）
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if IsRateLimitError(err) {
		return true
	}
	// 已携带明确的客户端错误状态（4xx）时直接判为不可重试：
	// 响应 body 里常出现无关的 "status code: 500" / "gateway timeout" 之类字样
	// （如 HTTP 400, body: {"msg":"unexpected status code: 500 from backend"}），
	// 若继续往下做短语匹配，会把永久性 4xx 误判成可重试而白跑几轮退避。
	// 429 除外——它由上面的 IsRateLimitError 处理并确实应当重试。
	for _, status := range []int{400, 401, 403, 404, 405, 409, 413, 422} {
		if HasHTTPStatus(err, status) {
			return false
		}
	}
	for _, code := range []int{500, 502, 503, 504} {
		if HasAPICode(err, code) || HasHTTPStatus(err, code) {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "internal error") ||
		strings.Contains(msg, "internal server error") ||
		strings.Contains(msg, "bad gateway") ||
		strings.Contains(msg, "service unavailable") ||
		strings.Contains(msg, "gateway timeout")
}

// IsPermanentError 判断错误是否为永久性错误（不应重试）
func IsPermanentError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Parse error") ||
		strings.Contains(msg, "Invalid request parameter")
}
