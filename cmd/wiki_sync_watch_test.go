package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/riba2534/feishu-cli/internal/wikisync"
)

func TestParseDebounce(t *testing.T) {
	cases := map[string]time.Duration{
		"":        10 * time.Second,
		"10s":     10 * time.Second,
		"500ms":   500 * time.Millisecond,
		"invalid": 10 * time.Second,
		"-1s":     10 * time.Second,
		"0s":      10 * time.Second,
	}
	for in, want := range cases {
		if got := parseDebounce(in); got != want {
			t.Fatalf("parseDebounce(%q) = %v, want %v", in, got, want)
		}
	}
}

func editEventBody(token, ftype string) []byte {
	return []byte(`{"schema":"2.0","header":{"event_id":"ev1","event_type":"drive.file.edit_v1"},
	  "event":{"file_type":"` + ftype + `","file_token":"` + token + `"}}`)
}

// TestOnEventRouting 验证 edit_v1 会把事件按 file_token 路由到对应 query 的去抖器，
// 而非 docx 的事件与未跟踪的 token 都被忽略。
func TestOnEventRouting(t *testing.T) {
	var fired []string
	spy := wikisync.NewDebouncer(time.Second, func(token string, _ wikisync.EditEvent) {
		fired = append(fired, token)
	})
	wq := &watchQuery{
		q:         &wikisync.Query{Name: "q1", LocalDir: "/tmp/wiki"},
		debouncer: spy,
		targets:   map[string][]wikisync.IndexEntry{"doc_a": {{ObjToken: "doc_a"}}},
	}
	r := &wikiWatchRun{
		queries:      []*watchQuery{wq},
		tokenQueries: map[string][]*watchQuery{"doc_a": {wq}},
	}

	// 命中：docx 事件应被聚合。
	if err := r.onEvent(context.Background(), "drive.file.edit_v1", editEventBody("doc_a", "docx")); err != nil {
		t.Fatalf("onEvent 失败: %v", err)
	}
	// 未命中：非 docx 忽略；未跟踪 token 忽略。
	if err := r.onEvent(context.Background(), "drive.file.edit_v1", editEventBody("sht_b", "sheet")); err != nil {
		t.Fatalf("onEvent(sheet) 失败: %v", err)
	}
	if err := r.onEvent(context.Background(), "drive.file.edit_v1", editEventBody("doc_untracked", "docx")); err != nil {
		t.Fatalf("onEvent(untracked) 失败: %v", err)
	}

	if n := spy.Drain(time.Now().Add(2 * time.Second)); n != 1 {
		t.Fatalf("应触发 1 次去抖，实际 %d", n)
	}
	if len(fired) != 1 || fired[0] != "doc_a" {
		t.Fatalf("路由异常: %v", fired)
	}
}
