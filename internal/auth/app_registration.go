package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/riba2534/feishu-cli/internal/config"
)

const (
	feishuAccountsBase = config.OfficialFeishuAccounts
	feishuOpenBase     = config.OfficialFeishuOpen
	appRegPath         = "/oauth/v1/app/registration"

	maxRegPollInterval = 60
	maxRegPollAttempts = 200
)

// AppRegistrationResponse 应用注册设备流响应
type AppRegistrationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// AppRegistrationResult 应用注册成功结果
type AppRegistrationResult struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	TenantBrand  string `json:"tenant_brand,omitempty"` // "feishu" or "lark"
}

// RequestAppRegistration 发起应用自注册 Device Flow
// 调用 accounts.feishu.cn/oauth/v1/app/registration (action=begin)
func RequestAppRegistration(baseURL string) (*AppRegistrationResponse, error) {
	accountsBase := feishuAccountsBase
	openBase := feishuOpenBase
	if strings.Contains(baseURL, "larksuite.com") {
		accountsBase = "https://accounts.larksuite.com"
		openBase = "https://open.larksuite.com"
	}

	endpoint := accountsBase + appRegPath

	form := url.Values{}
	form.Set("action", "begin")
	form.Set("archetype", "PersonalAgent")
	form.Set("auth_method", "client_secret")
	form.Set("request_user_info", "open_id tenant_brand")

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := config.NewHTTPClient(15 * time.Second)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("HTTP %d，响应非 JSON: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	if _, hasErr := data["error"]; hasErr || resp.StatusCode >= 400 {
		desc := getStrField(data, "error_description")
		if desc == "" {
			desc = getStrField(data, "error")
		}
		if desc == "" {
			desc = "未知错误"
		}
		return nil, fmt.Errorf("应用注册失败: %s", desc)
	}

	userCode := getStrField(data, "user_code")
	verificationURIComplete := fmt.Sprintf("%s/page/cli?user_code=%s", openBase, userCode)

	return &AppRegistrationResponse{
		DeviceCode:              getStrField(data, "device_code"),
		UserCode:                userCode,
		VerificationURI:         getStrField(data, "verification_uri"),
		VerificationURIComplete: verificationURIComplete,
		ExpiresIn:               getIntField(data, "expires_in", 300),
		Interval:                getIntField(data, "interval", 5),
	}, nil
}

// PollAppRegistration 轮询应用注册结果
// 用户扫码确认后返回 client_id + client_secret
func PollAppRegistration(ctx context.Context, baseURL, deviceCode string, interval, expiresIn int, onTick func(elapsed, total int)) (*AppRegistrationResult, error) {
	accountsBase := feishuAccountsBase
	if strings.Contains(baseURL, "larksuite.com") {
		accountsBase = "https://accounts.larksuite.com"
	}

	endpoint := accountsBase + appRegPath
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	currentInterval := interval
	if currentInterval <= 0 {
		currentInterval = 5
	}
	startTime := time.Now()

	httpClient := config.NewHTTPClient(15 * time.Second)
	attempts := 0

	for time.Now().Before(deadline) && attempts < maxRegPollAttempts {
		attempts++

		if ctx.Err() != nil {
			return nil, fmt.Errorf("轮询被取消")
		}

		// 逐秒等待，支持进度回调
		for i := 0; i < currentInterval; i++ {
			if !time.Now().Before(deadline) {
				break
			}
			if onTick != nil {
				elapsed := int(time.Since(startTime).Seconds())
				onTick(elapsed, expiresIn)
			}
			time.Sleep(time.Second)
		}

		if !time.Now().Before(deadline) {
			break
		}

		form := url.Values{}
		form.Set("action", "poll")
		form.Set("device_code", deviceCode)

		req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := httpClient.Do(req)
		if err != nil {
			currentInterval = min(currentInterval+1, maxRegPollInterval)
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			currentInterval = min(currentInterval+1, maxRegPollInterval)
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			currentInterval = min(currentInterval+1, maxRegPollInterval)
			continue
		}

		errStr := getStrField(data, "error")

		// 成功：有 client_id
		if errStr == "" && getStrField(data, "client_id") != "" {
			result := &AppRegistrationResult{
				ClientID:     getStrField(data, "client_id"),
				ClientSecret: getStrField(data, "client_secret"),
			}
			if userInfo, ok := data["user_info"].(map[string]interface{}); ok {
				result.TenantBrand = getStrField(userInfo, "tenant_brand")
			}

			// 如果是 lark 租户但没拿到 secret，用 lark 端点重试
			if result.ClientSecret == "" && result.TenantBrand == "lark" {
				larkEndpoint := "https://accounts.larksuite.com" + appRegPath
				larkForm := url.Values{}
				larkForm.Set("action", "poll")
				larkForm.Set("device_code", deviceCode)
				larkReq, _ := http.NewRequest("POST", larkEndpoint, strings.NewReader(larkForm.Encode()))
				larkReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if larkResp, err := httpClient.Do(larkReq); err == nil {
					larkBody, _ := io.ReadAll(io.LimitReader(larkResp.Body, 1<<20))
					larkResp.Body.Close()
					var larkData map[string]interface{}
					if json.Unmarshal(larkBody, &larkData) == nil {
						if s := getStrField(larkData, "client_secret"); s != "" {
							result.ClientSecret = s
						}
					}
				}
			}

			return result, nil
		}

		switch errStr {
		case "authorization_pending":
			continue
		case "slow_down":
			currentInterval = min(currentInterval+5, maxRegPollInterval)
			continue
		case "access_denied":
			return nil, fmt.Errorf("用户拒绝了应用注册")
		case "expired_token", "invalid_grant":
			return nil, fmt.Errorf("注册码已过期，请重试")
		}

		desc := getStrField(data, "error_description")
		if desc == "" {
			desc = errStr
		}
		if desc == "" {
			desc = "未知错误"
		}
		return nil, fmt.Errorf("应用注册失败: %s", desc)
	}

	return nil, fmt.Errorf("应用注册超时，请重试")
}

func getStrField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getIntField(m map[string]interface{}, key string, defaultVal int) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
