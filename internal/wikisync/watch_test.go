package wikisync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func editBody(token, etype, ft string) []byte {
	return []byte(`{
	  "schema": "2.0",
	  "header": {"event_id": "ev123", "event_type": "` + etype + `", "create_time": "1787637299000"},
	  "event": {
	    "file_type": "` + ft + `",
	    "file_token": "` + token + `",
	    "operator_id_list": [{"open_id": "ou_x"}, {"open_id": "ou_y"}],
	    "subscriber_id_list": [{"open_id": "ou_z"}]
	  }
	}`)
}

func TestParseEditEvent(t *testing.T) {
	ev, err := ParseEditEvent(editBody("doccn_abc", "drive.file.edit_v1", "docx"))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if ev.EventID != "ev123" || ev.EventType != "drive.file.edit_v1" || ev.FileToken != "doccn_abc" {
		t.Fatalf("字段解析异常: %+v", ev)
	}
	if ev.FileType != "docx" {
		t.Fatalf("FileType 期望 docx，实际 %q", ev.FileType)
	}
	if len(ev.OperatorIDList) != 2 || ev.OperatorIDList[0] != "ou_x" {
		t.Fatalf("OperatorIDList 异常: %+v", ev.OperatorIDList)
	}
}

func TestParseEditEventMissingFileToken(t *testing.T) {
	_, err := ParseEditEvent([]byte(`{"schema":"2.0","header":{"event_type":"drive.file.edit_v1"},"event":{"file_type":"docx"}}`))
	if err == nil || !strings.Contains(err.Error(), "file_token") {
		t.Fatalf("缺 file_token 应报错，实际 %v", err)
	}
}

func TestDebouncerAggregates(t *testing.T) {
	t0 := time.Unix(1700000000, 0)
	fired := []string{}
	d := NewDebouncer(10*time.Second, func(token string, _ EditEvent) {
		fired = append(fired, token)
	})

	// 同一 token 连续两次编辑（窗口内），只在安静后触发一次。
	d.Add(EditEvent{FileToken: "A"}, t0)
	d.Add(EditEvent{FileToken: "A"}, t0.Add(2*time.Second))
	if n := d.Drain(t0.Add(5 * time.Second)); n != 0 {
		t.Fatalf("窗口未到不应触发，实际触发 %d", n)
	}
	// 最后一次编辑在 t0+2s，需再静默 10s（到 t0+12s）才触发。
	if n := d.Drain(t0.Add(12 * time.Second)); n != 1 {
		t.Fatalf("窗口到后应触发 1 次，实际 %d", n)
	}
	if len(fired) != 1 || fired[0] != "A" {
		t.Fatalf("累积触发异常: %v", fired)
	}

	// 触发后清空，再次编辑可重新触发。
	d.Add(EditEvent{FileToken: "A"}, t0.Add(12*time.Second))
	if n := d.Drain(t0.Add(22 * time.Second)); n != 1 {
		t.Fatalf("重新编辑应再次触发，实际 %d", n)
	}
	if len(fired) != 2 {
		t.Fatalf("应累积 2 次触发，实际 %d", len(fired))
	}
}

func TestAppendChange(t *testing.T) {
	dir := t.TempDir()
	c := Change{
		FileToken: "doc_1", ObjToken: "doc_1", NodeToken: "node_1",
		LocalPath: "SOP.md", Title: "SOP", EventID: "ev1", Status: "updated",
	}
	if err := AppendChange(dir, c); err != nil {
		t.Fatalf("AppendChange 失败: %v", err)
	}
	if err := AppendChange(dir, Change{FileToken: "doc_2", Status: "failed", Error: "boom"}); err != nil {
		t.Fatalf("AppendChange 第二次失败: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(ChangesDir(dir), "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("应有一个 jsonl 文件，实际 %v (err=%v)", files, err)
	}
	data, _ := os.ReadFile(files[0])
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("应有两行记录，实际 %d", len(lines))
	}
	if !strings.Contains(lines[0], `"status":"updated"`) {
		t.Fatalf("第一行状态异常: %s", lines[0])
	}
}
