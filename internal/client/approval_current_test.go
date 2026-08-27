package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
)

type capturedApprovalRequest struct {
	Method string
	Path   string
	Query  url.Values
	Auth   string
	Body   map[string]any
}

func captureApprovalJSON(t *testing.T, handler http.HandlerFunc, invoke func()) capturedApprovalRequest {
	t.Helper()
	var got capturedApprovalRequest
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.Method = r.Method
		got.Path = r.URL.Path
		got.Query = r.URL.Query()
		got.Auth = r.Header.Get("Authorization")
		if r.Body != nil && r.Method != http.MethodGet {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &got.Body); err != nil {
					t.Fatalf("decode body %s: %v", raw, err)
				}
			}
		}
		handler(w, r)
	})
	defer cleanup()
	invoke()
	return got
}

func writeApprovalOK(w http.ResponseWriter, data string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":` + data + `}`))
}

func TestApprovalCurrentContractHTTPSurface(t *testing.T) {
	const userToken = "u-current"

	t.Run("definition detail", func(t *testing.T) {
		got := captureApprovalJSON(t, func(w http.ResponseWriter, r *http.Request) {
			writeApprovalOK(w, `{"approval_name":"请假","form":"[]","node_list":[]}`)
		}, func() {
			if _, err := GetApprovalDefinitionRaw("code_1", GetApprovalOptions{Locale: "en-US"}, userToken); err != nil {
				t.Fatalf("GetApprovalDefinitionRaw() error = %v", err)
			}
		})
		if got.Method != http.MethodGet || got.Path != "/open-apis/approval/v4/approvals/code_1/detail" {
			t.Fatalf("request = %s %s", got.Method, got.Path)
		}
		if got.Query.Get("locale") != "en-US" || got.Query.Get("user_id") != "" || got.Query.Get("with_admin_id") != "" {
			t.Fatalf("query = %v", got.Query)
		}
		if got.Auth != "Bearer "+userToken {
			t.Fatalf("auth = %q", got.Auth)
		}
	})

	t.Run("instance detail", func(t *testing.T) {
		got := captureApprovalJSON(t, func(w http.ResponseWriter, r *http.Request) {
			writeApprovalOK(w, `{"instance_code":"ic","status":"PENDING"}`)
		}, func() {
			if _, err := GetApprovalInstanceRaw(GetApprovalInstanceOptions{InstanceCode: "ic", Locale: "zh-CN", UserIDType: "user_id"}, userToken); err != nil {
				t.Fatalf("GetApprovalInstanceRaw() error = %v", err)
			}
		})
		if got.Method != http.MethodGet || got.Path != "/open-apis/approval/v4/instances/detail" {
			t.Fatalf("request = %s %s", got.Method, got.Path)
		}
		if got.Query.Get("instance_code") != "ic" || got.Query.Get("locale") != "zh-CN" || got.Query.Get("user_id_type") != "user_id" {
			t.Fatalf("query = %v", got.Query)
		}
		if got.Auth != "Bearer "+userToken {
			t.Fatalf("auth = %q", got.Auth)
		}
	})

	t.Run("instance initiated", func(t *testing.T) {
		got := captureApprovalJSON(t, func(w http.ResponseWriter, r *http.Request) {
			writeApprovalOK(w, `{"instances":[{"instance_code":"ic","instance_status":"1"}],"count":1,"has_more":false}`)
		}, func() {
			result, err := ListInitiatedApprovalInstances(ListInitiatedApprovalInstancesOptions{
				PageSize:       20,
				DefinitionCode: "def_1",
				UserIDType:     "open_id",
			}, userToken)
			if err != nil {
				t.Fatalf("ListInitiatedApprovalInstances() error = %v", err)
			}
			if result.Count == nil || *result.Count != 1 || len(result.Instances) != 1 || result.Instances[0].InstanceCode != "ic" {
				t.Fatalf("result = %#v", result)
			}
		})
		if got.Method != http.MethodGet || got.Path != "/open-apis/approval/v4/instances/initiated" {
			t.Fatalf("request = %s %s", got.Method, got.Path)
		}
		if got.Query.Get("page_size") != "20" || got.Query.Get("definition_code") != "def_1" || got.Query.Get("user_id_type") != "open_id" {
			t.Fatalf("query = %v", got.Query)
		}
		if got.Auth != "Bearer "+userToken {
			t.Fatalf("auth = %q", got.Auth)
		}
	})

	t.Run("instance initiate", func(t *testing.T) {
		got := captureApprovalJSON(t, func(w http.ResponseWriter, r *http.Request) {
			writeApprovalOK(w, `{"instance_code":"ic","instance_link":"https://x"}`)
		}, func() {
			if _, err := CreateApprovalInstance(CreateApprovalInstanceOptions{ApprovalCode: "AC-1", Form: "[]"}, userToken); err != nil {
				t.Fatalf("CreateApprovalInstance() error = %v", err)
			}
		})
		if got.Method != http.MethodPost || got.Path != "/open-apis/approval/v4/instances/initiate" {
			t.Fatalf("request = %s %s", got.Method, got.Path)
		}
		if got.Query.Get("user_id_type") != "" {
			t.Fatalf("initiate should not send user_id_type query, got %v", got.Query)
		}
		if got.Auth != "Bearer "+userToken {
			t.Fatalf("auth = %q", got.Auth)
		}
		assertApprovalBodyField(t, got.Body, "approval_code", "AC-1")
	})

	t.Run("task list omits user_id", func(t *testing.T) {
		got := captureApprovalJSON(t, func(w http.ResponseWriter, r *http.Request) {
			writeApprovalOK(w, `{"tasks":[],"has_more":false}`)
		}, func() {
			if _, err := QueryApprovalTasksRaw(ApprovalTaskQueryOptions{Topic: "2", DefinitionCode: "def", StartTimestamp: "1", EndTimestamp: "2"}, userToken); err != nil {
				t.Fatalf("QueryApprovalTasksRaw() error = %v", err)
			}
		})
		if got.Method != http.MethodGet || got.Path != "/open-apis/approval/v4/tasks" {
			t.Fatalf("request = %s %s", got.Method, got.Path)
		}
		if got.Query.Get("topic") != "2" || got.Query.Get("definition_code") != "def" {
			t.Fatalf("query = %v", got.Query)
		}
		if _, ok := got.Query["user_id"]; ok {
			t.Fatalf("user_id query must not exist, got %v", got.Query)
		}
		if got.Auth != "Bearer "+userToken {
			t.Fatalf("auth = %q", got.Auth)
		}
	})
}
