package wikisync

import (
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
