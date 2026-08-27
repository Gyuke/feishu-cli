package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteWikiNodeClient(t *testing.T) {
	deleteCalled := false
	var gotPath string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "DELETE" && r.URL.Path == "/open-apis/wiki/v2/spaces/sp-test/nodes/node-test" {
			deleteCalled = true
			gotPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"task_id":"task-999"}}`)
			return
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	defer server.Close()
	setupTestConfig(t, server.URL)

	taskID, err := DeleteWikiNode("sp-test", "node-test", "wiki", true, "u-token")
	if err != nil {
		t.Fatalf("DeleteWikiNode 失败: %v", err)
	}

	if !deleteCalled {
		t.Fatal("未发起 DELETE 请求")
	}
	if gotPath != "/open-apis/wiki/v2/spaces/sp-test/nodes/node-test" {
		t.Fatalf("请求路径 = %q, 期望 /open-apis/wiki/v2/spaces/sp-test/nodes/node-test", gotPath)
	}
	if gotBody["obj_type"] != "wiki" {
		t.Fatalf("obj_type = %v, 期望 wiki", gotBody["obj_type"])
	}
	if gotBody["include_children"] != true {
		t.Fatalf("include_children = %v, 期望 true", gotBody["include_children"])
	}
	if taskID != "task-999" {
		t.Fatalf("taskID = %q, 期望 task-999", taskID)
	}
}

func TestGetWikiDeleteNodeTaskClient(t *testing.T) {
	pollCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/open-apis/wiki/v2/tasks/task-999" {
			pollCalled = true
			if r.URL.Query().Get("task_type") != "delete_node" {
				http.Error(w, "task_type must be delete_node", http.StatusBadRequest)
				return
			}
			_, _ = fmt.Fprint(w, `{
				"code": 0,
				"msg": "ok",
				"data": {
					"task": {
						"task_id": "task-999",
						"simple_task_result": {
							"status": "success",
							"status_msg": "node deleted successfully"
						}
					}
				}
			}`)
			return
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	defer server.Close()
	setupTestConfig(t, server.URL)

	st, err := GetWikiDeleteNodeTask("task-999", "u-token")
	if err != nil {
		t.Fatalf("GetWikiDeleteNodeTask 失败: %v", err)
	}

	if !pollCalled {
		t.Fatal("未发起 GET 轮询请求")
	}
	if !st.Ready() {
		t.Fatalf("任务状态应当为 Ready")
	}
	if st.Status != "success" {
		t.Fatalf("status = %q, 期望 success", st.Status)
	}
}

func TestCreateWikiNodeShortcutClient(t *testing.T) {
	createCalled := false
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/open-apis/wiki/v2/spaces/sp-test/nodes" {
			createCalled = true
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = fmt.Fprint(w, `{
				"code": 0,
				"msg": "ok",
				"data": {
					"node": {
						"space_id": "sp-test",
						"node_token": "wikcnNewShortcut",
						"obj_token": "docxRealObj",
						"obj_type": "docx",
						"node_type": "shortcut",
						"origin_node_token": "wikcnOriginal"
					}
				}
			}`)
			return
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	defer server.Close()
	setupTestConfig(t, server.URL)

	res, err := CreateWikiNode("sp-test", "快捷方式标题", "parent-1", "docx", "shortcut", "wikcnOriginal", "u-token")
	if err != nil {
		t.Fatalf("CreateWikiNode 失败: %v", err)
	}

	if !createCalled {
		t.Fatal("未发起 POST 请求")
	}
	if gotBody["origin_node_token"] != "wikcnOriginal" {
		t.Fatalf("origin_node_token = %v, 期望 wikcnOriginal", gotBody["origin_node_token"])
	}
	if gotBody["node_type"] != "shortcut" {
		t.Fatalf("node_type = %v, 期望 shortcut", gotBody["node_type"])
	}
	if res.OriginNodeToken != "wikcnOriginal" {
		t.Fatalf("res.OriginNodeToken = %q, 期望 wikcnOriginal", res.OriginNodeToken)
	}
}
