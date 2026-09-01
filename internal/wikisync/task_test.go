package wikisync

import "testing"

func TestTaskIDStableAndKnown(t *testing.T) {
	// 锁定一个已知值，防止 hash 规则悄悄变化破坏 legacy `.map.json` 文件名兼容。
	const want = "4ff5be3aea0a5b2e54bcb03aea479ba3125eccd5daa62447cbc7d48604ae90f6"
	got := TaskID("https://example.feishu.cn/wiki/ABC123")
	if got != want {
		t.Fatalf("TaskID 期望 %s，得到 %s（稳定值变更会破坏脚本兼容）", want, got)
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
