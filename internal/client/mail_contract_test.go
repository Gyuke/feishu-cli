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

// TestMailSearch_QueryAndFilterContract 验证 search 的 page 参数放 query，系统标签迁移到 folder，无额外列表请求
func TestMailSearch_QueryAndFilterContract(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotBody map[string]any
	listFolderCalled := false
	listLabelCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/mail/v1/user_mailboxes/me/folders":
			listFolderCalled = true
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[]}}`)
		case "/open-apis/mail/v1/user_mailboxes/me/labels":
			listLabelCalled = true
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[]}}`)
		case "/open-apis/mail/v1/user_mailboxes/me/search":
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"msg_1"}],"has_more":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	// INBOX 属于系统 folder；IMPORTANT 属于系统 label（必须迁移到 filter.folder 值为 priority）
	filter := map[string]any{
		"folder_id":   "INBOX",
		"label_id":    "IMPORTANT",
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

	// 纯系统值应由本地解析，无需拉取 folders/labels 列表
	if listFolderCalled {
		t.Error("系统文件夹不应触发 ListMailFolders 请求")
	}
	if listLabelCalled {
		t.Error("系统标签迁移不应触发 ListMailLabels 请求")
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

	// INBOX 和 IMPORTANT 都应在 folder 中（inbox 和 priority）
	folders, ok := bodyFilter["folder"].([]any)
	if !ok || len(folders) != 2 {
		t.Fatalf("filter.folder = %v, want 2 items", bodyFilter["folder"])
	}
	fSet := map[string]bool{}
	for _, f := range folders {
		fSet[fmt.Sprintf("%v", f)] = true
	}
	if !fSet["inbox"] || !fSet["priority"] {
		t.Errorf("filter.folder = %v, want containing inbox and priority", bodyFilter["folder"])
	}
	// IMPORTANT 迁移到 folder 后，label 应被清除或不包含 priority
	if _, exists := bodyFilter["label"]; exists {
		t.Errorf("filter.label 应为空/被清除，got: %v", bodyFilter["label"])
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

// TestMailThread_PreserveFullJSONFields 验证 thread 排序时完整保留顶层和 thread 内部的所有未知/额外字段
func TestMailThread_PreserveFullJSONFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respData := map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"top_level_sentinel": "sentinel_value_top",
				"thread": map[string]any{
					"id":                "th_123",
					"subject":           "sentinel_subject_123",
					"participants":      []string{"p1@example.com", "p2@example.com"},
					"body_preview":      "preview text",
					"custom_meta_field": map[string]any{"key": "val"},
					"messages": []map[string]any{
						{
							"message_id":         "m2",
							"internal_date":      "1682378000000",
							"subject":            "Second",
							"custom_msg_payload": "payload_2",
						},
						{
							"message_id":         "m1",
							"internal_date":      "1682377000000",
							"subject":            "First",
							"custom_msg_payload": "payload_1",
						},
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

	var rawMap map[string]any
	if err := json.Unmarshal(data, &rawMap); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	// 验证顶层字段未丢失
	if rawMap["top_level_sentinel"] != "sentinel_value_top" {
		t.Errorf("top_level_sentinel 被丢弃: %v", rawMap["top_level_sentinel"])
	}

	threadMap, ok := rawMap["thread"].(map[string]any)
	if !ok {
		t.Fatalf("thread 字段缺失或类型不对: %v", rawMap)
	}

	// 验证 thread 内部的非标准/未知字段未丢失
	if threadMap["subject"] != "sentinel_subject_123" {
		t.Errorf("thread.subject 被丢弃: %v", threadMap["subject"])
	}
	participants, ok := threadMap["participants"].([]any)
	if !ok || len(participants) != 2 {
		t.Errorf("thread.participants 被丢弃: %v", threadMap["participants"])
	}
	if threadMap["body_preview"] != "preview text" {
		t.Errorf("thread.body_preview 被丢弃: %v", threadMap["body_preview"])
	}
	if threadMap["custom_meta_field"] == nil {
		t.Errorf("thread.custom_meta_field 被丢弃")
	}

	// 验证 messages 已按时间升序排好，且每条 message 内部的未知字段也保留
	msgs, ok := threadMap["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages 列表异常: %v", threadMap["messages"])
	}
	m1 := msgs[0].(map[string]any)
	m2 := msgs[1].(map[string]any)
	if m1["message_id"] != "m1" || m1["custom_msg_payload"] != "payload_1" {
		t.Errorf("m1 顺序或内部字段异常: %v", m1)
	}
	if m2["message_id"] != "m2" || m2["custom_msg_payload"] != "payload_2" {
		t.Errorf("m2 顺序或内部字段异常: %v", m2)
	}
}

// TestMailThread_PreserveBigIntGreaterThan2Pow53 验证 thread 排序时使用 UseNumber 保持 >2^53 大整数精度
func TestMailThread_PreserveBigIntGreaterThan2Pow53(t *testing.T) {
	const bigInt1 = `9007199254740993` // 2^53 + 1, float64 会丢失精度变为 9007199254740992
	const bigInt2 = `17300000000000001`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respJSON := fmt.Sprintf(`{
			"code": 0,
			"msg": "ok",
			"data": {
				"custom_big_int": %s,
				"thread": {
					"id": "th_big",
					"thread_snowflake_id": %s,
					"messages": [
						{"message_id": "m2", "internal_date": "1682378000000", "msg_big_id": %s},
						{"message_id": "m1", "internal_date": "1682377000000", "msg_big_id": %s}
					]
				}
			}
		}`, bigInt1, bigInt2, bigInt1, bigInt2)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respJSON)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	data, err := GetMailThread("me", "th_big", "full", "u-test-token")
	if err != nil {
		t.Fatalf("GetMailThread 失败: %v", err)
	}

	rawStr := string(data)
	// 验证字符串输出中依然精确包含原始大整数字面量，未被转成浮点或科学计数法
	if !strings.Contains(rawStr, bigInt1) {
		t.Errorf("大整数 %s 在输出中丢失/精度损坏: %s", bigInt1, rawStr)
	}
	if !strings.Contains(rawStr, bigInt2) {
		t.Errorf("大整数 %s 在输出中丢失/精度损坏: %s", bigInt2, rawStr)
	}
}

// TestMailSearch_FolderNameAndLabelNameMapping 验证 SearchMailMessages 对系统别名和自定义 ID 的映射及单次拉取复用
func TestMailSearch_FolderNameAndLabelNameMapping(t *testing.T) {
	var gotSearchBody map[string]any
	var gotSearchQuery string
	folderCallCount := 0
	labelCallCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/mail/v1/user_mailboxes/me/folders":
			folderCallCount++
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"fld_custom_001","name":"自定义工作文件夹"},{"id":"fld_custom_002","name":"归档旧项目"}]}}`)
		case "/open-apis/mail/v1/user_mailboxes/me/labels":
			labelCallCount++
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"lbl_custom_001","name":"紧急项目"},{"id":"lbl_custom_002","name":"待跟进"}]}}`)
		case "/open-apis/mail/v1/user_mailboxes/me/search":
			gotSearchQuery = r.URL.RawQuery
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotSearchBody)
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	// 1. 系统别名映射测试：INBOX → inbox, IMPORTANT → priority（迁移至 folder）
	filter1 := map[string]any{
		"folder":    "INBOX",
		"label":     "IMPORTANT",
		"page_size": 10,
	}
	_, err := SearchMailMessages("me", "test", filter1, "u-test-token")
	if err != nil {
		t.Fatalf("SearchMailMessages error: %v", err)
	}
	if !strings.Contains(gotSearchQuery, "page_size=10") {
		t.Errorf("query 应包含 page_size=10, got: %s", gotSearchQuery)
	}
	f1, _ := gotSearchBody["filter"].(map[string]any)
	if folders, ok := f1["folder"].([]any); !ok || len(folders) != 2 || folders[0] != "inbox" || folders[1] != "priority" {
		t.Errorf("INBOX/IMPORTANT 映射 = %v, want ['inbox', 'priority']", f1["folder"])
	}
	if _, exists := f1["label"]; exists {
		t.Errorf("IMPORTANT 迁移后 label 应为空，got: %v", f1["label"])
	}

	// 2. 自定义 ID 解析与单次拉取复用测试：
	// 数组包含 2 个自定义 folder ID 和 2 个自定义 label ID，验证 folders 和 labels 各只拉取 1 次
	gotSearchBody = nil
	folderCallCount = 0
	labelCallCount = 0
	filter2 := map[string]any{
		"folder": []string{"fld_custom_001", "fld_custom_002"},
		"label":  []string{"lbl_custom_001", "lbl_custom_002"},
	}
	_, err = SearchMailMessages("me", "test2", filter2, "u-test-token")
	if err != nil {
		t.Fatalf("SearchMailMessages error: %v", err)
	}
	if folderCallCount != 1 {
		t.Errorf("一次 search 中多 folder ID 解析只应调用 1 次 ListMailFolders，实际调用了 %d 次", folderCallCount)
	}
	if labelCallCount != 1 {
		t.Errorf("一次 search 中多 label ID 解析只应调用 1 次 ListMailLabels，实际调用了 %d 次", labelCallCount)
	}
	f2, _ := gotSearchBody["filter"].(map[string]any)
	if folders, ok := f2["folder"].([]any); !ok || len(folders) != 2 || folders[0] != "自定义工作文件夹" || folders[1] != "归档旧项目" {
		t.Errorf("custom folders 映射 = %v, want ['自定义工作文件夹', '归档旧项目']", f2["folder"])
	}
	if labels, ok := f2["label"].([]any); !ok || len(labels) != 2 || labels[0] != "紧急项目" || labels[1] != "待跟进" {
		t.Errorf("custom labels 映射 = %v, want ['紧急项目', '待跟进']", f2["label"])
	}
}

// TestMailSearch_FailClosedOnErrors 验证未找到 ID、损坏响应、同名歧义和接口错误均 fail-closed 且不发起 POST /search
func TestMailSearch_FailClosedOnErrors(t *testing.T) {
	searchCalled := false
	handlerMode := "not_found"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/mail/v1/user_mailboxes/me/folders":
			switch handlerMode {
			case "not_found":
				_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"fld_1","name":"Folder1"}]}}`)
			case "corrupt":
				_, _ = io.WriteString(w, `invalid_json_corrupt{`)
			case "ambiguous":
				_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[{"id":"fld_1","name":"DupName"},{"id":"fld_2","name":"DupName"}]}}`)
			case "api_error":
				_, _ = io.WriteString(w, `{"code":99991663,"msg":"permission denied","data":{}}`)
			}
		case "/open-apis/mail/v1/user_mailboxes/me/search":
			searchCalled = true
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"items":[]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	// 1. 未找到自定义 ID：fail-closed
	searchCalled = false
	handlerMode = "not_found"
	_, err := SearchMailMessages("me", "q", map[string]any{"folder": "not_exist_fld_id"}, "u-test-token")
	if err == nil {
		t.Fatal("未找到自定义 ID 应返回错误")
	}
	if !strings.Contains(err.Error(), "未找到文件夹") {
		t.Errorf("error = %v, want mentioning 未找到文件夹", err)
	}
	if searchCalled {
		t.Error("解析失败时不得继续发起 POST /search 请求")
	}

	// 2. 损坏响应：fail-closed
	searchCalled = false
	handlerMode = "corrupt"
	_, err = SearchMailMessages("me", "q", map[string]any{"folder": "fld_xyz"}, "u-test-token")
	if err == nil {
		t.Fatal("损坏响应应返回错误")
	}
	if searchCalled {
		t.Error("解析失败时不得继续发起 POST /search 请求")
	}

	// 3. 名称歧义（传了重复的自定义名称）：fail-closed
	searchCalled = false
	handlerMode = "ambiguous"
	_, err = SearchMailMessages("me", "q", map[string]any{"folder": "DupName"}, "u-test-token")
	if err == nil {
		t.Fatal("名称歧义应返回错误")
	}
	if !strings.Contains(err.Error(), "歧义") {
		t.Errorf("error = %v, want mentioning 歧义", err)
	}
	if searchCalled {
		t.Error("解析失败时不得继续发起 POST /search 请求")
	}

	// 4. List API 业务错误：fail-closed
	searchCalled = false
	handlerMode = "api_error"
	_, err = SearchMailMessages("me", "q", map[string]any{"folder": "fld_xyz"}, "u-test-token")
	if err == nil {
		t.Fatal("List API 业务错误应返回错误")
	}
	if searchCalled {
		t.Error("解析失败时不得继续发起 POST /search 请求")
	}
}

// TestMailBatchGet_ZeroItems 验证 0 项请求返回 total=0
func TestMailBatchGet_ZeroItems(t *testing.T) {
	setupTestConfig(t, "http://127.0.0.1:9999")

	data, err := BatchGetMailMessages("me", []string{}, "full", "u-test-token")
	if err != nil {
		t.Fatalf("BatchGetMailMessages error: %v", err)
	}

	var res struct {
		Messages []any `json:"messages"`
		Total    int   `json:"total"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(res.Messages) != 0 || res.Total != 0 {
		t.Errorf("got messages=%d, total=%d, want 0, 0", len(res.Messages), res.Total)
	}
}

// TestMailBatchGet_21PlusPartialAndDuplicates 验证 21+ 数量、部分缺失 ID（返回 unavailable_message_ids）和重复 ID 确定性处理
func TestMailBatchGet_21PlusPartialAndDuplicates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			MessageIDs []string `json:"message_ids"`
		}
		_ = json.Unmarshal(raw, &body)

		var msgs []map[string]any
		for _, id := range body.MessageIDs {
			// 模拟服务端只识别已知 ID，不返回以 "missing_" 开头的 ID
			if !strings.HasPrefix(id, "missing_") {
				msgs = append(msgs, map[string]any{
					"message_id": id,
					"subject":    "Subject of " + id,
				})
			}
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

	// 输入 25 个 ID，包含存在的 ID、重复的 ID 和缺失的 ID
	inputIDs := []string{
		"m01", "m02", "missing_01", "m03", "m01", // 重复 m01
		"m04", "m05", "m06", "m07", "m08",
		"m09", "m10", "m11", "missing_02", "m12",
		"m13", "m14", "m15", "m16", "m17",
		"m18", "m19", "m20", "m21", "m02", // 重复 m02，总数 25 > 20
	}

	data, err := BatchGetMailMessages("me", inputIDs, "full", "u-test-token")
	if err != nil {
		t.Fatalf("BatchGetMailMessages 失败: %v", err)
	}

	var result struct {
		Messages []struct {
			MessageID string `json:"message_id"`
			Subject   string `json:"subject"`
		} `json:"messages"`
		UnavailableMessageIDs []string `json:"unavailable_message_ids"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	// 验证 unavailable_message_ids 正确收集了 missing_01 和 missing_02
	if len(result.UnavailableMessageIDs) != 2 {
		t.Fatalf("unavailable_message_ids = %v, want 2 items", result.UnavailableMessageIDs)
	}
	if result.UnavailableMessageIDs[0] != "missing_01" || result.UnavailableMessageIDs[1] != "missing_02" {
		t.Errorf("unavailable_message_ids = %v, want ['missing_01', 'missing_02']", result.UnavailableMessageIDs)
	}

	// 验证命中的 23 个 message 严格按照 inputIDs 中的相对顺序排列，且重复项确定性出现
	wantFoundIDs := []string{
		"m01", "m02", "m03", "m01",
		"m04", "m05", "m06", "m07", "m08",
		"m09", "m10", "m11", "m12",
		"m13", "m14", "m15", "m16", "m17",
		"m18", "m19", "m20", "m21", "m02",
	}
	if len(result.Messages) != len(wantFoundIDs) {
		t.Fatalf("messages count = %d, want %d", len(result.Messages), len(wantFoundIDs))
	}
	for i, m := range result.Messages {
		if m.MessageID != wantFoundIDs[i] {
			t.Errorf("第 %d 个 message = %s, want %s (保序/重复处理失败)", i, m.MessageID, wantFoundIDs[i])
		}
	}
}
