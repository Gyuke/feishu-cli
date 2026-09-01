package cmd

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/riba2534/feishu-cli/internal/output"
	"github.com/riba2534/feishu-cli/internal/wikisync"
	"github.com/spf13/cobra"
)

// wikiSyncStatusCmd 是 wiki sync 的子命令：只读聚合当前配置下各个任务的同步状态。
//
// 不做任何 API 调用、不写文件，只读取本地已有 artifact（索引/manifest/对账状态/订阅汇总/
// 事件变更记录）并汇总输出。适合 cron 巡检前先看一眼"哪些还没同步、订阅是否齐全、对账基线在哪"。
var wikiSyncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "只读汇总各任务的同步状态（索引/订阅/对账基线/事件记录）",
	Long: `只读聚合当前配置下每个任务的同步状态，不调用 API、不写文件：
  - index     : 本地索引条目数、docx 数、订阅成功/失败数、gone（云端已消失）数
  - manifest  : 本任务实际写入的文件数（含 assets）与 Markdown 文件数
  - last_reconcile : 上次对账是否跑过、基线时间戳、该轮变更/重导/移除/失败数
  - events    : .feishu-cli/changes/*.jsonl 里记录的最近事件重导结果（updated/failed/missing）
  - subscriptions : 上次 subscribe 的汇总（该配置全局一份）

全程只读，无需登录态。配合 --format/--jq 可做机器可读过滤（如 --jq '.queries[].events.records'）。`,
	RunE: runWikiSyncStatus,
}

// wikiSyncStatusReport 是 status 的顶层输出结构。
type wikiSyncStatusReport struct {
	ConfigPath    string                      `json:"config"`
	ConfigHash    string                      `json:"config_hash"`
	StateDir      string                      `json:"state_dir"`
	GeneratedAt   string                      `json:"generated_at"`
	Subscriptions wikiSyncSubscriptionsStatus `json:"subscriptions"`
	Queries       []wikiSyncQueryStatusRow    `json:"queries"`
	Totals        wikiSyncStatusTotals        `json:"totals"`
}

// wikiSyncQueryStatusRow 对应单个 query 的状态汇总。
type wikiSyncQueryStatusRow struct {
	QueryName     string                  `json:"name"`
	WikiURL       string                  `json:"wiki_url"`
	LocalDir      string                  `json:"local_dir"`
	Clean         bool                    `json:"clean"`
	IncludeTypes  []string                `json:"include_types"`
	Index         wikiSyncIndexStatus     `json:"index"`
	Manifest      wikiSyncManifestStatus  `json:"manifest"`
	LastReconcile wikiSyncReconcileStatus `json:"last_reconcile"`
	Events        wikiSyncChangeStatus    `json:"events"`
}

// wikiSyncIndexStatus 索引维度统计。
type wikiSyncIndexStatus struct {
	Total           int `json:"total"`
	Docx            int `json:"docx"`
	Subscribed      int `json:"subscribed"`
	SubscribeFailed int `json:"subscribe_failed"`
	Gone            int `json:"gone"`
}

// wikiSyncManifestStatus 清单维度统计。
type wikiSyncManifestStatus struct {
	TotalFiles int `json:"total_files"`
	Markdown   int `json:"markdown"`
}

// wikiSyncReconcileStatus 上次对账基线（来自 last-reconcile.json）。
type wikiSyncReconcileStatus struct {
	Ran        bool   `json:"ran"`
	LastRun    string `json:"last_run"`
	Baseline   int64  `json:"baseline"`
	Changed    int    `json:"changed"`
	Reexported int    `json:"reexported"`
	Removed    int    `json:"removed"`
	Failed     int    `json:"failed"`
}

// wikiSyncChangeStatus 事件驱动重导记录（.feishu-cli/changes/*.jsonl）统计。
type wikiSyncChangeStatus struct {
	Records int    `json:"records"`
	Updated int    `json:"updated"`
	Failed  int    `json:"failed"`
	Missing int    `json:"missing"`
	Latest  string `json:"latest"`
}

// wikiSyncSubscriptionsStatus 全局订阅汇总（stateDir/subscriptions.json）。
type wikiSyncSubscriptionsStatus struct {
	Ran        bool   `json:"ran"`
	LastRun    string `json:"last_run"`
	Total      int    `json:"total"`
	Subscribed int    `json:"subscribed"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
}

// wikiSyncStatusTotals 跨 query 的聚合。
type wikiSyncStatusTotals struct {
	Index           int `json:"index_total"`
	Subscribed      int `json:"subscribed"`
	SubscribeFailed int `json:"subscribe_failed"`
	Gone            int `json:"gone"`
	MarkdownFiles   int `json:"markdown_files"`
	ChangedRecords  int `json:"changed_records"`
}

func runWikiSyncStatus(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".feishu-cli", "wiki-sync.yaml")
	}

	cfg, err := wikisync.LoadConfig(configPath)
	if err != nil {
		return err
	}
	stateDir, cfgHash, err := statusStateDir(configPath)
	if err != nil {
		return err
	}

	report := wikiSyncStatusReport{
		ConfigPath:    configPath,
		ConfigHash:    cfgHash,
		StateDir:      stateDir,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		Subscriptions: loadSubscriptionsStatus(stateDir),
	}
	for i := range cfg.Queries {
		q := &cfg.Queries[i]
		row := buildQueryStatus(q)
		report.Queries = append(report.Queries, row)
		report.Totals.Index += row.Index.Total
		report.Totals.Subscribed += row.Index.Subscribed
		report.Totals.SubscribeFailed += row.Index.SubscribeFailed
		report.Totals.Gone += row.Index.Gone
		report.Totals.MarkdownFiles += row.Manifest.Markdown
		report.Totals.ChangedRecords += row.Events.Records
	}

	o, oerr := output.ParseOptions(cmd)
	if oerr != nil {
		return oerr
	}
	return output.Render(o, report)
}

// buildQueryStatus 聚合单个 query 的状态。
func buildQueryStatus(q *wikisync.Query) wikiSyncQueryStatusRow {
	row := wikiSyncQueryStatusRow{
		QueryName:    q.Name,
		WikiURL:      q.WikiURL,
		LocalDir:     q.LocalDir,
		Clean:        q.Clean,
		IncludeTypes: q.IncludeTypes,
	}

	// index
	if entries, err := wikisync.LoadIndex(q.IndexFilePath()); err == nil {
		for _, e := range entries {
			if e.TaskID != q.TaskID() {
				continue
			}
			row.Index.Total++
			if e.ObjType == "docx" {
				row.Index.Docx++
			}
			switch e.SubscribeStatus {
			case wikisync.SubscribeStatusSubscribed:
				row.Index.Subscribed++
			case wikisync.SubscribeStatusFailed:
				row.Index.SubscribeFailed++
			}
			if e.SyncStatus == "gone" {
				row.Index.Gone++
			}
		}
	}

	// manifest
	if m, merr := wikisync.LoadManifest(q.WikiURL, q.LocalDir); merr == nil && m != nil {
		row.Manifest.TotalFiles = len(m.Files)
		for _, f := range m.Files {
			if strings.HasSuffix(strings.ToLower(f), ".md") {
				row.Manifest.Markdown++
			}
		}
	}

	// 上次对账基线（按任务身份派生目录，与配置文件哈希解耦）
	if st := loadReconcileStatus(taskReconcileStateDir(q)); st != nil {
		row.LastReconcile = *st
	}

	// 事件重导记录
	row.Events = loadChangeStatus(q.LocalDir)

	return row
}

// statusStateDir 只计算配置状态目录路径，不创建任何目录（status 是只读命令）。
// 与 reconcileStateDir 的唯一区别：少一次 MkdirAll。
func statusStateDir(configPath string) (dir, cfgHash string, err error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", "", err
	}
	cfgHash = wikisync.ContentHash(data)
	home, _ := os.UserHomeDir()
	dir = filepath.Join(home, ".feishu-cli", "state", "wiki-sync", cfgHash)
	return dir, cfgHash, nil
}

// loadReconcileStatus 读取状态目录下的 last-reconcile.json；不存在/解析失败返回 nil。
func loadReconcileStatus(stateDir string) *wikiSyncReconcileStatus {
	data, err := os.ReadFile(filepath.Join(stateDir, "last-reconcile.json"))
	if err != nil {
		return nil
	}
	var st wikiSyncReconcileStatus
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	st.Ran = true
	return &st
}

// loadSubscriptionsStatus 读取状态目录下的 subscriptions.json；不存在/解析失败返回"未跑"。
func loadSubscriptionsStatus(stateDir string) wikiSyncSubscriptionsStatus {
	data, err := os.ReadFile(filepath.Join(stateDir, "subscriptions.json"))
	if err != nil {
		return wikiSyncSubscriptionsStatus{}
	}
	var st wikiSyncSubscriptionsStatus
	if json.Unmarshal(data, &st) != nil {
		return wikiSyncSubscriptionsStatus{}
	}
	st.Ran = true
	return st
}

// loadChangeStatus 读取 local_dir/.feishu-cli/changes/*.jsonl，按状态聚合事件重导记录。
func loadChangeStatus(localDir string) wikiSyncChangeStatus {
	var st wikiSyncChangeStatus
	entries, err := os.ReadDir(wikisync.ChangesDir(localDir))
	if err != nil {
		return st
	}
	for _, f := range entries {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		r, err := os.Open(filepath.Join(wikisync.ChangesDir(localDir), f.Name()))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var c wikisync.Change
			if json.Unmarshal([]byte(line), &c) != nil {
				continue
			}
			st.Records++
			switch c.Status {
			case "updated":
				st.Updated++
			case "failed":
				st.Failed++
			case "missing":
				st.Missing++
			}
			if c.ChangedAt > st.Latest {
				st.Latest = c.ChangedAt
			}
		}
		r.Close()
	}
	return st
}

func init() {
	wikiSyncCmd.AddCommand(wikiSyncStatusCmd)
	output.AddOutputFlags(wikiSyncStatusCmd)
}
