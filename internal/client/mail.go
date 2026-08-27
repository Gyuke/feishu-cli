package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Mail API 基础路径
const mailBase = "/open-apis/mail/v1"

// mailboxPath 构造 mailbox 相关的 API path
// mailboxID 可以是 "me" 或具体 email 地址
func mailboxPath(mailboxID string, segments ...string) string {
	parts := make([]string, 0, 1+len(segments))
	parts = append(parts, url.PathEscape(mailboxID))
	for _, seg := range segments {
		if seg != "" {
			parts = append(parts, url.PathEscape(seg))
		}
	}
	return mailBase + "/user_mailboxes/" + strings.Join(parts, "/")
}

// callMailAPI 统一包装 mail API 调用
// method: GET/POST/PUT/DELETE
// path: API 完整路径（含 query string）
// body: 请求体（nil 表示无）
// 返回 data 字段原始 JSON
func callMailAPI(method, apiPath string, body any, userAccessToken string) (json.RawMessage, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}
	tokenType, opts := resolveTokenOpts(userAccessToken)

	var rawBody []byte
	var statusCode int

	switch method {
	case http.MethodGet:
		r, err := client.Get(Context(), apiPath, body, tokenType, opts...)
		if err != nil {
			return nil, fmt.Errorf("mail API %s %s 失败: %w", method, apiPath, err)
		}
		statusCode = r.StatusCode
		rawBody = r.RawBody
	case http.MethodPost:
		r, err := client.Post(Context(), apiPath, body, tokenType, opts...)
		if err != nil {
			return nil, fmt.Errorf("mail API %s %s 失败: %w", method, apiPath, err)
		}
		statusCode = r.StatusCode
		rawBody = r.RawBody
	case http.MethodPut:
		r, err := client.Put(Context(), apiPath, body, tokenType, opts...)
		if err != nil {
			return nil, fmt.Errorf("mail API %s %s 失败: %w", method, apiPath, err)
		}
		statusCode = r.StatusCode
		rawBody = r.RawBody
	case http.MethodDelete:
		r, err := client.Delete(Context(), apiPath, body, tokenType, opts...)
		if err != nil {
			return nil, fmt.Errorf("mail API %s %s 失败: %w", method, apiPath, err)
		}
		statusCode = r.StatusCode
		rawBody = r.RawBody
	default:
		return nil, fmt.Errorf("不支持的 HTTP 方法: %s", method)
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("mail API %s %s 失败: HTTP %d, body: %s", method, apiPath, statusCode, string(rawBody))
	}

	var apiResp struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		return nil, fmt.Errorf("mail API 解析响应失败: %w", err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("mail API 失败: code=%d, msg=%s", apiResp.Code, apiResp.Msg)
	}
	return apiResp.Data, nil
}

// MailboxProfile mailbox profile 信息
type MailboxProfile struct {
	PrimaryEmailAddress string `json:"primary_email_address"`
	UserMailboxID       string `json:"user_mailbox_id"`
	Name                string `json:"name"`
}

// GetMailboxProfile 获取 mailbox profile（用于解析当前用户邮箱地址）
// API: GET /open-apis/mail/v1/user_mailboxes/{mailbox_id}/profile
func GetMailboxProfile(mailboxID, userAccessToken string) (*MailboxProfile, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	data, err := callMailAPI(http.MethodGet, mailboxPath(mailboxID, "profile"), nil, userAccessToken)
	if err != nil {
		return nil, err
	}
	var profile MailboxProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("解析 mailbox profile 失败: %w", err)
	}
	return &profile, nil
}

// ==================== 邮件查询 ====================

// GetMailMessage 获取单封邮件
// API: GET /open-apis/mail/v1/user_mailboxes/{mailbox_id}/messages/{message_id}
// format: "full"（含 HTML） / "plain_text_full"（纯文本） / "raw"（原始 EML）
func GetMailMessage(mailboxID, messageID, format, userAccessToken string) (json.RawMessage, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	if format == "" {
		format = "full"
	}
	apiPath := mailboxPath(mailboxID, "messages", messageID) + "?format=" + url.QueryEscape(format)
	return callMailAPI(http.MethodGet, apiPath, nil, userAccessToken)
}

// BatchGetMailMessages 批量获取邮件（单批最多 20 条，自动分块并保序）
// API: POST /open-apis/mail/v1/user_mailboxes/{mailbox_id}/messages/batch_get
func BatchGetMailMessages(mailboxID string, messageIDs []string, format, userAccessToken string) (json.RawMessage, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	if format == "" {
		format = "full"
	}
	if len(messageIDs) == 0 {
		return json.Marshal(map[string]any{"messages": []any{}})
	}

	const batchSize = 20
	var allCollected []json.RawMessage
	for i := 0; i < len(messageIDs); i += batchSize {
		end := i + batchSize
		if end > len(messageIDs) {
			end = len(messageIDs)
		}
		chunk := messageIDs[i:end]
		body := map[string]any{
			"message_ids": chunk,
			"format":      format,
		}
		data, err := callMailAPI(http.MethodPost, mailboxPath(mailboxID, "messages", "batch_get"), body, userAccessToken)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("解析 batch_get 响应失败: %w", err)
		}
		allCollected = append(allCollected, resp.Messages...)
	}

	// 保证按照请求的 messageIDs 顺序保序
	type idHolder struct {
		MessageID string `json:"message_id"`
	}
	msgMap := make(map[string]json.RawMessage, len(allCollected))
	for _, raw := range allCollected {
		var holder idHolder
		if err := json.Unmarshal(raw, &holder); err == nil && holder.MessageID != "" {
			msgMap[holder.MessageID] = raw
		}
	}

	ordered := make([]json.RawMessage, 0, len(messageIDs))
	for _, id := range messageIDs {
		if raw, ok := msgMap[id]; ok {
			ordered = append(ordered, raw)
		}
	}
	if len(ordered) == 0 && len(allCollected) > 0 {
		ordered = allCollected
	}

	return json.Marshal(map[string]any{
		"messages": ordered,
	})
}

// GetMailThread 获取线程并按时间升序实际排序
// API: GET /open-apis/mail/v1/user_mailboxes/{mailbox_id}/threads/{thread_id}
func GetMailThread(mailboxID, threadID, format, userAccessToken string) (json.RawMessage, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	if format == "" {
		format = "full"
	}
	apiPath := mailboxPath(mailboxID, "threads", threadID) + "?format=" + url.QueryEscape(format)
	data, err := callMailAPI(http.MethodGet, apiPath, nil, userAccessToken)
	if err != nil {
		return nil, err
	}
	return sortThreadMessages(data)
}

func sortThreadMessages(data json.RawMessage) (json.RawMessage, error) {
	var parsed struct {
		Thread struct {
			ID          string            `json:"id,omitempty"`
			BodyPreview string            `json:"body_preview,omitempty"`
			Messages    []json.RawMessage `json:"messages,omitempty"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return data, nil
	}
	if len(parsed.Thread.Messages) <= 1 {
		return data, nil
	}
	type msgItem struct {
		raw  json.RawMessage
		date int64
	}
	items := make([]msgItem, len(parsed.Thread.Messages))
	for i, raw := range parsed.Thread.Messages {
		var d struct {
			InternalDate any `json:"internal_date"`
		}
		_ = json.Unmarshal(raw, &d)
		var dateVal int64
		switch v := d.InternalDate.(type) {
		case string:
			dateVal, _ = strconv.ParseInt(v, 10, 64)
		case float64:
			dateVal = int64(v)
		case json.Number:
			dateVal, _ = v.Int64()
		}
		items[i] = msgItem{raw: raw, date: dateVal}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].date < items[j].date
	})
	sortedMsgs := make([]json.RawMessage, len(items))
	for i, it := range items {
		sortedMsgs[i] = it.raw
	}
	parsed.Thread.Messages = sortedMsgs
	return json.Marshal(parsed)
}

// ListMailMessagesParams 邮件列表参数
type ListMailMessagesParams struct {
	MailboxID  string
	FolderID   string // INBOX / SENT / SPAM / ARCHIVED / STRANGER 或自定义 folder_id
	LabelID    string // 标签 id
	UnreadOnly bool
	PageSize   int
	PageToken  string
	AfterTime  int64 // Unix 毫秒
	BeforeTime int64 // Unix 毫秒
}

// ListMailMessages 列出邮件（按 folder/label/未读过滤；无 label 默认 INBOX）
// API: GET /open-apis/mail/v1/user_mailboxes/{mailbox_id}/messages
// 关键词搜索请使用 SearchMailMessages（走专用 /search 端点）
func ListMailMessages(params ListMailMessagesParams, userAccessToken string) (json.RawMessage, error) {
	mailboxID := params.MailboxID
	if mailboxID == "" {
		mailboxID = "me"
	}
	folderID := params.FolderID
	if folderID == "" && params.LabelID == "" {
		folderID = "INBOX"
	}
	q := url.Values{}
	if folderID != "" {
		q.Set("folder_id", folderID)
	}
	if params.LabelID != "" {
		q.Set("label_id", params.LabelID)
	}
	if params.UnreadOnly {
		q.Set("only_unread", "true")
	}
	if params.PageSize > 0 {
		q.Set("page_size", fmt.Sprintf("%d", params.PageSize))
	}
	if params.PageToken != "" {
		q.Set("page_token", params.PageToken)
	}
	if params.AfterTime > 0 {
		q.Set("after_time", fmt.Sprintf("%d", params.AfterTime))
	}
	if params.BeforeTime > 0 {
		q.Set("before_time", fmt.Sprintf("%d", params.BeforeTime))
	}
	apiPath := mailboxPath(mailboxID, "messages")
	if encoded := q.Encode(); encoded != "" {
		apiPath += "?" + encoded
	}
	return callMailAPI(http.MethodGet, apiPath, nil, userAccessToken)
}

// ==================== 草稿管理 ====================

// CreateMailDraft 创建草稿（raw EML base64url 编码）
// API: POST /open-apis/mail/v1/user_mailboxes/{mailbox_id}/drafts
// body: {"raw": "base64url_encoded_eml"}
// 返回 draft_id
func CreateMailDraft(mailboxID, rawEMLBase64URL, userAccessToken string) (string, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	data, err := callMailAPI(http.MethodPost, mailboxPath(mailboxID, "drafts"),
		map[string]any{"raw": rawEMLBase64URL}, userAccessToken)
	if err != nil {
		return "", err
	}
	return extractMailDraftID(data), nil
}

// UpdateMailDraft 更新草稿
// API: PUT /open-apis/mail/v1/user_mailboxes/{mailbox_id}/drafts/{draft_id}
func UpdateMailDraft(mailboxID, draftID, rawEMLBase64URL, userAccessToken string) error {
	if mailboxID == "" {
		mailboxID = "me"
	}
	_, err := callMailAPI(http.MethodPut, mailboxPath(mailboxID, "drafts", draftID),
		map[string]any{"raw": rawEMLBase64URL}, userAccessToken)
	return err
}

// SendMailDraft 发送草稿
// API: POST /open-apis/mail/v1/user_mailboxes/{mailbox_id}/drafts/{draft_id}/send
// 返回响应原始 data（含 message_id、thread_id 等）
func SendMailDraft(mailboxID, draftID, userAccessToken string) (json.RawMessage, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	return callMailAPI(http.MethodPost, mailboxPath(mailboxID, "drafts", draftID, "send"), nil, userAccessToken)
}

// GetMailDraftRaw 获取草稿原始 EML
// API: GET /open-apis/mail/v1/user_mailboxes/{mailbox_id}/drafts/{draft_id}?format=raw
func GetMailDraftRaw(mailboxID, draftID, userAccessToken string) (string, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	apiPath := mailboxPath(mailboxID, "drafts", draftID) + "?format=raw"
	data, err := callMailAPI(http.MethodGet, apiPath, nil, userAccessToken)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Draft struct {
			Raw string `json:"raw"`
		} `json:"draft"`
		Raw string `json:"raw"`
	}
	_ = json.Unmarshal(data, &parsed)
	if parsed.Raw != "" {
		return parsed.Raw, nil
	}
	return parsed.Draft.Raw, nil
}

func extractMailDraftID(data json.RawMessage) string {
	var parsed struct {
		DraftID string `json:"draft_id"`
		ID      string `json:"id"`
		Draft   struct {
			DraftID string `json:"draft_id"`
			ID      string `json:"id"`
		} `json:"draft"`
	}
	_ = json.Unmarshal(data, &parsed)
	if parsed.DraftID != "" {
		return parsed.DraftID
	}
	if parsed.ID != "" {
		return parsed.ID
	}
	if parsed.Draft.DraftID != "" {
		return parsed.Draft.DraftID
	}
	return parsed.Draft.ID
}

// ==================== 文件夹和标签 ====================

// SearchMailMessages 通过专用 search 端点搜索邮件
// API: POST /open-apis/mail/v1/user_mailboxes/{mailbox_id}/search?page_size=xx&page_token=yy
// body: {"query": "关键词", "filter": {"folder": ["INBOX"], "label": ["xxx"], "is_unread": true}}
// 用于 mail triage --query 的真实搜索（不同于 ListMailMessages 的列表过滤）
func SearchMailMessages(mailboxID, query string, filter map[string]any, userAccessToken string) (json.RawMessage, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	q := url.Values{}
	normalizedFilter := make(map[string]any)
	for k, v := range filter {
		switch k {
		case "page_size":
			q.Set("page_size", fmt.Sprintf("%v", v))
		case "page_token":
			if s := fmt.Sprintf("%v", v); s != "" {
				q.Set("page_token", s)
			}
		case "folder", "folder_id":
			switch val := v.(type) {
			case string:
				if val != "" {
					normalizedFilter["folder"] = []string{val}
				}
			case []string:
				normalizedFilter["folder"] = val
			case []any:
				var arr []string
				for _, item := range val {
					arr = append(arr, fmt.Sprintf("%v", item))
				}
				normalizedFilter["folder"] = arr
			default:
				normalizedFilter["folder"] = v
			}
		case "label", "label_id":
			switch val := v.(type) {
			case string:
				if val != "" {
					normalizedFilter["label"] = []string{val}
				}
			case []string:
				normalizedFilter["label"] = val
			case []any:
				var arr []string
				for _, item := range val {
					arr = append(arr, fmt.Sprintf("%v", item))
				}
				normalizedFilter["label"] = arr
			default:
				normalizedFilter["label"] = v
			}
		case "only_unread", "is_unread":
			if b, ok := v.(bool); ok {
				normalizedFilter["is_unread"] = b
			} else if s := fmt.Sprintf("%v", v); s == "true" {
				normalizedFilter["is_unread"] = true
			}
		default:
			normalizedFilter[k] = v
		}
	}
	body := map[string]any{"query": query}
	if len(normalizedFilter) > 0 {
		body["filter"] = normalizedFilter
	}
	apiPath := mailboxPath(mailboxID, "search")
	if encoded := q.Encode(); encoded != "" {
		apiPath += "?" + encoded
	}
	return callMailAPI(http.MethodPost, apiPath, body, userAccessToken)
}

// ListMailSignatures 列出邮箱签名
// API: GET /open-apis/mail/v1/user_mailboxes/{mailbox_id}/settings/signatures
// 权限: User Access Token + mail:user_mailbox:readonly
//
//	（飞书无 mail:user_mailbox.settings:read 这个 scope，settings 路径下端点复用 mailbox 读权限）
//
// 返回 data 字段原始 JSON（含 signatures 列表 + usages 使用信息）
func ListMailSignatures(mailboxID, userAccessToken string) (json.RawMessage, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	return callMailAPI(http.MethodGet, mailboxPath(mailboxID, "settings", "signatures"), nil, userAccessToken)
}

// ListMailFolders 列出邮箱文件夹
// API: GET /open-apis/mail/v1/user_mailboxes/{mailbox_id}/folders
func ListMailFolders(mailboxID, userAccessToken string) (json.RawMessage, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	return callMailAPI(http.MethodGet, mailboxPath(mailboxID, "folders"), nil, userAccessToken)
}

// ListMailLabels 列出邮箱标签
// API: GET /open-apis/mail/v1/user_mailboxes/{mailbox_id}/labels
func ListMailLabels(mailboxID, userAccessToken string) (json.RawMessage, error) {
	if mailboxID == "" {
		mailboxID = "me"
	}
	return callMailAPI(http.MethodGet, mailboxPath(mailboxID, "labels"), nil, userAccessToken)
}
