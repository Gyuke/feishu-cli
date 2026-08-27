package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// finding 的确切场景：[H1 "部署总览", 重要正文, H2 "部署检查", 待删正文]
func TestDevilDeleteRangeExactScenario(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/children"):
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[
				{"block_id":"b0","block_type":3,"heading1":{"elements":[{"text_run":{"content":"部署总览"}}]}},
				{"block_id":"b1","block_type":2,"text":{"elements":[{"text_run":{"content":"重要正文"}}]}},
				{"block_id":"b2","block_type":4,"heading2":{"elements":[{"text_run":{"content":"部署检查"}}]}},
				{"block_id":"b3","block_type":2,"text":{"elements":[{"text_run":{"content":"待删正文"}}]}}
			],"has_more":false}}`)
		case r.Method == "PUT":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			bodies = append(bodies, b)
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"document":{"revision_id":11}}}`)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()
	initDocUpdateTestConfig(t, server.URL)

	// 无 # 选择器 "部署"
	err := doDeleteRange("doc-z", "部署", "", "", "", -1)
	t.Logf("err = %v", err)
	for i, b := range bodies {
		t.Logf("PUT#%d command=%v start=%v end=%v", i+1, b["command"], b["start_block_id"], b["end_block_id"])
	}

	// 对照：带 # 的 "## 部署"
	bodies = nil
	err = doDeleteRange("doc-z", "## 部署", "", "", "", -1)
	t.Logf("with-hash err = %v", err)
	for i, b := range bodies {
		t.Logf("hash PUT#%d start=%v end=%v", i+1, b["start_block_id"], b["end_block_id"])
	}
}
