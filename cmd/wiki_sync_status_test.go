package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/riba2534/feishu-cli/internal/wikisync"
)

func TestLoadReconcileStatus(t *testing.T) {
	dir := t.TempDir()
	// 不存在
	if got := loadReconcileStatus(dir); got != nil {
		t.Fatalf("不存在应返回 nil，得 %+v", got)
	}
	// 写入后
	st := struct {
		Baseline   int64 `json:"baseline"`
		Changed    int   `json:"changed"`
		Reexported int   `json:"reexported"`
		Removed    int   `json:"removed"`
	}{
		Baseline: 1788228385, Changed: 2, Reexported: 2, Removed: 1,
	}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "last-reconcile.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	got := loadReconcileStatus(dir)
	if got == nil || !got.Ran || got.Baseline != 1788228385 || got.Removed != 1 || got.Changed != 2 {
		t.Fatalf("解析失败: %+v", got)
	}
}

func TestLoadSubscriptionsStatus(t *testing.T) {
	dir := t.TempDir()
	if got := loadSubscriptionsStatus(dir); got.Ran {
		t.Fatal("不存在应 ran=false")
	}
	st := struct {
		Total      int `json:"total"`
		Subscribed int `json:"subscribed"`
		Failed     int `json:"failed"`
	}{Total: 9, Subscribed: 7, Failed: 0}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "subscriptions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	got := loadSubscriptionsStatus(dir)
	if !got.Ran || got.Total != 9 || got.Subscribed != 7 {
		t.Fatalf("解析失败: %+v", got)
	}
}

func TestLoadChangeStatus(t *testing.T) {
	localDir := t.TempDir()
	dir := wikisync.ChangesDir(localDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	lines := []wikisync.Change{
		{FileToken: "t1", Status: "updated", ChangedAt: "2026-09-01T10:00:00+08:00"},
		{FileToken: "t2", Status: "failed", ChangedAt: "2026-09-01T10:01:00+08:00"},
		{FileToken: "t3", Status: "updated", ChangedAt: "2026-09-01T10:02:00+08:00"},
		{FileToken: "t4", Status: "missing", ChangedAt: "2026-09-01T10:03:00+08:00"},
	}
	var buf []byte
	for _, c := range lines {
		b, _ := json.Marshal(c)
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-09-01.jsonl"), buf, 0600); err != nil {
		t.Fatal(err)
	}
	// 空行/脏行应被跳过
	if err := os.WriteFile(filepath.Join(dir, "2026-09-02.jsonl"), []byte("\nnot-json\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := loadChangeStatus(localDir)
	if got.Records != 4 || got.Updated != 2 || got.Failed != 1 || got.Missing != 1 {
		t.Fatalf("聚合错误: %+v", got)
	}
	if got.Latest != "2026-09-01T10:03:00+08:00" {
		t.Fatalf("latest 应取最大值，得 %s", got.Latest)
	}
}

func TestBuildQueryStatus(t *testing.T) {
	localDir := t.TempDir()
	wikiURL := "https://example.feishu.cn/wiki/WIKINODE"
	q := &wikisync.Query{Name: "q1", WikiURL: wikiURL, LocalDir: localDir, IncludeTypes: []string{"docx", "sheet"}}
	tid := q.TaskID()

	// index：3 条目 belong 本任务（subscribed / failed / gone），1 条属其它任务应被过滤。
	entries := []wikisync.IndexEntry{
		{TaskID: tid, ObjType: "docx", SubscribeStatus: wikisync.SubscribeStatusSubscribed, LocalPath: "a.md"},
		{TaskID: tid, ObjType: "docx", SubscribeStatus: wikisync.SubscribeStatusFailed, LocalPath: "b.md"},
		{TaskID: tid, ObjType: "docx", SyncStatus: "gone", LocalPath: "c.md"},
		{TaskID: "other-task", ObjType: "docx", SubscribeStatus: wikisync.SubscribeStatusSubscribed, LocalPath: "d.md"},
	}
	data, _ := json.Marshal(entries)
	if err := os.MkdirAll(filepath.Dir(q.IndexFilePath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(q.IndexFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}

	// manifest：a.md + 一个 asset。
	if err := wikisync.SaveManifest(&wikisync.Manifest{
		TaskID: tid, WikiURL: wikiURL, LocalDir: localDir,
		Files: []string{"a.md", "assets/a.png"},
	}); err != nil {
		t.Fatal(err)
	}

	row := buildQueryStatus(q) // 任务状态目录不存在 → LastReconcile 零值
	if row.Index.Total != 3 || row.Index.Subscribed != 1 || row.Index.SubscribeFailed != 1 || row.Index.Gone != 1 {
		t.Fatalf("index 统计错误: %+v", row.Index)
	}
	if row.Manifest.TotalFiles != 2 || row.Manifest.Markdown != 1 {
		t.Fatalf("manifest 统计错误: %+v", row.Manifest)
	}
}
