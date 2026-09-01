package wikisync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".feishu-cli", "wiki-index.json")

	entries := []IndexEntry{
		{
			TaskID:           TaskID("https://e.feishu.cn/wiki/A"),
			SpaceID:          "SPACE1",
			NodeToken:        "A",
			ObjToken:         "doccn_a",
			ObjType:          "docx",
			Title:            "示例文档",
			HasChild:         false,
			NodeType:         "origin",
			ObjEditTime:      "1700000000",
			WikiURL:          "https://e.feishu.cn/wiki/A",
			LocalPath:        "子目录/示例文档.md",
			SubscribeStatus:  SubscribeStatusSubscribed,
			LastSubscribedAt: "2026-08-27T10:00:00+08:00",
		},
	}

	if err := SaveIndex(path, entries); err != nil {
		t.Fatalf("SaveIndex 失败: %v", err)
	}
	got, err := LoadIndex(path)
	if err != nil {
		t.Fatalf("LoadIndex 失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("期望 1 条，得到 %d", len(got))
	}
	if got[0].ObjToken != "doccn_a" || got[0].SubscribeStatus != "subscribed" {
		t.Errorf("round-trip 字段不符: %+v", got[0])
	}
}

func TestLoadIndexMissingReturnsEmpty(t *testing.T) {
	got, err := LoadIndex(filepath.Join(t.TempDir(), "no-such.json"))
	if err != nil {
		t.Fatalf("文件不存在应返回空而非报错: %v", err)
	}
	if got != nil {
		t.Fatalf("期望空切片，得到 %v", got)
	}
}

func TestFindByObjToken(t *testing.T) {
	entries := []IndexEntry{
		{ObjToken: "doccn_x", LocalPath: "x.md"},
		{ObjToken: "doccn_y", LocalPath: "y.md"},
	}
	e, ok := FindByObjToken(entries, "doccn_y")
	if !ok || e.LocalPath != "y.md" {
		t.Fatalf("应按 obj_token 命中 y，得到 ok=%v e=%+v", ok, e)
	}
	if _, ok := FindByObjToken(entries, "doccn_z"); ok {
		t.Fatal("不应命中不存在的 obj_token")
	}
}

func TestIndexSaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "wiki-index.json")
	if err := SaveIndex(path, nil); err != nil {
		t.Fatalf("SaveIndex 应自动创建父目录: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("索引文件未生成: %v", err)
	}
}
