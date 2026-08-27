package auth

import (
	"errors"
	"fmt"
	"os"
)

// logf 输出日志到 stderr，避免污染 stdout 的 JSON 输出
func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

// ErrNoUserTokenConfigured 表示未找到任何 User Token 来源（无 flag/env/token.json/config）。
var ErrNoUserTokenConfigured = errors.New("缺少 User Access Token，请通过以下方式之一提供:\n" +
	"  1. OAuth 登录: feishu-cli auth login\n" +
	"  2. 命令行参数: --user-access-token <token>\n" +
	"  3. 环境变量: export FEISHU_USER_ACCESS_TOKEN=<token>\n" +
	"  4. 配置文件: user_access_token: <token>")

// IsNoUserTokenConfigured 判断是否为未配置 User Token 的自然缺失状态。
func IsNoUserTokenConfigured(err error) bool {
	return errors.Is(err, ErrNoUserTokenConfigured)
}

// HasUserTokenConfigured 静态探测当前环境是否存在 User Token 配置（无网络请求，不执行刷新）。
func HasUserTokenConfigured(flagValue, configValue string) bool {
	if flagValue != "" {
		return true
	}
	if os.Getenv("FEISHU_USER_ACCESS_TOKEN") != "" {
		return true
	}
	token, err := LoadToken()
	if err == nil && token != nil && (token.AccessToken != "" || token.RefreshToken != "") {
		return true
	}
	if configValue != "" {
		return true
	}
	return false
}

// ResolveUserAccessToken 按优先级链获取 user_access_token，支持自动刷新
//
// 优先级:
//  1. flagValue（--user-access-token 参数）
//     - 若 flagValue 等于 token.json 中已过期的 access_token 且 refresh_token 仍有效，
//     自动刷新并返回新 access_token（写回 token.json）。常见场景：脚本从 token.json
//     读取 access_token 后传入 --user-access-token，本质是延伸本机身份。
//  2. FEISHU_USER_ACCESS_TOKEN 环境变量（同样支持本机身份延伸时的自动刷新）
//  3. token.json（access_token 有效直接返回；过期则用 refresh_token 刷新）
//  4. configValue（config.yaml 静态配置）
//  5. 全部为空 → 返回 ErrNoUserTokenConfigured
func ResolveUserAccessToken(flagValue, configValue, appID, appSecret, baseURL string) (string, error) {
	// 1. 命令行参数
	if flagValue != "" {
		if refreshed, ok := refreshIfStaleLocalToken(flagValue, appID, appSecret, baseURL); ok {
			return refreshed, nil
		}
		return flagValue, nil
	}

	// 2. 环境变量
	if envToken := os.Getenv("FEISHU_USER_ACCESS_TOKEN"); envToken != "" {
		if refreshed, ok := refreshIfStaleLocalToken(envToken, appID, appSecret, baseURL); ok {
			return refreshed, nil
		}
		return envToken, nil
	}

	// 3. token.json
	var tokenFileExpired bool
	token, err := LoadToken()
	if err != nil {
		return "", fmt.Errorf("读取本地 token 文件失败: %w", err)
	}
	if token != nil {
		if err := token.CheckAppMismatch(appID); err != nil {
			return "", err
		}
		if token.IsAccessTokenValid() {
			return token.AccessToken, nil
		}

		// access_token 过期，尝试刷新（跨进程锁：reload → check → refresh → commit）
		if token.IsRefreshTokenValid() {
			if err := token.RequireBoundApp(appID); err != nil {
				return "", err
			}
			logf("[自动刷新] Access Token 已过期，正在刷新...")
			newToken, refreshErr := refreshLocalTokenLocked(appID, appSecret, baseURL, false)
			if refreshErr != nil {
				logf("[自动刷新] 刷新失败: %v", refreshErr)
				return "", fmt.Errorf("自动刷新 Access Token 失败: %w", refreshErr)
			}
			logf("[自动刷新] 刷新成功，新 Token 有效期至 %s", newToken.ExpiresAt.Format("2006-01-02 15:04:05"))
			return newToken.AccessToken, nil
		}
		tokenFileExpired = true // token.json 存在但所有 token 都过期了
	}

	// 4. 配置文件
	if configValue != "" {
		return configValue, nil
	}

	// 5. 区分"从未登录"和"登录过期"
	if tokenFileExpired {
		return "", fmt.Errorf("User Access Token 已过期（access_token 和 refresh_token 均已失效）。\n" +
			"请重新登录: feishu-cli auth login")
	}
	return "", ErrNoUserTokenConfigured
}

// refreshIfStaleLocalToken 当显式传入的 token 等于 token.json 里已过期的 access_token 时，
// 触发自动刷新并写回 token.json。这是为了支持「脚本从 token.json 读 access_token 后传 flag」
// 这种常见用法——既保留显式传入 token 的契约，又解决了过期场景。
//
// 返回值:
//   - (newToken, true): 已成功刷新并保存
//   - ("", false): 不匹配本地 token，或不需要刷新，调用方应使用原始 token
func refreshIfStaleLocalToken(explicitToken, appID, appSecret, baseURL string) (string, bool) {
	local, err := LoadToken()
	if err != nil || local == nil {
		return "", false
	}
	// 必须确认 explicitToken 就是 token.json 的 access_token，否则不能擅自 refresh
	if local.AccessToken != explicitToken {
		return "", false
	}
	if err := local.CheckAppMismatch(appID); err != nil {
		return "", false
	}
	// 已经有效，不需要刷新
	if local.IsAccessTokenValid() {
		return "", false
	}
	// access 过期但 refresh 失效，无能为力
	if !local.IsRefreshTokenValid() {
		return "", false
	}
	if err := local.RequireBoundApp(appID); err != nil {
		logf("[自动刷新] %v", err)
		return "", false
	}
	logf("[自动刷新] 显式传入的 access_token 已过期且匹配本地 token.json，正在刷新...")
	newToken, refreshErr := refreshLocalTokenLocked(appID, appSecret, baseURL, false)
	if refreshErr != nil {
		logf("[自动刷新] 刷新失败: %v", refreshErr)
		return "", false
	}
	logf("[自动刷新] 刷新成功，新 Token 有效期至 %s", newToken.ExpiresAt.Format("2006-01-02 15:04:05"))
	return newToken.AccessToken, true
}

// ForceRefreshLocalToken 强制刷新 token.json 中的 access_token，
// 即使当前 access_token 仍然有效。由 `auth refresh` 子命令调用。
//
// 失败原因可能是: token.json 不存在、refresh_token 已过期、网络/服务端错误。
func ForceRefreshLocalToken(appID, appSecret, baseURL string) (*TokenStore, error) {
	local, err := LoadToken()
	if err != nil {
		return nil, fmt.Errorf("读取 token.json 失败: %w", err)
	}
	if local == nil {
		return nil, fmt.Errorf("未登录（token.json 不存在），请先 `feishu-cli auth login`")
	}
	if local.RefreshToken == "" {
		return nil, fmt.Errorf("token.json 中缺少 refresh_token，请重新 `feishu-cli auth login`")
	}
	if !local.IsRefreshTokenValid() {
		return nil, fmt.Errorf("refresh_token 已过期（%s），请重新 `feishu-cli auth login`",
			local.RefreshExpiresAt.Format("2006-01-02 15:04:05"))
	}
	if err := local.RequireBoundApp(appID); err != nil {
		return nil, err
	}
	return refreshLocalTokenLocked(appID, appSecret, baseURL, true)
}

// refreshLocalTokenLocked 在跨进程锁下 reload→check→refresh→commit。
// 写失败时保留旧 token 文件。force 为 true 时即使 access 仍有效也刷新。
func refreshLocalTokenLocked(appID, appSecret, baseURL string, force bool) (*TokenStore, error) {
	path, err := TokenPath()
	if err != nil {
		return nil, err
	}
	var result *TokenStore
	err = withTokenFileLock(path, func() error {
		current, err := LoadTokenFrom(path)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("未登录（token.json 不存在），请先 `feishu-cli auth login`")
		}
		if err := current.RequireBoundApp(appID); err != nil {
			return err
		}
		if !force && current.IsAccessTokenValid() {
			result = current
			return nil
		}
		if !current.IsRefreshTokenValid() {
			return fmt.Errorf("refresh_token 已过期，请重新 `feishu-cli auth login`")
		}
		snapshotRefresh := current.RefreshToken
		fresh, refreshErr := RefreshAccessToken(current, appID, appSecret, baseURL)
		if refreshErr != nil {
			// 可能已被另一进程消耗同一 refresh_token；reload 后若已是新一代则直接采用。
			reloaded, loadErr := LoadTokenFrom(path)
			if loadErr == nil && reloaded != nil && reloaded.RefreshToken != snapshotRefresh && reloaded.IsAccessTokenValid() {
				if bindErr := reloaded.CheckAppMismatch(appID); bindErr == nil {
					result = reloaded
					return nil
				}
			}
			return refreshErr
		}
		if err := writeTokenUnlocked(path, fresh); err != nil {
			return fmt.Errorf("刷新成功但写入 token.json 失败（原文件未改动）: %w", err)
		}
		result = fresh
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
