package client

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseEmbeddedJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  any
	}{
		{
			name:  "json object",
			input: `{"name":"leave"}`,
			want:  map[string]any{"name": "leave"},
		},
		{
			name:  "json array",
			input: `[{"id":"widget_1"}]`,
			want:  []any{map[string]any{"id": "widget_1"}},
		},
		{
			name:  "plain string fallback",
			input: `not-json`,
			want:  "not-json",
		},
		{
			name:  "empty string",
			input: `   `,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEmbeddedJSON(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseEmbeddedJSON(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestApprovalTaskStringUnmarshal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "string", input: `"RUNNING"`, want: "RUNNING"},
		{name: "number", input: `42`, want: "42"},
		{name: "boolean", input: `true`, want: "true"},
		{name: "null", input: `null`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got approvalTaskString
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v", tt.input, err)
			}
			if got.String() != tt.want {
				t.Fatalf("json.Unmarshal(%s) = %q, want %q", tt.input, got.String(), tt.want)
			}
		})
	}
}

func TestParseApprovalDefinitionResponse(t *testing.T) {
	body := []byte(`{
		"code": 0,
		"msg": "success",
		"data": {
			"approval_name": "请假",
			"form": "[{\"id\":\"widget_1\"}]",
			"node_list": [
				{
					"name": "直属上级",
					"node_id": "node_1",
					"custom_node_id": "custom_1",
					"node_type": "AND",
					"need_approver": true,
					"approver_chosen_multi": false,
					"require_signature": true
				}
			]
		}
	}`)

	result, err := parseApprovalDefinitionResponse(body, "approval_123")
	if err != nil {
		t.Fatalf("parseApprovalDefinitionResponse() error = %v", err)
	}
	if result.ApprovalCode != "approval_123" {
		t.Fatalf("ApprovalCode = %q, want %q", result.ApprovalCode, "approval_123")
	}
	if result.ApprovalName != "请假" {
		t.Fatalf("ApprovalName = %q, want %q", result.ApprovalName, "请假")
	}
	wantForm := []any{map[string]any{"id": "widget_1"}}
	if !reflect.DeepEqual(result.Form, wantForm) {
		t.Fatalf("Form = %#v, want %#v", result.Form, wantForm)
	}
	if len(result.NodeList) != 1 || result.NodeList[0].NodeID != "node_1" || !result.NodeList[0].NeedApprover {
		t.Fatalf("NodeList = %#v, want one populated node", result.NodeList)
	}
}

func TestParseApprovalDefinitionResponseError(t *testing.T) {
	body := []byte(`{"code": 99991663, "msg": "invalid approval code"}`)
	_, err := parseApprovalDefinitionResponse(body, "approval_123")
	if err == nil {
		t.Fatal("parseApprovalDefinitionResponse() error = nil, want non-nil")
	}
	if got := err.Error(); got != "获取审批定义失败: code=99991663, msg=invalid approval code" {
		t.Fatalf("parseApprovalDefinitionResponse() error = %q", got)
	}
}

func TestParseApprovalTaskQueryResponseCurrentContract(t *testing.T) {
	body := []byte(`{
		"code": 0,
		"msg": "success",
		"data": {
			"page_token": "next_page",
			"has_more": true,
			"count": 10,
			"tasks": [
				{
					"topic": 1,
					"user_id": "ou_user",
					"title": "审批标题",
					"status": "1",
					"instance_code": "instance_1",
					"instance_status": "1",
					"definition_code": "def_1",
					"definition_name": "文档权限申请",
					"task_id": "task_id",
					"initiator": "ou_init",
					"initiator_name": "A",
					"summaries": [{"key":"reason","value":"出差"}],
					"support_api_operate": true
				}
			]
		}
	}`)

	result, err := parseApprovalTaskQueryResponse(body)
	if err != nil {
		t.Fatalf("parseApprovalTaskQueryResponse() error = %v", err)
	}
	if !result.HasMore || result.PageToken != "next_page" {
		t.Fatalf("pagination = %#v", result)
	}
	if result.Count == nil || *result.Count != 10 {
		t.Fatalf("Count = %#v, want 10", result.Count)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("len(Tasks) = %d, want 1", len(result.Tasks))
	}
	task := result.Tasks[0]
	if task.Topic != "1" {
		t.Fatalf("Topic = %q, want 1", task.Topic)
	}
	if task.InstanceCode != "instance_1" {
		t.Fatalf("InstanceCode = %q", task.InstanceCode)
	}
	if task.InstanceStatus != "1" {
		t.Fatalf("InstanceStatus = %q", task.InstanceStatus)
	}
	if task.Initiator != "ou_init" || task.InitiatorName != "A" {
		t.Fatalf("initiator = %q/%q", task.Initiator, task.InitiatorName)
	}
	if !task.SupportAPIOperate {
		t.Fatal("SupportAPIOperate = false, want true")
	}
	if len(task.Summaries) != 1 || task.Summaries[0].Key != "reason" {
		t.Fatalf("Summaries = %#v", task.Summaries)
	}
}

func TestQueryApprovalTasksUsesCurrentListPathAndUserToken(t *testing.T) {
	const userToken = "u-test"
	var gotMethod, gotPath, gotAuth, gotUserID, gotTopic, gotPageSize string
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotUserID = r.URL.Query().Get("user_id")
		gotTopic = r.URL.Query().Get("topic")
		gotPageSize = r.URL.Query().Get("page_size")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"tasks":[],"has_more":false,"count":0}}`))
	})
	defer cleanup()

	_, err := QueryApprovalTasksRaw(ApprovalTaskQueryOptions{
		Topic:      "1",
		PageSize:   20,
		UserIDType: "open_id",
	}, userToken)
	if err != nil {
		t.Fatalf("QueryApprovalTasksRaw() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/open-apis/approval/v4/tasks" {
		t.Fatalf("path = %q, want /open-apis/approval/v4/tasks", gotPath)
	}
	if gotAuth != "Bearer "+userToken {
		t.Fatalf("Authorization = %q, want user token", gotAuth)
	}
	if gotUserID != "" {
		t.Fatalf("user_id query = %q, want empty", gotUserID)
	}
	if gotTopic != "1" {
		t.Fatalf("topic = %q, want 1", gotTopic)
	}
	if gotPageSize != "20" {
		t.Fatalf("page_size = %q, want 20", gotPageSize)
	}
}

func TestQueryApprovalTasksRejectsMissingUserToken(t *testing.T) {
	_, err := QueryApprovalTasksRaw(ApprovalTaskQueryOptions{Topic: "1"}, "")
	if err == nil {
		t.Fatal("expected missing user token error")
	}
}

func TestGetApprovalInstanceUsesCurrentDetailPathAndUserToken(t *testing.T) {
	const userToken = "u-test"
	var gotMethod, gotPath, gotAuth, gotInstanceCode, gotUserIDType string
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotInstanceCode = r.URL.Query().Get("instance_code")
		gotUserIDType = r.URL.Query().Get("user_id_type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"instance_code":"instance_1","status":"PENDING"}}`))
	})
	defer cleanup()

	data, err := GetApprovalInstance(GetApprovalInstanceOptions{
		InstanceCode: "instance_1",
		UserIDType:   "open_id",
	}, userToken)
	if err != nil {
		t.Fatalf("GetApprovalInstance() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/open-apis/approval/v4/instances/detail" {
		t.Fatalf("path = %q, want instances/detail", gotPath)
	}
	if gotAuth != "Bearer "+userToken {
		t.Fatalf("Authorization = %q, want user token", gotAuth)
	}
	if gotInstanceCode != "instance_1" {
		t.Fatalf("instance_code = %q", gotInstanceCode)
	}
	if gotUserIDType != "open_id" {
		t.Fatalf("user_id_type = %q", gotUserIDType)
	}
	if data["status"] != "PENDING" {
		t.Fatalf("status = %#v", data["status"])
	}
}

func TestGetApprovalInstanceRejectsMissingUserToken(t *testing.T) {
	_, err := GetApprovalInstanceRaw(GetApprovalInstanceOptions{InstanceCode: "instance_1"}, "")
	if err == nil {
		t.Fatal("expected missing user token error")
	}
}

func TestGetApprovalDefinitionUsesCurrentDetailPathAndUserToken(t *testing.T) {
	const userToken = "u-test"
	var gotMethod, gotPath, gotAuth, gotLocale string
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotLocale = r.URL.Query().Get("locale")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"approval_name":"请假","form":"[]","node_list":[]}}`))
	})
	defer cleanup()

	def, err := GetApprovalDefinition("7C468A54-8745-2245-9675-08B7C63E7A85", GetApprovalOptions{Locale: "zh-CN"}, userToken)
	if err != nil {
		t.Fatalf("GetApprovalDefinition() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotPath != "/open-apis/approval/v4/approvals/7C468A54-8745-2245-9675-08B7C63E7A85/detail" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer "+userToken {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotLocale != "zh-CN" {
		t.Fatalf("locale = %q", gotLocale)
	}
	if def.ApprovalName != "请假" {
		t.Fatalf("ApprovalName = %q", def.ApprovalName)
	}
}

func TestApprovalSourceHasNoUATPaths(t *testing.T) {
	data, err := os.ReadFile("approval.go")
	if err != nil {
		t.Fatalf("read approval.go: %v", err)
	}
	if strings.Contains(string(data), "uat_") {
		t.Fatal("production approval.go still contains uat_ paths")
	}
}
