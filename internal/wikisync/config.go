package wikisync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Options 是多个 query 共享的默认设置，单个 query 可覆盖部分字段。
type Options struct {
	Debounce        string   `yaml:"debounce"`
	Clean           bool     `yaml:"clean"`
	ContinueOnError bool     `yaml:"continue_on_error"`
	DownloadImages  bool     `yaml:"download_images"`
	IncludeTypes    []string `yaml:"include_types"`
	ExpandSheets    bool     `yaml:"expand_sheets"`
	ExpandMentions  bool     `yaml:"expand_mentions"`
	Conflict        string   `yaml:"conflict"`
}

// Query 描述一个知识库同步任务（一个 wiki_url 到 local_dir 的镜像）。
type Query struct {
	Name           string   `yaml:"name"`
	WikiURL        string   `yaml:"wiki_url"`
	LocalDir       string   `yaml:"local_dir"`
	AssetsDir      string   `yaml:"assets_dir"`
	IncludeTypes   []string `yaml:"include_types"`
	Conflict       string   `yaml:"conflict"`
	Clean          bool     `yaml:"clean"`
	Debounce       string   `yaml:"debounce"`
	IndexFile      string   `yaml:"index_file"`
	DownloadImages bool     `yaml:"download_images"`
	ExpandSheets   bool     `yaml:"expand_sheets"`
	ExpandMentions bool     `yaml:"expand_mentions"`
	ContinueOnErr  bool     `yaml:"continue_on_error"`
}

// Config 是 wiki-sync.yaml 的顶层结构。
type Config struct {
	Options Options `yaml:"options"`
	Queries []Query `yaml:"queries"`
}

// LoadConfig 读取并校验 YAML 配置，填好默认值后返回。
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("解析配置 %s 失败: %w", path, err)
	}
	return cfg, nil
}

// ParseConfig 解析配置字节并校验、填充默认值。
func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.normalizeDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// normalizeDefaults 把 options 的默认值下推给每个 query，并补齐 query 内可默认字段。
func (c *Config) normalizeDefaults() {
	opt := c.Options
	for i := range c.Queries {
		q := &c.Queries[i]
		q.Name = strings.TrimSpace(q.Name)
		if q.Name == "" {
			q.Name = fmt.Sprintf("query-%d", i+1)
		}
		q.WikiURL = strings.TrimSpace(q.WikiURL)
		q.LocalDir = ExpandTilde(strings.TrimSpace(q.LocalDir))

		// assets_dir 是 query 级别字段，默认 local_dir/assets（options 层不提供）。
		if q.AssetsDir == "" {
			q.AssetsDir = filepath.Join(q.LocalDir, "assets")
		}
		q.AssetsDir = ExpandTilde(q.AssetsDir)
		if len(q.IncludeTypes) == 0 {
			if len(opt.IncludeTypes) > 0 {
				q.IncludeTypes = opt.IncludeTypes
			} else {
				q.IncludeTypes = []string{"docx", "sheet"}
			}
		}
		if q.Conflict == "" {
			if opt.Conflict != "" {
				q.Conflict = opt.Conflict
			} else {
				q.Conflict = "overwrite"
			}
		}
		if q.Debounce == "" {
			if opt.Debounce != "" {
				q.Debounce = opt.Debounce
			} else {
				q.Debounce = "10s"
			}
		}
		if !q.Clean {
			q.Clean = opt.Clean
		}
		q.DownloadImages = opt.DownloadImages
		q.ExpandSheets = opt.ExpandSheets
		q.ExpandMentions = opt.ExpandMentions
		q.ContinueOnErr = opt.ContinueOnError
	}
}

// ExpandTilde 把路径开头的 `~`（或 `~/`）展开为用户主目录。
// 配置文件里的路径不会经过 shell 展开，`local_dir: ~/Documents/...` 若不加处理会被
// 当作字面量目录名（`filepath.Abs` 会相对 cwd 解析），因此这里显式展开 `~`。
// 若 `~` 后面不是 `/` 或 `\`（如 `~user`），保持原样，交由后续逻辑自行判断。
func ExpandTilde(p string) string {
	if p == "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// Validate 校验配置，返回中文错误。
func (c *Config) Validate() error {
	if len(c.Queries) == 0 {
		return fmt.Errorf("queries 至少需要一个条目")
	}
	seen := make(map[string]bool)
	for i := range c.Queries {
		q := &c.Queries[i]
		if q.WikiURL == "" {
			return fmt.Errorf("queries[%d].wiki_url 必须填写", i)
		}
		if q.LocalDir == "" {
			return fmt.Errorf("queries[%d].local_dir 必须填写", i)
		}
		switch q.Conflict {
		case "fail", "skip", "overwrite":
		default:
			return fmt.Errorf("queries[%d].conflict 必须是 fail、skip 或 overwrite（当前：%q）", i, q.Conflict)
		}
		// 同 local_dir 下不允许重复的 wiki_url（任务身份唯一）；不同 local_dir 可重复。
		key := q.LocalDir + "\x00" + TaskID(q.WikiURL)
		if seen[key] {
			return fmt.Errorf("queries[%d] 与其它 query 重复：同一 local_dir 下 wiki_url 重复（%s）", i, q.WikiURL)
		}
		seen[key] = true
	}
	return nil
}

// TaskID 计算任务的稳定身份标识（见 task.go）。
func (q *Query) TaskID() string {
	return TaskID(q.WikiURL)
}

// IndexFilePath 返回该 query 的合并索引路径（相对 local_dir）。
func (q *Query) IndexFilePath() string {
	if q.IndexFile != "" {
		return filepath.Join(q.LocalDir, q.IndexFile)
	}
	return filepath.Join(q.LocalDir, ".feishu-cli", "wiki-index.json")
}

// IncludesType 判断该 query 是否导出指定 obj_type。
func (q *Query) IncludesType(objType string) bool {
	for _, t := range q.IncludeTypes {
		if strings.EqualFold(strings.TrimSpace(t), objType) {
			return true
		}
	}
	return false
}
