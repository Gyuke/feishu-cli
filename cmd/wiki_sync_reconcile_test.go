package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/wikisync"
)

func TestParseSinceValue(t *testing.T) {
	t.Run("空串返回0", func(t *testing.T) {
		if v, err := parseSinceValue(""); err != nil || v != 0 {
			t.Fatalf("空串应得 0，得 %d (%v)", v, err)
		}
	})
	t.Run("unix秒", func(t *testing.T) {
		if v, err := parseSinceValue("1787637299"); err != nil || v != 1787637299 {
			t.Fatalf("应得 1787637299，得 %d (%v)", v, err)
		}
	})
	t.Run("today为今日0点", func(t *testing.T) {
		v, err := parseSinceValue("today")
		if err != nil {
			t.Fatalf("today 解析失败: %v", err)
		}
		now := time.Now()
		y, m, d := now.Date()
		want := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Unix()
		if v != want {
			t.Errorf("today 应得 %d，得 %d", want, v)
		}
	})
	t.Run("现在", func(t *testing.T) {
		before := time.Now().Unix()
		v, err := parseSinceValue("now")
		if err != nil {
			t.Fatalf("now 解析失败: %v", err)
		}
		if v < before || v > time.Now().Unix() {
			t.Errorf("now 应在 [%d, %d] 内，得 %d", before, time.Now().Unix(), v)
		}
	})
	t.Run("非法值报错", func(t *testing.T) {
		if _, err := parseSinceValue("not-a-date"); err == nil {
			t.Fatal("非法值应报错")
		}
	})
	t.Run("RFC3339", func(t *testing.T) {
		v, err := parseSinceValue("2026-08-25T09:30:00+08:00")
		if err != nil {
			t.Fatalf("RFC3339 解析失败: %v", err)
		}
		want := time.Date(2026, 8, 25, 9, 30, 0, 0, time.FixedZone("", 8*3600)).Unix()
		if v != want {
			t.Errorf("RFC3339 应得 %d，得 %d", want, v)
		}
	})
}

func TestParseObjEditTime(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
	}{
		{"空串-1", "", -1},
		{"秒级", "1787637299", 1787637299},
		{"毫秒级", "1787637299000", 1787637299},
		{"RFC3339", "2026-08-25T10:00:00+08:00", time.Date(2026, 8, 25, 10, 0, 0, 0, time.FixedZone("", 8*3600)).Unix()},
		{"格式化" + "按UTC解析", "2026-08-25 10:00:00", time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC).Unix()}, // 无时区信息按 UTC 兜底
		{"乱串-1", "abc", -1},
		{"前后空格", " 1787637299 ", 1787637299},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseObjEditTime(tc.in); got != tc.want {
				t.Errorf("parseObjEditTime(%q) = %d, 期望 %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestComputeLiveFiles(t *testing.T) {
	localDir := t.TempDir()
	assetsDir := filepath.Join(localDir, "assets")
	// 资产子目录与 export 侧一致：assets/<相对路径 stem>（含子目录）。a.md→assets/a，b/b.md→assets/b/b。
	os.MkdirAll(filepath.Join(assetsDir, "a"), 0700)
	os.MkdirAll(filepath.Join(assetsDir, "b", "b"), 0700)
	os.WriteFile(filepath.Join(assetsDir, "a", "p1.png"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(assetsDir, "b", "b", "p2.jpg"), []byte("x"), 0600)

	entries := []wikisync.IndexEntry{
		{NodeToken: "n1", ObjType: "docx", LocalPath: "a.md"},
		{NodeToken: "n2", ObjType: "docx", LocalPath: "b/b.md"},
		{NodeToken: "n3", ObjType: "sheet", LocalPath: "c.csv"}, // 非 docx 不下载图片
		{NodeToken: "n4", ObjType: "docx", LocalPath: ""},       // 空路径跳过
	}

	t.Run("开启下载图片", func(t *testing.T) {
		got := computeLiveFiles(entries, localDir, assetsDir, true)
		if !got["a.md"] || !got["b/b.md"] || !got["c.csv"] {
			t.Fatalf("应包含全部 .md: %+v", got)
		}
		if !got["assets/a/p1.png"] || !got["assets/b/b/p2.jpg"] {
			t.Fatalf("docx 资产应并入活集: %+v", got)
		}
		if len(got) != 5 {
			t.Fatalf("活集应为 {a.md,b/b.md,c.csv,assets/a/p1.png,assets/b/b/p2.jpg} 共 5 项，得 %d: %+v", len(got), got)
		}
	})

	t.Run("关闭下载图片不并入资产", func(t *testing.T) {
		got := computeLiveFiles(entries, localDir, assetsDir, false)
		if got["assets/a/p1.png"] || got["assets/b/b/p2.jpg"] {
			t.Fatalf("关闭下载图片时不应并入资产: %+v", got)
		}
		if len(got) != 3 {
			t.Fatalf("应只含 3 个 .md，得 %d: %+v", len(got), got)
		}
	})
}

func TestStaleFiles(t *testing.T) {
	old := map[string]bool{"a.md": true, "assets/a/p1.png": true, "gone.md": true}
	live := map[string]bool{"a.md": true, "assets/a/p1.png": true}
	stale := staleFiles(old, live)
	if len(stale) != 1 || stale[0] != "gone.md" {
		t.Fatalf("孤儿应只有 gone.md，得 %v", stale)
	}
}

func TestIsNodeChanged(t *testing.T) {
	base := int64(1787637200)

	mk := func(edit string) *client.WikiNode {
		return &client.WikiNode{NodeToken: "n1", ObjToken: "t1", Title: "t", ObjEditTime: edit}
	}
	entry := wikisync.IndexEntry{NodeToken: "n1", ObjEditTime: "1787637299", Title: "t"}

	cases := []struct {
		name string
		node *client.WikiNode
		want bool
	}{
		{"编辑时间晚于基线-变更", mk("1787637299"), true},
		{"编辑时间早于基线-未变", mk("1787637000"), false},
		{"等于基线-未变", mk("1787637200"), false},
		{"解析失败退化为字符串比较(不同)-变更", mk("999999-01-01"), true},
		{"标题不同-结构变更(编辑时间未变)", mk("1787637000"), true},  // 改名：obj_edit_time 不推高也要重导
		{"父节点不同-结构变更(编辑时间未变)", mk("1787637000"), true}, // 移动：同上重导 + 路径校正
	}
	cases[4].node.Title = "t2"
	cases[5].node.ParentNodeToken = "parent2"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNodeChanged(tc.node, entry, base); got != tc.want {
				t.Errorf("isNodeChanged = %v, 期望 %v", got, tc.want)
			}
		})
	}
}
