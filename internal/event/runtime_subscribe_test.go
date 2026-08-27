package event

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func subscribeTestDef(url string) KeyDefinition {
	return KeyDefinition{
		Key:            "approval.instance.status_changed_v4",
		SubscribePath:  "/subscribe",
		SubscribeTypes: []string{"INVOLVED_APPROVAL"},
	}
}

func newSubscribeRuntime(baseURL string) *Runtime {
	return NewRuntime(ConsumeOptions{
		AppID:           "cli_test",
		AppSecret:       "secret",
		EventKey:        "approval.instance.status_changed_v4",
		BaseURL:         baseURL,
		UserAccessToken: "u-test",
		ErrOut:          io.Discard,
	})
}

func TestRegisterSubscriptionsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subscribe" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer u-test" {
			t.Errorf("Authorization = %s", got)
		}
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	r := newSubscribeRuntime(srv.URL)
	if err := r.registerSubscriptions(context.Background(), subscribeTestDef(srv.URL)); err != nil {
		t.Fatalf("应成功，实际: %v", err)
	}
}

func TestRegisterSubscriptionsFailClosedOnHTMLError(t *testing.T) {
	// 网关 5xx + HTML 错误页：必须判定失败（此前吞 Unmarshal 错误会假成功）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()

	r := newSubscribeRuntime(srv.URL)
	err := r.registerSubscriptions(context.Background(), subscribeTestDef(srv.URL))
	if err == nil {
		t.Fatal("HTTP 502 + HTML 响应应判定注册失败，实际返回 nil（假成功）")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("错误应包含状态码，实际: %v", err)
	}
}

func TestRegisterSubscriptionsFailClosedOnBadJSON(t *testing.T) {
	// HTTP 200 但响应体非 JSON：同样失败
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	r := newSubscribeRuntime(srv.URL)
	if err := r.registerSubscriptions(context.Background(), subscribeTestDef(srv.URL)); err == nil {
		t.Fatal("非 JSON 响应应判定注册失败")
	}
}

func TestRegisterSubscriptionsBizError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":99991679,"msg":"scope 不足"}`))
	}))
	defer srv.Close()

	r := newSubscribeRuntime(srv.URL)
	err := r.registerSubscriptions(context.Background(), subscribeTestDef(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "99991679") {
		t.Fatalf("业务错误码应透出，实际: %v", err)
	}
}

func TestRegisterSubscriptionsCtxCancel(t *testing.T) {
	// 端点挂起：ctx 取消必须能中断（此前 DefaultClient 无超时且未绑 ctx 会永久阻塞）。
	// handler 用有界 sleep 模拟挂起（阻塞到连接关闭会让 srv.Close 等待，反而卡测试）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r := newSubscribeRuntime(srv.URL)
	start := time.Now()
	err := r.registerSubscriptions(ctx, subscribeTestDef(srv.URL))
	if err == nil {
		t.Fatal("挂起端点应超时报错")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("应在 ctx 超时后立即返回，实际耗时 %v", elapsed)
	}
}

func TestRegisterSubscriptionsRequiresUserToken(t *testing.T) {
	r := NewRuntime(ConsumeOptions{EventKey: "x", BaseURL: "http://127.0.0.1:1", ErrOut: io.Discard})
	err := r.registerSubscriptions(context.Background(), subscribeTestDef(""))
	if err == nil || !strings.Contains(err.Error(), "auth login") {
		t.Fatalf("缺 User Token 应报错并提示登录，实际: %v", err)
	}
}

func TestSubscriptionRequestBodiesApprovalVsVC(t *testing.T) {
	approval := KeyDefinition{
		EventType:      "approval.instance.status_changed_v4",
		SubscribeTypes: []string{"INVOLVED_APPROVAL", "MANAGED_APPROVAL"},
	}
	got := subscriptionRequestBodies(approval)
	if len(got) != 2 || got[0]["subscription_type"] != "INVOLVED_APPROVAL" || got[1]["subscription_type"] != "MANAGED_APPROVAL" {
		t.Fatalf("审批 body = %#v", got)
	}
	if _, ok := got[0]["event_type"]; ok {
		t.Fatal("审批 body 不应带 event_type")
	}

	vc := KeyDefinition{
		EventType:          "vc.meeting.participant_meeting_started_v1",
		SubscribeEventType: true,
		SubscribeTypes:     []string{"should-be-ignored"},
	}
	got = subscriptionRequestBodies(vc)
	if len(got) != 1 || got[0]["event_type"] != "vc.meeting.participant_meeting_started_v1" {
		t.Fatalf("VC body = %#v, want event_type=participant_meeting_started", got)
	}
	if _, ok := got[0]["subscription_type"]; ok {
		t.Fatal("VC body 不应带 subscription_type")
	}
}

func TestRegisterSubscriptionsVCEventTypePathAndBody(t *testing.T) {
	var gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, r.Body)
		gotBody = buf.String()
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	r := NewRuntime(ConsumeOptions{
		AppID:           "cli_test",
		AppSecret:       "secret",
		EventKey:        "vc.meeting.participant_meeting_started_v1",
		BaseURL:         srv.URL,
		UserAccessToken: "u-test",
		ErrOut:          io.Discard,
	})
	def := KeyDefinition{
		Key:                "vc.meeting.participant_meeting_started_v1",
		EventType:          "vc.meeting.participant_meeting_started_v1",
		SubscribePath:      "/open-apis/vc/v1/meetings/subscription",
		SubscribeEventType: true,
		Scopes:             []string{"vc:meeting.meetingevent:read"},
	}
	if err := r.registerSubscriptions(context.Background(), def); err != nil {
		t.Fatalf("VC 订阅应成功，实际: %v", err)
	}
	if gotPath != "/open-apis/vc/v1/meetings/subscription" {
		t.Errorf("path = %s", gotPath)
	}
	if gotAuth != "Bearer u-test" {
		t.Errorf("Authorization = %s", gotAuth)
	}
	if !strings.Contains(gotBody, `"event_type":"vc.meeting.participant_meeting_started_v1"`) {
		t.Errorf("body = %s, want event_type", gotBody)
	}
	if strings.Contains(gotBody, "subscription_type") {
		t.Errorf("VC body 不应含 subscription_type: %s", gotBody)
	}
}
