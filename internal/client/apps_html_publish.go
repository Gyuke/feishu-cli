package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 妙搭 html-publish 业务错误码（后端 owns，文档更新时同步）。
const (
	sparkErrCodeBuildFailed     = 90001     // 上传成功但服务端构建失败
	sparkErrCodeAppNotFound     = 90002     // app_id 不存在或无权访问（旧码）
	sparkErrCodeAppTypeRejected = 400000059 // release-create 拒绝不支持的 app_type
	sparkErrCodeAppNotExist     = 400002577 // GET app 应用不存在
)

// htmlPublishableAppTypes 是 html-publish 三段协议能部署的 app_type 白名单。
// 服务端返回大写（HTML / MODERN_HTML），查询时归一成小写再比。
var htmlPublishableAppTypes = map[string]bool{
	"html":        true,
	"modern_html": true,
}

// tosHTTPClient 专用于 TOS 预签名 PUT：与飞书 SDK 隔离，避免带上 Authorization。
// 测试可替换。
var tosHTTPClient = &http.Client{Timeout: 2 * time.Minute}

// SparkHTMLPublish 按官方三段协议发布 HTML tar.gz，成功只白名单返回 release_id。
//
//  1. GET /apps/{id} 校验 app_type 为 html / modern_html
//  2. GET /apps/{id}/pre_release 解析 kvs 中的 upload_url / tos_path
//  3. 对预签名 URL PUT tar.gz（不携带飞书 Authorization）
//  4. POST /apps/{id}/releases，body 为 {"tos_path": ...}
//
// 任一步失败即中止，不再继续后续网络调用。
func SparkHTMLPublish(appID string, tarball []byte, userAccessToken string) (map[string]any, error) {
	if err := ensureHTMLPublishable(appID, userAccessToken); err != nil {
		return nil, err
	}

	preData, err := SparkCall("GET", SparkPreReleasePath(appID), nil, nil, userAccessToken)
	if err != nil {
		return nil, wrapSparkHTMLPublishErr(err, appID)
	}
	uploadURL, tosPath, err := parsePreReleaseKVs(preData)
	if err != nil {
		return nil, err
	}

	if err := putPresignedTOS(uploadURL, tarball); err != nil {
		return nil, err
	}

	releaseData, err := SparkCall("POST", SparkReleaseCreatePath(appID), nil, map[string]any{
		"tos_path": tosPath,
	}, userAccessToken)
	if err != nil {
		return nil, wrapSparkReleaseCreateErr(err, appID)
	}

	rid := sparkString(releaseData["release_id"])
	if rid == "" {
		return nil, fmt.Errorf("妙搭 release-create 未返回 release_id\n稍后重试 `feishu-cli apps html-publish --app-id %s --path <path>`", appID)
	}
	return map[string]any{"release_id": rid}, nil
}

// ensureHTMLPublishable 在打包上传前校验目标应用类型；解析失败也给出可恢复 hint。
func ensureHTMLPublishable(appID, userAccessToken string) error {
	appType, err := sparkQueryAppType(appID, userAccessToken)
	if err != nil {
		return wrapSparkHTMLPublishErr(err, appID)
	}
	if htmlPublishableAppTypes[appType] {
		return nil
	}
	return fmt.Errorf("应用 %s 的 app_type 是 %q，html-publish 仅支持 html 与 modern_html\n该类型走各自的构建/发布链，不能用静态 HTML 打包发布", appID, appType)
}

// sparkQueryAppType GET /apps/{id} 读取 app_type，归一为小写。
func sparkQueryAppType(appID, userAccessToken string) (string, error) {
	data, err := SparkCall("GET", SparkAppGetPath(appID), nil, nil, userAccessToken)
	if err != nil {
		return "", err
	}
	appRaw, _ := data["app"].(map[string]any)
	if appRaw == nil {
		return "", fmt.Errorf("查询应用类型失败: 响应缺少 app 对象")
	}
	appType := strings.ToLower(sparkString(appRaw["app_type"]))
	if appType == "" {
		return "", fmt.Errorf("查询应用类型失败: 响应缺少 app_type")
	}
	return appType, nil
}

// parsePreReleaseKVs 从 pre_release 的 data.kvs 取出 upload_url 与 tos_path。
func parsePreReleaseKVs(data map[string]any) (uploadURL, tosPath string, err error) {
	raw, ok := data["kvs"]
	if !ok {
		return "", "", fmt.Errorf("妙搭 pre_release 未返回 kvs\n稍后重试 html-publish；若持续失败，核对 app_id 与 spark:app:write")
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return "", "", fmt.Errorf("妙搭 pre_release 返回的 kvs 为空\n稍后重试 html-publish；若持续失败，核对 app_id 与 spark:app:write")
	}
	kvm := make(map[string]string, len(list))
	for _, item := range list {
		kv, _ := item.(map[string]any)
		if kv == nil {
			continue
		}
		k := sparkString(kv["key"])
		v := sparkString(kv["value"])
		if k != "" {
			kvm[k] = v
		}
	}
	uploadURL = strings.TrimSpace(kvm["upload_url"])
	tosPath = strings.TrimSpace(kvm["tos_path"])
	if uploadURL == "" || tosPath == "" {
		return "", "", fmt.Errorf("妙搭 pre_release kvs 缺少 upload_url 或 tos_path\n稍后重试 html-publish 以重新获取预签名 URL")
	}
	return uploadURL, tosPath, nil
}

// putPresignedTOS 把 tar.gz 直传到 TOS 预签名 URL。
// 必须使用独立 http.Client，禁止走飞书 SDK（SDK 会注入 Authorization）。
func putPresignedTOS(uploadURL string, tarball []byte) error {
	if err := validateTOSUploadURL(uploadURL); err != nil {
		return err
	}
	ctx := ContextWithTimeout(2 * time.Minute)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(tarball))
	if err != nil {
		return fmt.Errorf("构造 TOS 上传请求失败: %w\n重新执行 html-publish 以获取新的预签名 URL", err)
	}
	req.ContentLength = int64(len(tarball))
	req.Header.Set("Content-Type", "application/gzip")
	// 预签名 URL 自带签名；飞书 token 外送到 TOS 会泄露凭证。
	req.Header.Del("Authorization")

	resp, err := tosHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("TOS 预签名上传失败: %w\n网络瞬时故障时稍后重试同一条 html-publish（会重新 GET pre_release 换新预签名 URL）", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("TOS 预签名上传失败: HTTP %d\n服务端瞬时故障，稍后重试同一条 `feishu-cli apps html-publish`（会重新 GET pre_release 换新预签名 URL）", resp.StatusCode)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("TOS 预签名上传失败: HTTP %d\n预签名 URL 可能过期或签名不匹配；不要把飞书 Authorization 带到 TOS；重新执行 html-publish 获取新的 upload_url", resp.StatusCode)
	}
	return nil
}

func validateTOSUploadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("pre_release 返回的 upload_url 无效\n重新执行 html-publish 以获取新的预签名 URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("upload_url 只接受 http/https（得到 %s）\n重新执行 html-publish 以获取新的预签名 URL", u.Scheme)
	}
	return nil
}

func wrapSparkHTMLPublishErr(err error, appID string) error {
	if err == nil {
		return nil
	}
	if HasAPICode(err, sparkErrCodeAppNotFound) || HasAPICode(err, sparkErrCodeAppNotExist) {
		return fmt.Errorf("%w\n%s", err, sparkHTMLPublishHint(sparkErrCodeAppNotExist))
	}
	if hint := sparkHTMLPublishHintFromErr(err); hint != "" {
		return fmt.Errorf("%w\n%s", err, hint)
	}
	return fmt.Errorf("%w\n检查 app_id %s 与 spark:app:read / spark:app:write；用 --dry-run 核对打包计划后重试", err, appID)
}

func wrapSparkReleaseCreateErr(err error, appID string) error {
	if err == nil {
		return nil
	}
	if HasAPICode(err, sparkErrCodeAppTypeRejected) || strings.Contains(strings.ToLower(err.Error()), "app_type") {
		return fmt.Errorf("%w\n该 app_type 不能通过 html-publish 发布，仅 html / modern_html 支持静态 HTML 打包", err)
	}
	if hint := sparkHTMLPublishHintFromErr(err); hint != "" {
		return fmt.Errorf("%w\n%s", err, hint)
	}
	return fmt.Errorf("%w\n用 `feishu-cli apps html-publish --app-id %s --path <path> --dry-run` 检查打包清单后重试", err, appID)
}

func sparkHTMLPublishHintFromErr(err error) string {
	for _, code := range []int{
		sparkErrCodeBuildFailed,
		sparkErrCodeAppNotFound,
		sparkErrCodeAppNotExist,
		sparkErrCodeAppTypeRejected,
	} {
		if HasAPICode(err, code) {
			return sparkHTMLPublishHint(code)
		}
	}
	return ""
}

func sparkHTMLPublishHint(code int) string {
	switch code {
	case sparkErrCodeBuildFailed:
		return "构建失败：用 `feishu-cli apps html-publish --app-id <id> --path <path> --dry-run` 检查打包文件清单"
	case sparkErrCodeAppNotFound, sparkErrCodeAppNotExist:
		return "应用不存在或无权访问；确认 app_id（从妙搭应用链接 https://miaoda.feishu.cn/app/app_xxx 的 /app/ 后提取，或直接给 app_xxx 字符串）"
	case sparkErrCodeAppTypeRejected:
		return "该 app_type 不能通过 html-publish 发布，仅 html / modern_html 支持静态 HTML 打包"
	default:
		return ""
	}
}

func sparkString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return strings.TrimSpace(t.String())
	default:
		return ""
	}
}
