package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMailSearch_QueryAndFilterContract 验证 search 的 page 参数放 query，filter 规范化
func TestMailSearch_QueryAndFilterContract(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"msg_1"}],"has_more":false}}`)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	filter := map[string]any{
		"folder_id":   "INBOX",
		"label_id":    "LBL_WORK",
		"only_unread": true,
		"page_size":   15,
		"page_token":  "pt_123",
	}

	res, err := SearchMailMessages("me", "测试邮件", filter, "u-test-token")
	if err != nil {
		t.Fatalf("SearchMailMessages 失败: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("返回数据为空")
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/open-apis/mail/v1/user_mailboxes/me/search" {
		t.Errorf("path = %s, want /open-apis/mail/v1/user_mailboxes/me/search", gotPath)
	}

	// 验证 query 参数包含 page_size 和 page_token
	if !strings.Contains(gotQuery, "page_size=15") {
		t.Errorf("query %q 未包含 page_size=15", gotQuery)
	}
	if !strings.Contains(gotQuery, "page_token=pt_123") {
		t.Errorf("query %q 未包含 page_token=pt_123", gotQuery)
	}

	// 验证 body 包含 query 和规范化的 filter
	if gotBody["query"] != "测试邮件" {
		t.Errorf("body.query = %v, want '测试邮件'", gotBody["query"])
	}
	bodyFilter, ok := gotBody["filter"].(map[string]any)
	if !ok {
		t.Fatalf("body.filter 缺失或格式不正确: %v", gotBody)
	}

	// folder 应为 []any{"INBOX"}
	folders, ok := bodyFilter["folder"].([]any)
	if !ok || len(folders) != 1 || folders[0] != "INBOX" {
		t.Errorf("filter.folder = %v, want ['INBOX']", bodyFilter["folder"])
	}
	// label 应为 []any{"LBL_WORK"}
	labels, ok := bodyFilter["label"].([]any)
	if !ok || len(labels) != 1 || labels[0] != "LBL_WORK" {
		t.Errorf("filter.label = %v, want ['LBL_WORK']", bodyFilter["label"])
	}
	// is_unread 应为 true
	if isUnread, ok := bodyFilter["is_unread"].(bool); !ok || !isUnread {
		t.Errorf("filter.is_unread = %v, want true", bodyFilter["is_unread"])
	}
	// filter 内部不应再残留 page_size / page_token
	if _, exists := bodyFilter["page_size"]; exists {
		t.Errorf("filter 内部不应包含 page_size")
	}
	if _, exists := bodyFilter["page_token"]; exists {
		t.Errorf("filter 内部不应包含 page_token")
	}
}

// TestMailList_DefaultInboxWithoutLabel 验证无 label 且无 folder 时默认 INBOX
func TestMailList_DefaultInboxWithoutLabel(t *testing.T) {
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[],"has_more":false}}`)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	// 1. 无 folder_id 无 label_id → 默认 folder_id=INBOX
	_, err := ListMailMessages(ListMailMessagesParams{
		MailboxID: "me",
	}, "u-test-token")
	if err != nil {
		t.Fatalf("ListMailMessages 失败: %v", err)
	}
	if !strings.Contains(gotQuery, "folder_id=INBOX") {
		t.Errorf("无 label 默认 folder_id=INBOX，got query: %s", gotQuery)
	}

	// 2. 有 label_id 时不自动加 folder_id=INBOX
	gotQuery = ""
	_, err = ListMailMessages(ListMailMessagesParams{
		MailboxID: "me",
		LabelID:   "LBL_IMPORTANT",
	}, "u-test-token")
	if err != nil {
		t.Fatalf("ListMailMessages 失败: %v", err)
	}
	if strings.Contains(gotQuery, "folder_id=INBOX") {
		t.Errorf("有 label 时不应自动注入 folder_id=INBOX，got query: %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "label_id=LBL_IMPORTANT") {
		t.Errorf("query 应包含 label_id=LBL_IMPORTANT，got query: %s", gotQuery)
	}
}

// TestMailBatchGet_Chunk20AndKeepOrder 验证 batch_get 超过 20 条时自动分块且保持输入顺序
func TestMailBatchGet_Chunk20AndKeepOrder(t *testing.T) {
	callCount := 0
	var requestedBatches [][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			MessageIDs []string `json:"message_ids"`
			Format     string   `json:"format"`
		}
		_ = json.Unmarshal(raw, &body)
		requestedBatches = append(requestedBatches, body.MessageIDs)

		// 构造返回的 messages，故意将顺序倒序返回，以测试客户端保序逻辑
		var msgs []map[string]any
		for i := len(body.MessageIDs) - 1; i >= 0; i-- {
			id := body.MessageIDs[i]
			msgs = append(msgs, map[string]any{
				"message_id": id,
				"subject":    "Subject of " + id,
			})
		}
		respData := map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"messages": msgs,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respData)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	// 生成 45 个 IDs (需分成 20, 20, 5 三批)
	var inputIDs []string
	for i := 1; i <= 45; i++ {
		inputIDs = append(inputIDs, fmt.Sprintf("msg_%02d", i))
	}

	data, err := BatchGetMailMessages("me", inputIDs, "full", "u-test-token")
	if err != nil {
		t.Fatalf("BatchGetMailMessages 失败: %v", err)
	}

	if callCount != 3 {
		t.Errorf("45 条 ID 应分 3 批调用，实际调用了 %d 次", callCount)
	}
	if len(requestedBatches) != 3 {
		t.Fatalf("requestedBatches 数量 = %d, want 3", len(requestedBatches))
	}
	if len(requestedBatches[0]) != 20 || len(requestedBatches[1]) != 20 || len(requestedBatches[2]) != 5 {
		t.Errorf("分块大小不正确: %d, %d, %d (want 20, 20, 5)",
			len(requestedBatches[0]), len(requestedBatches[1]), len(requestedBatches[2]))
	}

	var result struct {
		Messages []struct {
			MessageID string `json:"message_id"`
			Subject   string `json:"subject"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析聚合结果失败: %v", err)
	}

	if len(result.Messages) != 45 {
		t.Fatalf("聚合后 messages 数量 = %d, want 45", len(result.Messages))
	}

	// 验证保序：必须严格等于 inputIDs 的顺序
	for i, m := range result.Messages {
		if m.MessageID != inputIDs[i] {
			t.Errorf("第 %d 个 message_id = %s, want %s (保序失败)", i, m.MessageID, inputIDs[i])
		}
	}
}

// TestMailThread_SortMessagesByDate 验证 thread 获取后按 internal_date 实际升序排序
func TestMailThread_SortMessagesByDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 返回乱序的 thread messages
		respData := map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"thread": map[string]any{
					"id": "th_123",
					"messages": []map[string]any{
						{"message_id": "m3", "internal_date": "1682379000000", "subject": "Third"},
						{"message_id": "m1", "internal_date": "1682377000000", "subject": "First"},
						{"message_id": "m2", "internal_date": "1682378000000", "subject": "Second"},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respData)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	data, err := GetMailThread("me", "th_123", "full", "u-test-token")
	if err != nil {
		t.Fatalf("GetMailThread 失败: %v", err)
	}

	var parsed struct {
		Thread struct {
			ID       string `json:"id"`
			Messages []struct {
				MessageID    string `json:"message_id"`
				InternalDate string `json:"internal_date"`
			} `json:"messages"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("解析 thread 响应失败: %v", err)
	}

	if len(parsed.Thread.Messages) != 3 {
		t.Fatalf("thread messages 数量 = %d, want 3", len(parsed.Thread.Messages))
	}
	// 期望排序为 m1 (1682377000000) -> m2 (1682378000000) -> m3 (1682379000000)
	wantOrder := []string{"m1", "m2", "m3"}
	for i, msg := range parsed.Thread.Messages {
		if msg.MessageID != wantOrder[i] {
			t.Errorf("第 %d 个 message = %s, want %s (未按时间正确排序)", i, msg.MessageID, wantOrder[i])
		}
	}
}
