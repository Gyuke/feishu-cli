package client

import (
	"fmt"
	"net/http"

	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

// DriveFileTypeFromObjType 把 wiki 节点的 obj_type 映射为 drive 订阅接口的 file_type。
// 订阅端点(file_type 查询参数)接受的类型：doc/docx/sheet/bitable/mindnote/slides/file。
func DriveFileTypeFromObjType(objType string) string {
	switch objType {
	case "doc", "docx", "sheet", "bitable", "mindnote", "slides", "file":
		return objType
	default:
		return "docx"
	}
}

// subscribeRetryCfg 统一给订阅/回查接口使用的重试策略。
// 两个端点都有频率限制（回查 GET 实测 ~1 QPS，返回 99991400）；
// RetryOnRateLimit=true 让限流不计入 MaxRetries，按 x-ogw-ratelimit-reset 退避重试。
func subscribeRetryCfg() RetryConfig {
	return RetryConfig{
		MaxRetries:       2,
		MaxTotalAttempts: 8,
		RetryOnRateLimit: true,
	}
}

// SubscribeDriveFile 订阅单个云文档的事件（drive.file.edit_v1 等）。
// API: POST /open-apis/drive/v1/files/:file_token/subscribe?file_type=docx
//
// 仅文档的拥有者可以对"自己拥有"的文档订阅；订阅关系持久存在、重复调用幂等。
// 服务端不支持按事件类型订阅（"暂不支持单独订阅文档维度的某类事件"），因此这里不传 event_type。
// userAccessToken 为空时回退到 Tenant Token（SDK 同时接受 user/tenant 两种令牌）。
// 自动处理 429/99991400 限流与可重试的瞬时错误。
func SubscribeDriveFile(fileToken, fileType, userAccessToken string) error {
	c, err := GetClient()
	if err != nil {
		return err
	}
	req := larkdrive.NewSubscribeFileReqBuilder().
		FileToken(fileToken).
		FileType(fileType).
		Build()

	result := DoVoidWithRetry(func() (http.Header, error) {
		resp, err := c.Drive.File.Subscribe(Context(), req, UserTokenOption(userAccessToken)...)
		if err != nil {
			return nil, fmt.Errorf("订阅文档 %s 失败: %w", fileToken, err)
		}
		headers := resp.Header
		if !resp.Success() {
			return headers, fmt.Errorf("订阅文档 %s 失败: code=%d, msg=%s", fileToken, resp.Code, resp.Msg)
		}
		return headers, nil
	}, subscribeRetryCfg())
	if result.Err != nil {
		return result.Err
	}
	return nil
}

// GetDriveFileSubscribeStatus 查询云文档的事件订阅状态。
// API: GET /open-apis/drive/v1/files/:file_token/subscribe?file_type=docx
// 返回是否处于订阅状态（true=已订阅，false=未订阅）。自动处理限流重试。
func GetDriveFileSubscribeStatus(fileToken, fileType, userAccessToken string) (bool, error) {
	c, err := GetClient()
	if err != nil {
		return false, err
	}
	req := larkdrive.NewGetSubscribeFileReqBuilder().
		FileToken(fileToken).
		FileType(fileType).
		Build()

	result := DoWithRetry(func() (bool, http.Header, error) {
		resp, err := c.Drive.File.GetSubscribe(Context(), req, UserTokenOption(userAccessToken)...)
		if err != nil {
			return false, nil, fmt.Errorf("查询订阅状态 %s 失败: %w", fileToken, err)
		}
		headers := resp.Header
		if !resp.Success() {
			return false, headers, fmt.Errorf("查询订阅状态 %s 失败: code=%d, msg=%s", fileToken, resp.Code, resp.Msg)
		}
		if resp.Data == nil || resp.Data.IsSubscribe == nil {
			return false, headers, nil
		}
		return *resp.Data.IsSubscribe, headers, nil
	}, subscribeRetryCfg())
	if result.Err != nil {
		return false, result.Err
	}
	return result.Value, nil
}
