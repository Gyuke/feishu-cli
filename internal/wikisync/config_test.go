package wikisync

import (
	"os"
	"strings"
	"testing"
)

const validYAML = `options:
  debounce: 15s
  clean: true
  continue_on_error: true
  download_images: true
  include_types: [docx, sheet]
  expand_sheets: false
  expand_mentions: false
  conflict: skip

queries:
  - name: "api 文档"
    wiki_url: "https://example.feishu.cn/wiki/API_TOKEN"
    local_dir: "./docs/api"
  - name: "最佳实践"
    wiki_url: "https://example.feishu.cn/wiki/BEST_TOKEN"
    local_dir: "./docs/best"
`

func TestParseConfigValid(t *testing.T) {
	cfg, err := ParseConfig([]byte(validYAML))
	if err != nil {
		t.Fatalf("合法配置解析失败: %v", err)
	}
	if len(cfg.Queries) != 2 {
		t.Fatalf("期望 2 个 query，得到 %d", len(cfg.Queries))
	}
	q0 := cfg.Queries[0]
	if q0.Conflict != "skip" {
		t.Errorf("query[0].conflict 应从 options 继承 skip，得到 %q", q0.Conflict)
	}
	if q0.Debounce != "15s" {
		t.Errorf("query[0].debounce 应从 options 继承 15s，得到 %q", q0.Debounce)
	}
	if q0.Clean != true {
		t.Error("query[0].clean 应从 options 继承 true")
	}
	if q0.AssetsDir != "docs/api/assets" {
		t.Errorf("query[0].assets_dir 应默认 local_dir/assets，得到 %q", q0.AssetsDir)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
queries:
  - name: solo
    wiki_url: "https://example.feishu.cn/wiki/SOLO"
    local_dir: "./x"
`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	q := cfg.Queries[0]
	if q.Conflict != "overwrite" {
		t.Errorf("默认 conflict 应为 overwrite，得到 %q", q.Conflict)
	}
	if q.Debounce != "10s" {
		t.Errorf("默认 debounce 应为 10s，得到 %q", q.Debounce)
	}
	if len(q.IncludeTypes) != 2 || q.IncludeTypes[0] != "docx" {
		t.Errorf("默认 include_types 应为 [docx sheet]，得到 %v", q.IncludeTypes)
	}
}

func TestParseConfigValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"空 queries", `queries: []`, "至少需要一个"},
		{"缺 wiki_url", `queries: [{"local_dir": "./x"}]`, "wiki_url"},
		{"缺 local_dir", `queries: [{"wiki_url": "https://e.feishu.cn/wiki/A"}]`, "local_dir"},
		{"非法 conflict", `
queries:
  - name: a
    wiki_url: "https://e.feishu.cn/wiki/A"
    local_dir: "./x"
    conflict: never`, "conflict"},
		{"同目录重复 url", `
queries:
  - name: a
    wiki_url: "https://e.feishu.cn/wiki/SAME"
    local_dir: "./x"
  - name: b
    wiki_url: "https://e.feishu.cn/wiki/SAME"
    local_dir: "./x"`, "重复"},
		{"space 任务同目录重复", `
queries:
  - name: a
    space_id: "7349730005238317084"
    local_dir: "./x"
  - name: b
    space_id: "7349730005238317084"
    local_dir: "./x"`, "重复"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("期望报错，但解析成功")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息 %q 不含 %q", err.Error(), tc.want)
			}
		})
	}
}

func TestQueryIncludesType(t *testing.T) {
	q := Query{IncludeTypes: []string{"docx", "sheet"}}
	if !q.IncludesType("docx") {
		t.Error("应包含 docx")
	}
	if q.IncludesType("bitable") {
		t.Error("不应包含 bitable")
	}
	if !q.IncludesType("DOCX") {
		t.Error("大小写不同也应命中")
	}
}

func TestParseConfigSpaceTask(t *testing.T) {
	// space 任务只填 space_id，wiki_url 可省略（生成节点 URL 时回退 feishu.cn）。
	cfg, err := ParseConfig([]byte(`
queries:
  - name: "整库"
    space_id: "7349730005238317084"
    local_dir: "./docs/whole"
`))
	if err != nil {
		t.Fatalf("space 任务应能解析: %v", err)
	}
	q := cfg.Queries[0]
	if q.SpaceID != "7349730005238317084" {
		t.Errorf("SpaceID 应保留，得到 %q", q.SpaceID)
	}
	if got := q.TaskIdentity(); got != "space:7349730005238317084" {
		t.Errorf("TaskIdentity() 期望 space:7349730005238317084，得到 %q", got)
	}
	if got := q.TaskID(); got != TaskIDForSpace("7349730005238317084") {
		t.Errorf("TaskID() 应等于 TaskIDForSpace，得到 %q", got)
	}
}

func TestQueryTaskIdentity(t *testing.T) {
	node := Query{WikiURL: "https://e.feishu.cn/wiki/A"}
	if node.TaskIdentity() != "https://e.feishu.cn/wiki/A" {
		t.Errorf("单节点任务 TaskIdentity 应为 wiki_url，得到 %q", node.TaskIdentity())
	}
	space := Query{SpaceID: "123"}
	if space.TaskIdentity() != "space:123" {
		t.Errorf("space 任务 TaskIdentity 应为 space:123，得到 %q", space.TaskIdentity())
	}
	if space.TaskID() != TaskIDForSpace("123") {
		t.Errorf("space 任务 TaskID 应等于 TaskIDForSpace，得到 %q", space.TaskID())
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("无法获取用户主目录: %v", err)
	}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空串", "", ""},
		{"仅波浪号", "~", home},
		{"波浪号加斜杠", "~/Documents", home + string(os.PathSeparator) + "Documents"},
		{"相对路径不动", "./docs/api", "./docs/api"},
		{"绝对路径不动", "/data/x", "/data/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandTilde(tc.in); got != tc.want {
				t.Errorf("ExpandTilde(%q) = %q, 期望 %q", tc.in, got, tc.want)
			}
		})
	}
}
