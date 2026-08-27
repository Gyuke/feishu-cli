package client

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBuildCreateApprovalInstanceBody_RequiredAndOptional(t *testing.T) {
	body, err := buildCreateApprovalInstanceBody(CreateApprovalInstanceOptions{
		ApprovalCode: "AC-1",
		UUID:         "uuid-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertApprovalBodyField(t, body, "approval_code", "AC-1")
	assertApprovalBodyField(t, body, "uuid", "uuid-1")
	if _, ok := body["form"]; ok {
		t.Fatalf("form should be omitted when empty, got %#v", body["form"])
	}
	if _, ok := body["open_id"]; ok {
		t.Fatalf("open_id should not appear in current initiate body")
	}
	if _, ok := body["user_id"]; ok {
		t.Fatalf("user_id should not appear in current initiate body")
	}
}

func TestBuildCreateApprovalInstanceBody_RequiredFields(t *testing.T) {
	_, err := buildCreateApprovalInstanceBody(CreateApprovalInstanceOptions{})
	if err == nil || !strings.Contains(err.Error(), "approval_code") {
		t.Fatalf("expected approval_code error, got %v", err)
	}
}

func TestBuildCreateApprovalInstanceBody_FormOptionalWhenProvided(t *testing.T) {
	body, err := buildCreateApprovalInstanceBody(CreateApprovalInstanceOptions{
		ApprovalCode: "AC-1",
		Form:         `[{"id":"widget_1","type":"input","value":"ok"}]`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertApprovalBodyField(t, body, "form", `[{"id":"widget_1","type":"input","value":"ok"}]`)
}

func TestBuildCreateApprovalInstanceBody_NodeListsNormalizedToKeyValue(t *testing.T) {
	body, err := buildCreateApprovalInstanceBody(CreateApprovalInstanceOptions{
		ApprovalCode:     "AC-1",
		NodeApproverList: json.RawMessage(`[{"node_id":"n1","value":["ou_a"]}]`),
		NodeCCList:       json.RawMessage(`[{"key":"n2","value":["ou_b"]}]`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := body["node_approver_user_id_list"]; ok {
		t.Fatal("legacy node_approver_user_id_list should not be present")
	}
	approvers, ok := body["node_approver_list"].([]map[string]any)
	if !ok || len(approvers) != 1 || approvers[0]["key"] != "n1" {
		t.Fatalf("node_approver_list = %#v", body["node_approver_list"])
	}
	ccs, ok := body["node_cc_list"].([]map[string]any)
	if !ok || len(ccs) != 1 || ccs[0]["key"] != "n2" {
		t.Fatalf("node_cc_list = %#v", body["node_cc_list"])
	}
}

func TestCreateApprovalInstance_RequiredFieldsRegression(t *testing.T) {
	_, err := CreateApprovalInstance(CreateApprovalInstanceOptions{ApprovalCode: ""}, "u-test")
	if err == nil || !strings.Contains(err.Error(), "approval_code") {
		t.Fatalf("expected approval_code error, got %v", err)
	}
}

func TestCreateApprovalInstanceUsesInitiatePathAndUserToken(t *testing.T) {
	const userToken = "u-test"
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"instance_code":"ic_1","instance_link":"https://example.com"}}`))
	})
	defer cleanup()

	result, err := CreateApprovalInstance(CreateApprovalInstanceOptions{
		ApprovalCode: "AC-1",
		Form:         `[]`,
	}, userToken)
	if err != nil {
		t.Fatalf("CreateApprovalInstance() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/open-apis/approval/v4/instances/initiate" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer "+userToken {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	assertApprovalBodyField(t, gotBody, "approval_code", "AC-1")
	if result.InstanceCode != "ic_1" || result.InstanceLink != "https://example.com" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCreateApprovalInstanceRejectsMissingUserToken(t *testing.T) {
	_, err := CreateApprovalInstance(CreateApprovalInstanceOptions{ApprovalCode: "AC-1"}, "")
	if err == nil {
		t.Fatal("expected missing user token error")
	}
}
