package wikisync

import "testing"

func TestTaskIDStableAndKnown(t *testing.T) {
	// 锁定一个已知值，防止 hash 规则悄悄变化破坏任务 manifest 路径的文件名兼容。
	const want = "4ff5be3aea0a5b2e54bcb03aea479ba3125eccd5daa62447cbc7d48604ae90f6"
	got := TaskID("https://example.feishu.cn/wiki/ABC123")
	if got != want {
		t.Fatalf("TaskID 期望 %s，得到 %s（稳定值变更会破坏 manifest 路径）", want, got)
	}
}

func TestTaskIDStableAcrossCalls(t *testing.T) {
	url := "https://example.feishu.cn/wiki/WIKI_NODE_TOKEN"
	if TaskID(url) != TaskID(url) {
		t.Fatal("TaskID 应对同一输入返回稳定值")
	}
}

func TestTaskIDIndependentOfLocalDir(t *testing.T) {
	// 任务身份只依赖 wiki_url，不依赖 local_dir。
	a := TaskID("https://example.feishu.cn/wiki/WIKI_NODE_TOKEN")
	b := TaskID("https://example.feishu.cn/wiki/WIKI_NODE_TOKEN")
	if a != b {
		t.Fatal("不同 local_dir 的同名任务应具有相同 TaskID")
	}
}

func TestTaskIDDiffersForDifferentURLs(t *testing.T) {
	if TaskID("https://example.feishu.cn/wiki/A") == TaskID("https://example.feishu.cn/wiki/B") {
		t.Fatal("不同 wiki_url 应产生不同 TaskID")
	}
}

func TestTaskIDForSpaceStableAndKnown(t *testing.T) {
	// 锁定一个已知值，防止 space 身份哈希规则悄悄变化破坏 space 任务 manifest 路径的文件名兼容。
	const want = "c1aaf1d2735e4e6839bdcc9cdd1c8e7a9f1bdde34d674256ae8aae0407f17bf4"
	got := TaskIDForSpace("ABC123")
	if got != want {
		t.Fatalf("TaskIDForSpace 期望 %s，得到 %s（稳定值变更会破坏 space 任务 manifest 路径）", want, got)
	}
}

func TestTaskIDForSpaceIsolatedFromWikiURL(t *testing.T) {
	// space 身份带 "space:" 前缀，避免与同字符串的 wiki_url 语义撞出相同 ID。
	if TaskIDForSpace("https://example.feishu.cn/wiki/A") == TaskID("https://example.feishu.cn/wiki/A") {
		t.Fatal("space 身份与 wiki_url 身份应产生不同 TaskID")
	}
}
