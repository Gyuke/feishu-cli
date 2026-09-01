package wikisync

import (
	"path/filepath"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		TaskID:   TaskID("https://e.feishu.cn/wiki/A"),
		WikiURL:  "https://e.feishu.cn/wiki/A",
		LocalDir: dir,
		Files:    []string{"子目录/示例文档.md", "assets/x.png"},
	}
	if err := SaveManifest(m); err != nil {
		t.Fatalf("SaveManifest 失败: %v", err)
	}

	got, err := LoadManifest(m.WikiURL, dir)
	if err != nil {
		t.Fatalf("LoadManifest 失败: %v", err)
	}
	if got == nil {
		t.Fatal("期望读到 manifest，得到 nil")
	}
	if got.TaskID != m.TaskID || len(got.Files) != 2 {
		t.Errorf("round-trip 不符: %+v", got)
	}
}

func TestLoadManifestMissingReturnsNil(t *testing.T) {
	got, err := LoadManifest("https://e.feishu.cn/wiki/NOEXIST", t.TempDir())
	if err != nil {
		t.Fatalf("文件不存在应返回 nil 而非报错: %v", err)
	}
	if got != nil {
		t.Fatalf("期望 nil，得到 %+v", got)
	}
}

func TestManifestPathLayout(t *testing.T) {
	m := &Manifest{TaskID: "abc", LocalDir: "/tmp/w"}
	want := filepath.Join("/tmp/w", ".feishu-cli", "manifests", "abc.json")
	if m.ManifestPath() != want {
		t.Fatalf("ManifestPath 布局不符: %s != %s", m.ManifestPath(), want)
	}
}
