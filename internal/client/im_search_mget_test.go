package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSearchMessagesCurrentContract(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{
			"code":0,"msg":"ok",
			"data":{"items":[{"meta_data":{"message_id":"om_1"}},{"meta_data":{"message_id":"om_2"}}],
			"has_more":true,"page_token":"next"}
		}`)
	})

	res, err := SearchMessages(SearchMessagesOptions{
		Query:        "incident",
		ChatIDs:      []string{"oc_1"},
		FromIDs:      []string{"ou_1"},
		AtChatterIDs: []string{"ou_2"},
		MessageType:  "media",
		ChatType:     "group_chat",
		FromType:     "user",
		StartTime:    "2026-03-01T00:00:00+08:00",
		EndTime:      "2026-03-02T23:59:59+08:00",
		PageSize:     20,
		PageToken:    "tok-a",
		UserIDType:   "open_id",
	}, testUserToken)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("request count = %d, want 1", len(reqs))
	}
	req := reqs[0]
	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if req.Path != "/open-apis/im/v1/messages/search" {
		t.Errorf("path = %s", req.Path)
	}
	if req.Query.Get("page_size") != "20" || req.Query.Get("page_token") != "tok-a" {
		t.Errorf("query = %s", req.Query.Encode())
	}
	if req.Query.Get("user_id_type") != "" {
		t.Errorf("user_id_type must not be sent on current IM search, got %s", req.Query.Get("user_id_type"))
	}
	if req.Auth != "Bearer "+testUserToken {
		t.Errorf("identity = %q", req.Auth)
	}
	var body map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["query"] != "incident" {
		t.Errorf("query = %v", body["query"])
	}
	filter, _ := body["filter"].(map[string]any)
	tr, _ := filter["time_range"].(map[string]any)
	if tr["start_time"] != "2026-03-01T00:00:00+08:00" || tr["end_time"] != "2026-03-02T23:59:59+08:00" {
		t.Errorf("time_range = %#v", tr)
	}
	if filter["chat_type"] != "group" {
		t.Errorf("chat_type = %v, want group", filter["chat_type"])
	}
	if types, _ := filter["include_attachment_types"].([]any); len(types) != 1 || types[0] != "video" {
		t.Errorf("include_attachment_types = %#v, want [video] (media→video)", filter["include_attachment_types"])
	}
	if types, _ := filter["from_types"].([]any); len(types) != 1 || types[0] != "user" {
		t.Errorf("from_types = %#v", filter["from_types"])
	}
	if res.MessageIDs[0] != "om_1" || res.MessageIDs[1] != "om_2" || !res.HasMore || res.PageToken != "next" {
		t.Errorf("result = %#v", res)
	}
}

func TestSearchMessagesBusinessCode(t *testing.T) {
	_ = captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":230001,"msg":"no permission"}`)
	})
	_, err := SearchMessages(SearchMessagesOptions{Query: "x"}, testUserToken)
	if err == nil {
		t.Fatal("want business code error")
	}
	if !HasAPICode(err, 230001) {
		t.Errorf("HasAPICode(230001) = false, err=%v", err)
	}
}

func TestSearchChatsCurrentContract(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{
			"code":0,"msg":"ok",
			"data":{"items":[{"meta_data":{"chat_id":"oc_1","name":"team-alpha","description":"d","owner_id":"ou_1","external":false}}],
			"has_more":false,"page_token":""}
		}`)
	})
	res, err := SearchChats(SearchChatsOptions{Query: "team-alpha", PageSize: 50, PageToken: "p1"}, testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("request count = %d", len(reqs))
	}
	req := reqs[0]
	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if req.Path != "/open-apis/im/v2/chats/search" {
		t.Errorf("path = %s", req.Path)
	}
	if req.Query.Get("page_size") != "50" || req.Query.Get("page_token") != "p1" {
		t.Errorf("query = %s", req.Query.Encode())
	}
	if req.Auth != "Bearer "+testUserToken {
		t.Errorf("identity = %q", req.Auth)
	}
	var body map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["query"] != `"team-alpha"` {
		t.Errorf("hyphenated query = %#v, want quoted", body["query"])
	}
	if len(res.Items) != 1 || res.Items[0].ChatID != "oc_1" || res.Items[0].Name != "team-alpha" {
		t.Errorf("items = %#v", res.Items)
	}
}

func TestSearchChatsBusinessCode(t *testing.T) {
	_ = captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":232033,"msg":"external chat denied"}`)
	})
	_, err := SearchChats(SearchChatsOptions{Query: "x"}, testUserToken)
	if err == nil {
		t.Fatal("want business code error")
	}
	if !HasAPICode(err, 232033) {
		t.Errorf("HasAPICode(232033) = false, err=%v", err)
	}
}

func TestBatchGetMessagesMGetBatching(t *testing.T) {
	ids := make([]string, 51)
	for i := range ids {
		ids[i] = fmt.Sprintf("om_%02d", i+1)
	}
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		if r.URL.Path != "/open-apis/im/v1/messages/mget" {
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if cap.Auth != "Bearer "+testUserToken {
			t.Errorf("identity = %q", cap.Auth)
		}
		if r.URL.Query().Get("with_sender_name") != "true" {
			t.Errorf("with_sender_name = %q", r.URL.Query().Get("with_sender_name"))
		}
		if r.URL.Query().Get("card_msg_content_type") != CardMsgContentTypeUser {
			t.Errorf("card_msg_content_type = %q", r.URL.Query().Get("card_msg_content_type"))
		}
		gotIDs := r.URL.Query()["message_ids"]
		if len(gotIDs) > messagesMGetBatchSize {
			t.Errorf("batch size %d exceeds 50", len(gotIDs))
		}
		var items []string
		for _, id := range gotIDs {
			items = append(items, fmt.Sprintf(`{"message_id":%q,"msg_type":"text","body":{"content":"{\"text\":\"%s\"}"}}`, id, id))
		}
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"code":0,"msg":"ok","data":{"items":[%s]}}`, strings.Join(items, ",")))
	})

	res, err := BatchGetMessages(ids, testUserToken, CardMsgContentTypeUser)
	if err != nil {
		t.Fatal(err)
	}
	reqs := got()
	if len(reqs) != 2 {
		t.Fatalf("want 2 mget batches for 51 ids, got %d", len(reqs))
	}
	if len(reqs[0].Query["message_ids"]) != 50 || len(reqs[1].Query["message_ids"]) != 1 {
		t.Errorf("batch sizes = %d, %d", len(reqs[0].Query["message_ids"]), len(reqs[1].Query["message_ids"]))
	}
	for _, req := range reqs {
		if strings.Contains(req.Path, "/open-apis/im/v1/messages/om_") {
			t.Errorf("N+1 GetMessage leaked: %s", req.Path)
		}
	}
	if len(res.Messages) != 51 || StringVal(res.Messages[0].MessageId) != "om_01" || StringVal(res.Messages[50].MessageId) != "om_51" {
		t.Errorf("order/count mismatch: len=%d first=%v last=%v", len(res.Messages), res.Messages[0], res.Messages[50])
	}
}

func TestBatchGetMessagesBusinessCode(t *testing.T) {
	_ = captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":230002,"msg":"message not found"}`)
	})
	_, err := BatchGetMessages([]string{"om_missing"}, testUserToken, "")
	if err == nil {
		t.Fatal("want business code error")
	}
	if !HasAPICode(err, 230002) {
		t.Errorf("HasAPICode(230002) = false, err=%v", err)
	}
}

func TestBatchGetMessagesMissingIDStrict(t *testing.T) {
	_ = captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0,"msg":"ok","data":{"items":[{"message_id":"om_ok","msg_type":"text"}]}}`)
	})
	_, err := BatchGetMessages([]string{"om_ok", "om_missing"}, testUserToken, "")
	if err == nil {
		t.Fatal("strict mode should fail on missing id")
	}
}

func TestSearchChatsNextPageTokenFallback(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{
			"code":0,"msg":"ok",
			"data":{"items":[{"meta_data":{"chat_id":"oc_1","name":"n"}}],
			"has_more":true,"next_page_token":"n2"}
		}`)
	})
	res, err := SearchChats(SearchChatsOptions{Query: "n", PageSize: 20}, testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasMore || res.PageToken != "n2" {
		t.Errorf("next_page_token fallback = %#v", res)
	}
	_ = got
}

func TestSearchChatsPageSizeRejected(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0}`)
	})
	_, err := SearchChats(SearchChatsOptions{Query: "n", PageSize: 101}, testUserToken)
	if err == nil {
		t.Fatal("page-size 101 must fail")
	}
	if len(got()) != 0 {
		t.Fatalf("invalid page-size must not hit network, got %d", len(got()))
	}
}

func TestSearchMessagesExtraFiltersAndLink(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0,"msg":"ok","data":{"items":[],"has_more":false}}`)
	})
	_, err := SearchMessages(SearchMessagesOptions{
		Query:           "",
		ChatIDs:         []string{"oc_1"},
		MessageType:     "link",
		FromType:        "user",
		ExcludeFromType: "bot",
		IsAtMe:          true,
		PageSize:        20,
	}, testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(got()[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	filter, _ := body["filter"].(map[string]any)
	if filter["is_at_me"] != true {
		t.Errorf("is_at_me = %#v", filter["is_at_me"])
	}
	if types, _ := filter["include_attachment_types"].([]any); len(types) != 1 || types[0] != "link" {
		t.Errorf("attachment = %#v", filter["include_attachment_types"])
	}
	if types, _ := filter["exclude_from_types"].([]any); len(types) != 1 || types[0] != "bot" {
		t.Errorf("exclude_from_types = %#v", filter["exclude_from_types"])
	}
}

func TestSearchMessagesPageSizeRejected(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0}`)
	})
	_, err := SearchMessages(SearchMessagesOptions{Query: "x", PageSize: 51}, testUserToken)
	if err == nil {
		t.Fatal("page-size 51 must fail")
	}
	if len(got()) != 0 {
		t.Fatalf("invalid page-size must not hit network, got %d", len(got()))
	}
}

func TestSearchMessagesInvalidTimeBeforeNetwork(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0}`)
	})
	_, err := SearchMessages(SearchMessagesOptions{Query: "x", StartTime: "not-a-time"}, testUserToken)
	if err == nil {
		t.Fatal("invalid time must fail")
	}
	if len(got()) != 0 {
		t.Fatalf("invalid time must not hit network, got %d", len(got()))
	}
	_, err = SearchMessages(SearchMessagesOptions{
		Query:     "x",
		StartTime: "2026-04-27T00:00:00+08:00",
		EndTime:   "2026-04-20T00:00:00+08:00",
	}, testUserToken)
	if err == nil {
		t.Fatal("start>end must fail")
	}
}

func TestResolvePageLimit(t *testing.T) {
	if _, err := ResolvePageLimit(-1, SearchPageLimitMax, true); err == nil {
		t.Fatal("negative page-limit must fail")
	}
	if _, err := ResolvePageLimit(41, SearchPageLimitMax, true); err == nil {
		t.Fatal("page-limit > 40 must fail")
	}
	got, err := ResolvePageLimit(0, SearchPageLimitMax, true)
	if err != nil || got != SearchPageLimitMax {
		t.Errorf("page-all + 0 = %d %v, want %d", got, err, SearchPageLimitMax)
	}
	got, err = ResolvePageLimit(5, SearchPageLimitMax, true)
	if err != nil || got != 5 {
		t.Errorf("explicit 5 = %d %v", got, err)
	}
	got, err = ResolvePageLimit(0, SearchPageLimitMax, false)
	if err != nil || got != 0 {
		t.Errorf("not page-all + 0 stays 0: %d %v", got, err)
	}
}

func TestPaginationCursorNoProgress(t *testing.T) {
	more, token, err := PaginationCursor(true, "", "n2", "")
	if err != nil || !more || token != "n2" {
		t.Errorf("fallback next_page_token: more=%v token=%q err=%v", more, token, err)
	}
	if _, _, err := PaginationCursor(true, "", "", "prev"); err == nil {
		t.Fatal("empty cursor with has_more must fail")
	}
	if _, _, err := PaginationCursor(true, "same", "", "same"); err == nil {
		t.Fatal("repeated cursor must fail")
	}
	more, token, err = PaginationCursor(false, "", "n2", "p")
	if err != nil || more || token != "n2" {
		t.Errorf("has_more=false still surfaces token: %v %q %v", more, token, err)
	}
}

func TestNormalizeChatSearchQuery(t *testing.T) {
	if got := normalizeChatSearchQuery("hello"); got != "hello" {
		t.Errorf("plain = %q", got)
	}
	if got := normalizeChatSearchQuery("team-alpha"); got != `"team-alpha"` {
		t.Errorf("hyphen = %q", got)
	}
}
