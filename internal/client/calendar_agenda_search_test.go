package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type capturedHTTPRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
	Auth   string
}

func captureAPI(t *testing.T, handle func(http.ResponseWriter, *http.Request, *capturedHTTPRequest)) func() []*capturedHTTPRequest {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs []*capturedHTTPRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"t-fake","expire":7200}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		cap := &capturedHTTPRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Body:   append([]byte(nil), body...),
			Auth:   r.Header.Get("Authorization"),
		}
		mu.Lock()
		reqs = append(reqs, cap)
		mu.Unlock()
		handle(w, r, cap)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)
	return func() []*capturedHTTPRequest {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*capturedHTTPRequest, len(reqs))
		copy(out, reqs)
		return out
	}
}

const testUserToken = "u-test-token"

func writeJSON(w http.ResponseWriter, status int, payload string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, payload)
}

func TestSearchEventsCurrentContract(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{
			"code":0,"msg":"ok",
			"data":{"items":[{"meta_data":{
				"event_id":"evt_1","summary":"周会",
				"start":{"date_time":"2026-04-23T15:00:00+08:00","timezone":"Asia/Shanghai"},
				"end":{"date_time":"2026-04-23T16:00:00+08:00","timezone":"Asia/Shanghai"},
				"app_link":"https://applink.feishu.cn/client/calendar/event/detail?id=evt_1"
			}}],"page_token":"next-tok"}
		}`)
	})

	res, err := SearchEventsWithParams(SearchEventsParams{
		CalendarID:  "cal_primary",
		Query:       "周会",
		StartTime:   "2026-04-01T00:00:00+08:00",
		EndTime:     "2026-04-30T23:59:59+08:00",
		AttendeeIDs: []string{"ou_user1", "oc_chat1", "omm_room1"},
		PageSize:    20,
		PageToken:   "tok-1",
	}, testUserToken)
	if err != nil {
		t.Fatalf("SearchEventsWithParams: %v", err)
	}
	events := res.Events
	pageToken := res.PageToken
	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("request count = %d, want 1", len(reqs))
	}
	req := reqs[0]
	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if req.Path != "/open-apis/calendar/v4/calendars/cal_primary/events/search_event" {
		t.Errorf("path = %s", req.Path)
	}
	if req.Query.Get("page_size") != "20" || req.Query.Get("page_token") != "tok-1" {
		t.Errorf("query = %s", req.Query.Encode())
	}
	if req.Auth != "Bearer "+testUserToken {
		t.Errorf("identity = %q, want user token", req.Auth)
	}
	var body map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("body json: %v", err)
	}
	if body["query"] != "周会" {
		t.Errorf("query = %v", body["query"])
	}
	filter, _ := body["filter"].(map[string]any)
	tr, _ := filter["time_range"].(map[string]any)
	if tr["start_time"] != "2026-04-01T00:00:00+08:00" || tr["end_time"] != "2026-04-30T23:59:59+08:00" {
		t.Errorf("time_range = %#v", tr)
	}
	if got, _ := filter["attendee_user_ids"].([]any); len(got) != 1 || got[0] != "ou_user1" {
		t.Errorf("attendee_user_ids = %#v", filter["attendee_user_ids"])
	}
	if got, _ := filter["attendee_chat_ids"].([]any); len(got) != 1 || got[0] != "oc_chat1" {
		t.Errorf("attendee_chat_ids = %#v", filter["attendee_chat_ids"])
	}
	if got, _ := filter["meeting_room_ids"].([]any); len(got) != 1 || got[0] != "omm_room1" {
		t.Errorf("meeting_room_ids = %#v", filter["meeting_room_ids"])
	}
	if _, ok := filter["start_time"]; ok {
		t.Error("legacy filter.start_time must not be sent")
	}
	if len(events) != 1 || events[0].EventID != "evt_1" || events[0].Summary != "周会" {
		t.Errorf("events = %#v", events)
	}
	if pageToken != "next-tok" {
		t.Errorf("pageToken = %q", pageToken)
	}
}

func TestSearchEventsBusinessCode(t *testing.T) {
	_ = captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":190003,"msg":"invalid calendar"}`)
	})
	_, err := SearchEventsWithParams(SearchEventsParams{CalendarID: "cal_x", Query: "x"}, testUserToken)
	if err == nil {
		t.Fatal("want business code error")
	}
	if !HasAPICode(err, 190003) {
		t.Errorf("HasAPICode(190003) = false, err=%v", err)
	}
}

func TestSearchEventsPageSizeRejected(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0,"msg":"ok","data":{"items":[]}}`)
	})
	_, err := SearchEventsWithParams(SearchEventsParams{CalendarID: "cal_x", Query: "x", PageSize: 99}, testUserToken)
	if err == nil {
		t.Fatal("page-size 99 must fail, not clamp")
	}
	if len(got()) != 0 {
		t.Fatalf("invalid page-size must not hit network, got %d", len(got()))
	}
}

func TestSearchEventsDefaultPrimaryAndEmptyQuery(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0,"msg":"ok","data":{"items":[]}}`)
	})
	_, err := SearchEventsWithParams(SearchEventsParams{}, testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("request count = %d", len(reqs))
	}
	if reqs[0].Path != "/open-apis/calendar/v4/calendars/primary/events/search_event" {
		t.Errorf("path = %s", reqs[0].Path)
	}
	var body map[string]any
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["query"] != "" {
		t.Errorf("empty query = %#v", body["query"])
	}
}

func TestSearchEventsHasMoreIndependentOfToken(t *testing.T) {
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{
			"code":0,"msg":"ok",
			"data":{"items":[{"meta_data":{"event_id":"evt_h","summary":"x",
				"start":{"date_time":"2026-04-23T15:00:00+08:00"},
				"end":{"date_time":"2026-04-23T16:00:00+08:00"}}}],
			"has_more":true,"page_token":""}
		}`)
	})
	res, err := SearchEventsWithParams(SearchEventsParams{CalendarID: "primary", Query: "x"}, testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	_ = got
	if res == nil || !res.HasMore {
		t.Fatalf("has_more must be true even when page_token is empty, got %#v", res)
	}
	if res.PageToken != "" {
		t.Errorf("page_token = %q, want empty", res.PageToken)
	}
	events, tok, err := SearchEvents("primary", "x", "", "", "", 0, testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || tok != "" {
		t.Errorf("wrapper signature: events=%d tok=%q", len(events), tok)
	}
}

func TestParseSearchEventTimeRange(t *testing.T) {
	start, end, err := ParseSearchEventTimeRange("2026-04-20", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(start, "2026-04-20T00:00:00") || !strings.HasPrefix(end, "2026-04-20T23:59:59") {
		t.Errorf("one-sided start: %s %s", start, end)
	}
	start, end, err = ParseSearchEventTimeRange("", "2026-04-27")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(start, "2026-04-27T00:00:00") || !strings.HasPrefix(end, "2026-04-27T23:59:59") {
		t.Errorf("one-sided end: %s %s", start, end)
	}
	if _, _, err := ParseSearchEventTimeRange("2026-04-27", "2026-04-20"); err == nil {
		t.Fatal("start>end must fail")
	}
	if _, _, err := ParseSearchEventTimeRange("not-a-date", "2026-04-20"); err == nil {
		t.Fatal("invalid start must fail")
	}
}

func TestParseAgendaDateRangeDSTAndOrder(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, loc)
	start, end, err := ParseAgendaDateRange("2026-03-08", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start.Format("2006-01-02") != "2026-03-08" {
		t.Errorf("start day = %s", start)
	}
	y, m, d := end.In(loc).Date()
	h, min, sec := end.In(loc).Clock()
	if y != 2026 || m != time.March || d != 8 || h != 23 || min != 59 || sec != 59 {
		t.Errorf("DST-safe end = %s, want 2026-03-08 23:59:59", end.In(loc))
	}
	unsafe := start.Add(24*time.Hour - time.Second)
	if unsafe.Equal(end) {
		t.Error("Add(24h-1s) matched calendar end; this TZ may not exercise DST")
	}
	if _, _, err := ParseAgendaDateRange("2026-03-09", "2026-03-08", now); err == nil {
		t.Fatal("start>end must fail")
	}
}

func TestAgendaWindowUnder40DaysNoSplit(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	end := start + 39*24*60*60
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0,"msg":"ok","data":{"items":[]}}`)
	})
	_, pageToken, hasMore, err := ListCalendarAgenda("cal_day", start, end, 50, "fake-token", testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	if pageToken != "" || hasMore {
		t.Errorf("fake pagination leaked: token=%q hasMore=%v", pageToken, hasMore)
	}
	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("want 1 request for <40d window, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Method != http.MethodGet {
		t.Errorf("method = %s", req.Method)
	}
	if req.Path != "/open-apis/calendar/v4/calendars/cal_day/events/instance_view" {
		t.Errorf("path = %s", req.Path)
	}
	if req.Query.Get("page_size") != "" || req.Query.Get("page_token") != "" {
		t.Errorf("must not send fake pagination, query=%s", req.Query.Encode())
	}
	if req.Query.Get("start_time") != strconv.FormatInt(start, 10) || req.Query.Get("end_time") != strconv.FormatInt(end, 10) {
		t.Errorf("window query = %s", req.Query.Encode())
	}
	if req.Auth != "Bearer "+testUserToken {
		t.Errorf("identity = %q", req.Auth)
	}
}

func TestAgendaPreemptiveSplitOver40Days(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	end := start + 40*24*60*60 + 1
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":0,"msg":"ok","data":{"items":[]}}`)
	})
	_, _, _, err := ListCalendarAgenda("primary", start, end, 0, "", testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	reqs := got()
	if len(reqs) < 2 {
		t.Fatalf("want preemptive split (>=2 requests), got %d", len(reqs))
	}
	maxSpan := int64(maxInstanceViewSpanSeconds)
	var minStart, maxEnd int64 = end, start
	for _, req := range reqs {
		st, _ := strconv.ParseInt(req.Query.Get("start_time"), 10, 64)
		et, _ := strconv.ParseInt(req.Query.Get("end_time"), 10, 64)
		if et-st > maxSpan {
			t.Errorf("sub-window %d-%d exceeds 40 days", st, et)
		}
		if st < minStart {
			minStart = st
		}
		if et > maxEnd {
			maxEnd = et
		}
		if req.Query.Get("page_size") != "" || req.Query.Get("page_token") != "" {
			t.Errorf("fake pagination on split request: %s", req.Query.Encode())
		}
	}
	if minStart != start || maxEnd != end {
		t.Errorf("split coverage [%d,%d], want [%d,%d]", minStart, maxEnd, start, end)
	}
}

func TestAgenda193104SplitAndDedup(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Unix()
	end := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC).Unix()
	var hits int
	got := captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		hits++
		if hits == 1 {
			writeJSON(w, http.StatusOK, `{"code":193104,"msg":"too many instances"}`)
			return
		}
		writeJSON(w, http.StatusOK, `{
			"code":0,"msg":"ok",
			"data":{"items":[{
				"event_id":"evt_dup","summary":"Overlap",
				"status":"confirmed",
				"start_time":{"timestamp":"1740790000"},
				"end_time":{"timestamp":"1740793600"}
			}]}
		}`)
	})
	events, _, _, err := ListCalendarAgenda("cal_busy", start, end, 10, "ignore-me", testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	reqs := got()
	if len(reqs) < 3 {
		t.Fatalf("want original + two halves, got %d", len(reqs))
	}
	if reqs[0].Query.Get("page_token") != "" {
		t.Error("193104 path must not send page_token")
	}
	if len(events) != 1 || events[0].EventID != "evt_dup" {
		t.Errorf("dedup failed: %#v", events)
	}
}

func TestAgenda193104SplitExhausted(t *testing.T) {
	start := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC).Unix()
	end := start + 60*60
	_ = captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{"code":193104,"msg":"too many instances"}`)
	})
	_, _, _, err := ListCalendarAgenda("cal_x", start, end, 0, "", testUserToken)
	if err == nil {
		t.Fatal("want 193104 error")
	}
	if !HasAPICode(err, 193104) {
		t.Errorf("HasAPICode(193104) = false, err=%v", err)
	}
}

func TestAgendaAllDayEndExclusive(t *testing.T) {
	start := time.Date(2025, 3, 21, 0, 0, 0, 0, time.UTC).Unix()
	end := start + 24*60*60 - 1
	_ = captureAPI(t, func(w http.ResponseWriter, r *http.Request, cap *capturedHTTPRequest) {
		writeJSON(w, http.StatusOK, `{
			"code":0,"msg":"ok",
			"data":{"items":[
				{
					"event_id":"evt_allday","summary":"Holiday",
					"status":"confirmed",
					"start_time":{"date":"2025-03-21"},
					"end_time":{"date":"2025-03-22"}
				},
				{
					"event_id":"evt_cancelled","summary":"Gone",
					"status":"cancelled",
					"start_time":{"timestamp":"1742515200"},
					"end_time":{"timestamp":"1742518800"}
				}
			]}
		}`)
	})
	events, _, _, err := ListCalendarAgenda("primary", start, end, 0, "", testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 visible event, got %#v", events)
	}
	if !events[0].IsAllDay {
		t.Error("want is_all_day")
	}
	if events[0].StartTime != "2025-03-21" {
		t.Errorf("start = %s", events[0].StartTime)
	}
	if events[0].EndTime != "2025-03-21" {
		t.Errorf("exclusive all-day end = %s, want 2025-03-21", events[0].EndTime)
	}
}

func TestBuildSearchEventFilterEmpty(t *testing.T) {
	if got := buildSearchEventFilter("", "", nil); got != nil {
		t.Errorf("empty filter = %#v", got)
	}
}
