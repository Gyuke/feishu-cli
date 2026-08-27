package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/riba2534/feishu-cli/internal/config"
)

const tatHTTPTimeout = 10 * time.Second

type tatResponse struct {
	Code             int    `json:"code"`
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Msg              string `json:"msg"`
}

// TATEndpointFunc 解析 Accounts OAuth v3 token 端点，测试可替换为 httptest URL。
var TATEndpointFunc = DefaultTATEndpoint

// DefaultTATEndpoint 返回官方 Accounts `{origin}/oauth/v3/token`。
// FEISHU_TAT_ENDPOINT 仅在 loopback 时生效，供本地 mock；远端覆盖会被忽略以免外送凭证。
func DefaultTATEndpoint(baseURL string) string {
	if ep := strings.TrimSpace(os.Getenv("FEISHU_TAT_ENDPOINT")); ep != "" {
		if u, err := url.Parse(ep); err == nil &&
			(strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) &&
			config.IsLoopbackHost(u.Hostname()) {
			return ep
		}
	}
	return strings.TrimRight(config.ResolveAccountsBase(baseURL), "/") + config.OAuthTokenV3Path
}

// FetchTenantAccessToken 用 client_credentials 向官方 Accounts OAuth v3 换取 tenant access token。
func FetchTenantAccessToken(appID, appSecret, baseURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tatHTTPTimeout)
	defer cancel()
	return fetchTenantAccessToken(ctx, config.NewHTTPClient(tatHTTPTimeout), appID, appSecret, baseURL)
}

func fetchTenantAccessToken(ctx context.Context, httpClient *http.Client, appID, appSecret, baseURL string) (string, error) {
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("缺少 app_id 或 app_secret 配置")
	}
	if httpClient == nil {
		httpClient = config.NewHTTPClient(tatHTTPTimeout)
	}
	endpoint := TATEndpointFunc(baseURL)
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("tenant token 端点无效: %w", err)
	}
	if err := config.CheckRequestURL(u); err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", appID)
	form.Set("client_secret", appSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("构造 tenant token 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 tenant token 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := readLimitedAuthBody(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取 tenant token 响应失败: %w", err)
	}

	var result tatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("获取 tenant_access_token 失败: HTTP %d %s", resp.StatusCode, authBodyPreview(body))
		}
		return "", fmt.Errorf("解析 tenant token 响应失败（HTTP %d）: %w", resp.StatusCode, err)
	}

	if resp.StatusCode < 400 && result.Code == 0 && result.AccessToken != "" && result.Error == "" {
		return result.AccessToken, nil
	}

	if result.Error == "server_error" || result.Error == "temporarily_unavailable" || result.Error == "slow_down" || resp.StatusCode >= 500 {
		desc := result.ErrorDescription
		if desc == "" {
			desc = result.Msg
		}
		return "", fmt.Errorf("tenant token 端点暂时失败（HTTP %d, code=%d, error=%s）: %s",
			resp.StatusCode, result.Code, result.Error, redactAuthPreview(desc))
	}

	return "", classifyTATFailure(resp.StatusCode, body)
}

func classifyTATFailure(status int, body []byte) error {
	var result tatResponse
	if json.Unmarshal(body, &result) == nil {
		desc := result.ErrorDescription
		if desc == "" {
			desc = result.Msg
		}
		if desc == "" {
			desc = result.Error
		}
		if result.Error != "" || result.Code != 0 {
			if desc == "" {
				desc = "未知错误"
			}
			desc = redactAuthPreview(desc)
			if result.Error != "" {
				return fmt.Errorf("获取 tenant_access_token 失败: HTTP %d error=%s code=%d %s",
					status, result.Error, result.Code, desc)
			}
			return fmt.Errorf("获取 tenant_access_token 失败: HTTP %d code=%d %s", status, result.Code, desc)
		}
		if result.AccessToken == "" {
			return fmt.Errorf("tenant token 响应缺少 access_token（HTTP %d）", status)
		}
	}
	return fmt.Errorf("获取 tenant_access_token 失败: HTTP %d %s", status, authBodyPreview(body))
}

// FetchTenantAccessTokenContext 与 FetchTenantAccessToken 相同，但使用调用方 context。
func FetchTenantAccessTokenContext(ctx context.Context, appID, appSecret, baseURL string) (string, error) {
	return fetchTenantAccessToken(ctx, config.NewHTTPClient(tatHTTPTimeout), appID, appSecret, baseURL)
}
