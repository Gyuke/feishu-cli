package client

import (
	"fmt"
	"testing"
)

func TestHasAPICode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		want bool
	}{
		{"标准 code= 形态", fmt.Errorf("获取失败: code=2091003, msg=生成中"), 2091003, true},
		{"SDK code: 形态", fmt.Errorf("code: 99991400, msg: request trigger frequency limit"), 99991400, true},
		{"包装后仍命中", fmt.Errorf("外层: %w", fmt.Errorf("code=1062507, msg=full")), 1062507, true},
		{"raw body JSON 形态", fmt.Errorf(`HTTP 400, body: {"code": 232033,"msg":"x"}`), 232033, true},
		{"log_id 同数字串不误判", fmt.Errorf(`code=99991679, msg=x, log_id=20260722091003ABC`), 2091003, false},
		{"数字是前缀不误判", fmt.Errorf("code=10625071, msg=x"), 1062507, false},
		{"nil 错误", nil, 1062507, false},
		{"无关错误", fmt.Errorf("网络超时"), 232033, false},
		{"log_id 含 429 不误判为 code 429", fmt.Errorf("code=10000, msg=fail, log_id=20260429123456"), 429, false},
		{"log_id 含 500 不误判为 code 500", fmt.Errorf("code=10000, msg=fail, log_id=20260827123450000000000000000000"), 500, false},
	}
	for _, c := range cases {
		if got := HasAPICode(c.err, c.code); got != c.want {
			t.Errorf("%s: HasAPICode(%v, %d) = %v, want %v", c.name, c.err, c.code, got, c.want)
		}
	}
}

func TestHasHTTPStatus(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		want   bool
	}{
		{"HTTP 500", fmt.Errorf("HTTP 500, body: internal error"), 500, true},
		{"HTTP 状态码 502", fmt.Errorf("下载失败: HTTP 状态码 502"), 502, true},
		{"status code: 429", fmt.Errorf("request failed: status code: 429"), 429, true},
		{"status code 503", fmt.Errorf("status code 503"), 503, true},
		{"nil 错误", nil, 500, false},
		{"log_id 含 500 不误判", fmt.Errorf("code=10000, msg=fail, log_id=20260827123450000000000000000000"), 500, false},
		{"log_id 含 429 不误判", fmt.Errorf("code=10000, msg=fail, log_id=20260429123456"), 429, false},
		{"token 含 502 不误判", fmt.Errorf("token=boxcn502abcdef"), 502, false},
	}
	for _, c := range cases {
		if got := HasHTTPStatus(c.err, c.status); got != c.want {
			t.Errorf("%s: HasHTTPStatus(%v, %d) = %v, want %v", c.name, c.err, c.status, got, c.want)
		}
	}
}
