package client

import (
	"fmt"

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

// SubscribeDriveFile 订阅单个云文档的事件（drive.file.edit_v1 等）。
// API: POST /open-apis/drive/v1/files/:file_token/subscribe?file_type=docx
//
// 仅文档的拥有者可以对"自己拥有"的文档订阅；订阅关系持久存在、重复调用幂等。
// 服务端不支持按事件类型订阅（"暂不支持单独订阅文档维度的某类事件"），因此这里不传 event_type。
// userAccessToken 为空时回退到 Tenant Token（SDK 同时接受 user/tenant 两种令牌）。
func SubscribeDriveFile(fileToken, fileType, userAccessToken string) error {
	c, err := GetClient()
	if err != nil {
		return err
	}
	req := larkdrive.NewSubscribeFileReqBuilder().
		FileToken(fileToken).
		FileType(fileType).
		Build()
	resp, err := c.Drive.File.Subscribe(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return fmt.Errorf("订阅文档 %s 失败: %w", fileToken, err)
	}
	if !resp.Success() {
		return fmt.Errorf("订阅文档 %s 失败: code=%d, msg=%s", fileToken, resp.Code, resp.Msg)
	}
	return nil
}

// GetDriveFileSubscribeStatus 查询云文档的事件订阅状态。
// API: GET /open-apis/drive/v1/files/:file_token/subscribe?file_type=docx
// 返回是否处于订阅状态（true=已订阅，false=未订阅）。
func GetDriveFileSubscribeStatus(fileToken, fileType, userAccessToken string) (bool, error) {
	c, err := GetClient()
	if err != nil {
		return false, err
	}
	req := larkdrive.NewGetSubscribeFileReqBuilder().
		FileToken(fileToken).
		FileType(fileType).
		Build()
	resp, err := c.Drive.File.GetSubscribe(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return false, fmt.Errorf("查询订阅状态 %s 失败: %w", fileToken, err)
	}
	if !resp.Success() {
		return false, fmt.Errorf("查询订阅状态 %s 失败: code=%d, msg=%s", fileToken, resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.IsSubscribe == nil {
		return false, nil
	}
	return *resp.Data.IsSubscribe, nil
}
