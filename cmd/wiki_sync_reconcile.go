package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/wikisync"
	"github.com/spf13/cobra"
)

// wikiSyncReconcileCmd 是 wiki sync 的子命令：基于 obj_edit_time 做增量对账，只重导改过的文档。
//
// 与 watch（常驻长连接）不同，reconcile 是「周期跑一次即可」的兜底：
// 重新枚举整棵知识库，比对每个节点的 obj_edit_time 是否大于上次同步基线，只有变了的节点才定向重导。
// 不依赖「知识库动态」前端页面（那是未公开内部接口，且信息与 obj_edit_time 同源），全部走官方 OpenAPI。
var wikiSyncReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "增量对账：只重导自上次同步后改动过的文档",
	Long: `按配置逐个知识库做增量对账：重新枚举整棵，用每个节点的 obj_edit_time 与
上次同步基线比较，只定向重导「新增/自上次改动」的节点，未变的直接跳过。

适合 cron 每日跑一次，无需常驻进程；watch 则提供秒级实时，两者可共存。

基线判定（--since）：
  - 不写 --since：读上次对账基线（~/.feishu-cli/state/wiki-sync/tasks/<task_hash>/last-reconcile.json，
    按「local_dir + wiki_url」任务身份派生，与配置文件哈希解耦），首次运行无基线则视为全量（全部重导）。
  - --since today        ：以今日 00:00 为界，只重导今天改过的。
  - --since now          ：只重导此刻之后……即一条不漏地全量（常配合 --full 语义用）。
  - --since <unix秒>      ：以上次成功时间戳为界。
  - --since <RFC3339>     ：如 2026-08-25T09:00:00+08:00。

每次成功对账后会把「本次结束时刻」写回基线，下次增量基于它。产物、索引、manifest
的写法与 pull 完全一致，保证目录下各 artifact 保持同步。`,
	RunE: runWikiSyncReconcile,
}

func runWikiSyncReconcile(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".feishu-cli", "wiki-sync.yaml")
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	since, _ := cmd.Flags().GetString("since")

	cfg, err := wikisync.LoadConfig(configPath)
	if err != nil {
		return err
	}

	// 解析显式基线：--since 优先；否则每个任务各自读自己的基线（见 taskReconcileStateDir）。
	hasExplicitSince := cmd.Flags().Changed("since")
	explicitCutoff := int64(0)
	if hasExplicitSince {
		if strings.TrimSpace(since) == "" {
			return fmt.Errorf("--since 需要值：today / now / <unix秒> / <RFC3339>")
		}
		explicitCutoff, err = parseSinceValue(since)
		if err != nil {
			return err
		}
	}

	userAccessToken := resolveOptionalUserTokenWithFallback(cmd)

	ok, failed := 0, 0
	totalChanged, totalReexported, totalRemoved, totalSkipped, totalFailed := 0, 0, 0, 0, 0
	for i := range cfg.Queries {
		q := &cfg.Queries[i]
		// 每个任务用自己的基线（键 = local_dir + wiki_url，与配置文件哈希解耦，
		// 改 clean 等字段不丢增量状态）。取本任务「开始时刻」写回为下次基线，
		// 保证对账期间被编辑的文档其 obj_edit_time > 基线，能在下一轮被捞到。
		baseline := time.Now().Unix()
		cutoff := explicitCutoff
		if !hasExplicitSince {
			cutoff = loadReconcileBaseline(taskReconcileStateDir(q))
		}
		res, err := reconcileOneQuery(q, userAccessToken, cutoff, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[wiki-sync] 任务 %q 失败: %v\n", q.Name, err)
			failed++
			if !q.ContinueOnErr {
				return err
			}
			continue
		}
		totalChanged += res.Changed
		totalReexported += res.Reexported
		totalRemoved += res.Removed
		totalSkipped += res.Skipped
		totalFailed += res.Failed
		fmt.Printf("[wiki-sync] 任务 %q 对账: 变更=%d 重导=%d 移除=%d 未变=%d 跳过=%d 失败=%d\n",
			q.Name, res.Changed, res.Reexported, res.Removed, res.Unchanged, res.Skipped, res.Failed)
		if res.Failed > 0 {
			failed++
		} else {
			ok++
		}
		if !dryRun {
			if err := saveReconcileState(q, baseline, res); err != nil {
				fmt.Fprintf(os.Stderr, "[wiki-sync] 任务 %q 写对账状态失败: %v\n", q.Name, err)
			}
		}
	}

	if !dryRun {
		fmt.Printf("[wiki-sync] 对账完成：变更 %d / 重导 %d / 移除 %d\n",
			totalChanged, totalReexported, totalRemoved)
	}

	if totalFailed > 0 || failed > 0 {
		return fmt.Errorf("wiki sync reconcile 完成但有失败（重导 %d / 失败 %d）", totalReexported, totalFailed)
	}
	return nil
}

// reconcileQueryResult 记录单个 query 的对账统计。
type reconcileQueryResult struct {
	QueryName  string
	Unchanged  int // 已存在且 obj_edit_time <= 基线，未变
	Changed    int // 命中基线（新增/自上次改动/结构变化）
	Gone       int // 云端已消失（删除/移出/回收站）的节点数
	Reexported int // 实际重导成功
	Removed    int // 因 clean 从磁盘+清单剔除的本地文件数
	Skipped    int // 类型不在 include_types 内
	Failed     int // 重导失败
}

// reconcileOneQuery 处理单个 query：枚举 + 与索引基线比对 + 定向重导变更节点 + 写回 artifact。
func reconcileOneQuery(q *wikisync.Query, userAccessToken string, cutoff int64, dryRun bool) (*reconcileQueryResult, error) {
	if err := validateOutputPath(q.LocalDir, ""); err != nil {
		return nil, fmt.Errorf("输出目录不安全: %w", err)
	}
	exportCmd := exportCmdForQuery(q)

	roots, err := resolveQueryRoots(q, userAccessToken)
	if err != nil {
		return nil, err
	}
	jobs, err := collectWikiForest(roots, q.LocalDir, 0, userAccessToken)
	if err != nil {
		return nil, fmt.Errorf("收集子树失败: %w", err)
	}

	existing, err := wikisync.LoadIndex(q.IndexFilePath())
	if err != nil {
		return nil, err
	}
	existingByToken := make(map[string]wikisync.IndexEntry, len(existing))
	for _, e := range existing {
		if e.TaskID == q.TaskID() {
			existingByToken[e.NodeToken] = e
		}
	}

	// 本次枚举到的 token 集合，用于识别「云端已消失」的节点。
	freshByToken := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		freshByToken[job.Node.NodeToken] = struct{}{}
	}

	// 旧 manifest 文件集合（相对 local_dir 的 / 分隔路径）：与后续「权威活集」做差，
	// 识别移动/重命名/删除后遗留的孤儿文件（旧 .md 或旧 assets）。
	oldManifestFiles := map[string]bool{}
	if manifest, merr := wikisync.LoadManifest(q.TaskIdentity(), q.LocalDir); merr == nil && manifest != nil {
		for _, f := range manifest.Files {
			if f != "" {
				oldManifestFiles[f] = true
			}
		}
	}

	res := &reconcileQueryResult{QueryName: q.Name}
	var indexEntries []wikisync.IndexEntry

	// 阶段一：识别「云端已消失」节点（在旧索引里，但本次枚举没再出现）。
	// 可能是被删除、移出本知识库、或进了回收站。
	//   - clean=true ：云端删了本地也删，索引/清单不再保留它。
	//   - clean=false：保留磁盘文件与清单记录，仅把索引条目标记为 gone，供 status 展示。
	for _, e := range existingByToken {
		if _, ok := freshByToken[e.NodeToken]; ok {
			continue
		}
		res.Changed++ // 视为结构变更（删除/移出/回收站）
		res.Gone++
		if q.Clean && isExportableWikiType(e.ObjType, q.IncludeTypes) && e.LocalPath != "" {
			// clean=true 且云端已删：不加入索引，其 .md 会在下方「孤儿清理」中被一并移除。
			continue
		}
		if !q.Clean {
			// 非干净：保留索引条目标记为 gone（保留原 local_path 供反查）。
			e.SyncStatus = "gone"
			indexEntries = append(indexEntries, e)
		}
	}

	// 阶段二：处理本次枚举出的每个节点。
	for _, job := range jobs {
		if !isExportableWikiType(job.Node.ObjType, q.IncludeTypes) {
			res.Skipped++
			continue
		}
		rel, _ := filepath.Rel(q.LocalDir, job.OutputPath)
		rel = filepath.ToSlash(rel)
		e, exists := existingByToken[job.Node.NodeToken]
		if exists && !isNodeChanged(job.Node, e, cutoff) {
			// 未变：不重导正文，但刷新元数据（obj_edit_time/title/parent 等），
			// 让索引始终反映云端最新。同时校正 local_path（目录改名/移动导致的 rel 漂移）。
			res.Unchanged++
			e.SpaceID = job.Node.SpaceID
			e.ObjToken = job.Node.ObjToken
			e.ObjType = job.Node.ObjType
			e.Title = job.Node.Title
			e.ParentNodeToken = job.Node.ParentNodeToken
			e.HasChild = job.Node.HasChild
			e.NodeType = job.Node.NodeType
			e.ObjEditTime = job.Node.ObjEditTime
			e.SyncStatus = "" // 重新出现/正常（曾 gone 的复活）
			e.LocalPath = rel
			indexEntries = append(indexEntries, e)
			continue
		}

		res.Changed++
		if dryRun {
			res.Reexported++
			if exists && e.LocalPath != rel {
				fmt.Printf("[wiki-sync] (dry-run) 将重导(改名/移动) %s → %s\n", e.LocalPath, rel)
			} else {
				fmt.Printf("[wiki-sync] (dry-run) 将重导 %s (%s, obj_edit_time=%s)\n",
					job.Node.Title, job.Node.NodeToken, job.Node.ObjEditTime)
			}
			indexEntries = append(indexEntries, indexEntryFromNode(q, job, rel))
			continue
		}

		if err := os.MkdirAll(filepath.Dir(job.OutputPath), 0700); err != nil {
			res.Failed++
			if !q.ContinueOnErr {
				return res, fmt.Errorf("创建目录失败: %w", err)
			}
			continue
		}
		assetsDirOverride := wikiTreeNodeAssetsDir(exportCmd, q.LocalDir, job)
		markdown, err := exportWikiNodeMarkdown(job.Node, userAccessToken, exportCmd, assetsDirOverride)
		if err != nil {
			res.Failed++
			if !q.ContinueOnErr {
				return res, fmt.Errorf("%s 导出失败: %w", job.Node.Title, err)
			}
			continue
		}
		if assetsDirOverride != "" && markdown != "" {
			markdown = makeImagePathsDocumentRelative(markdown, assetsDirOverride, job.OutputPath)
		}
		if err := os.WriteFile(job.OutputPath, []byte(markdown), 0600); err != nil {
			res.Failed++
			if !q.ContinueOnErr {
				return res, fmt.Errorf("写入文件失败: %w", err)
			}
			continue
		}

		res.Reexported++
		// 改名/移动：新路径已被写入磁盘（导成功后才到这一步），旧路径此时仍在磁盘上无人认领，
		// 交由下方「孤儿清理」按 oldManifest 差集移除（clean=true 才删，避免中途失败丢数据）。
		indexEntries = append(indexEntries, indexEntryFromNode(q, job, rel))
	}

	// 权威活集（liveFiles）：由最终索引 + 磁盘资产推导，是本任务「当前实际持有」的文件清单。
	//   - 每个索引条目对应一个 .md（LocalPath）
	//   - download_images 开启时，各 .md 的资产子目录（与 export 侧 wikiTreeNodeAssetsDir 同规则：assets/<相对路径 stem>）
	// 移动/重命名会让旧 .md 与旧 assets 掉出活集；云端消失（clean=false 保留的 gone）仍在活集里，其 .md/assets 得以保留。
	liveFiles := computeLiveFiles(indexEntries, q.LocalDir, q.AssetsDir, q.DownloadImages)

	// 孤儿 = 旧 manifest 有、权威活集没有。都是本次移动/删除/重命名后不再持有的文件（旧 .md / 旧 assets）。
	//   - clean=true ：物理删除（只删清单记录过的路径，绝不碰用户手工文件）
	//   - clean=false：保留磁盘文件仅供查看，但停止追踪（从清单移除）
	// dry-run 时仅提示，不落盘、不计数。
	for p := range oldManifestFiles {
		if liveFiles[p] {
			continue
		}
		if q.Clean {
			if dryRun {
				fmt.Printf("[wiki-sync] (dry-run) 将删除遗留 %s\n", p)
			} else if removeLocalFile(q.LocalDir, p) {
				res.Removed++
			}
		}
	}

	if !dryRun {
		// 写回：manifest（权威活集，去重排序）、合并索引（仅替换本任务）、legacy map。
		if err := wikisync.SaveManifest(&wikisync.Manifest{
			TaskID:   q.TaskID(),
			WikiURL:  q.WikiURL,
			LocalDir: q.LocalDir,
			Files:    dedupeAndSort(keysOf(liveFiles)),
		}); err != nil {
			return res, err
		}
		if err := mergeIndexForTask(q, indexEntries); err != nil {
			return res, err
		}
	}
	return res, nil
}

// isNodeChanged 判断节点是否命中对账基线（需要重导）。
// 判定顺序：标题/父节点结构变化 → 变更；obj_edit_time 秒级 > 基线 → 变更；解析失败退化为与索引字符串比对。
func isNodeChanged(n *client.WikiNode, e wikisync.IndexEntry, cutoff int64) bool {
	// 结构变化：改名（title）或移动（parent_node_token）通常不会推高 obj_edit_time，
	// 但会改变本地路径，故单独作为变更信号，触发重导 + 路径校正。
	if n.Title != e.Title || n.ParentNodeToken != e.ParentNodeToken {
		return true
	}
	objT := parseObjEditTime(n.ObjEditTime)
	if objT >= 0 {
		return objT > cutoff
	}
	return n.ObjEditTime != e.ObjEditTime
}

// keysOf 返回 map 的所有键（去掉空串），供 manifest 排序用。
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

// removeLocalFile 删除 local_dir 内的一个相对文件；越界或不存在返回 false。
// 保守起见，任何将路径解析到 local_dir 之外的情况都拒绝。
func removeLocalFile(localDir, rel string) bool {
	target := filepath.Join(localDir, filepath.FromSlash(rel))
	base, err := filepath.Rel(localDir, target)
	if err != nil || base == "." || strings.HasPrefix(base, "..") {
		return false
	}
	return os.Remove(target) == nil
}

// assetDirForRel 由相对 local_dir 的 .md 路径推导其图片资产子目录，与 export 侧
// wikiTreeNodeAssetsDir 同规则（assets/<相对路径 stem>，路径一变资产目录跟着变，
// 因此移动文档的旧资产只会留在旧目录，需靠孤儿清理移除）。
func assetDirForRel(assetsDir, rel string) string {
	if assetsDir == "" {
		return ""
	}
	rel = filepath.FromSlash(rel)
	return filepath.Join(assetsDir, strings.TrimSuffix(rel, filepath.Ext(rel)))
}

// computeLiveFiles 由最终索引 + 磁盘资产推导「当前实际持有」的文件集合（相对 local_dir 的 / 分隔路径）。
//   - 每个索引条目对应一个 .md（LocalPath）
//   - download_images 开启时，各 docx 条目的资产子目录（同 export 侧 wikiTreeNodeAssetsDir 规则）
func computeLiveFiles(entries []wikisync.IndexEntry, localDir, assetsDir string, downloadImages bool) map[string]bool {
	liveFiles := map[string]bool{}
	for _, e := range entries {
		if e.LocalPath == "" {
			continue
		}
		rel := filepath.ToSlash(e.LocalPath)
		liveFiles[rel] = true
		if downloadImages && e.ObjType == "docx" {
			for _, asset := range collectAssetFiles(assetDirForRel(assetsDir, e.LocalPath), localDir) {
				if asset != "" {
					liveFiles[asset] = true
				}
			}
		}
	}
	return liveFiles
}

// staleFiles 返回旧清单里有、权威活集里没有的孤儿文件（移动/删除/重命名后遗留的旧 .md 或旧 assets）。
func staleFiles(oldManifestFiles, liveFiles map[string]bool) []string {
	var out []string
	for p := range oldManifestFiles {
		if !liveFiles[p] {
			out = append(out, p)
		}
	}
	return out
}

// parseObjEditTime 解析 obj_edit_time（秒级或毫秒级 unix 字符串，或 RFC3339）为秒级时间戳。
// 解析失败/空串返回 -1。
func parseObjEditTime(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > int64(1e12) {
			return n / 1000 // 毫秒转秒
		}
		return n
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.Unix()
	}
	return -1
}

// parseSinceValue 解析 --since：支持 today / now / unix秒 / RFC3339。
func parseSinceValue(s string) (int64, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return 0, nil
	case strings.EqualFold(s, "today"):
		now := time.Now()
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Unix(), nil
	case strings.EqualFold(s, "now"):
		return time.Now().Unix(), nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix(), nil
	}
	return 0, fmt.Errorf("无法解析 --since %q：支持 today / now / <unix秒> / <RFC3339>", s)
}

// taskReconcileStateDir 返回单个任务的对账基线目录。
// 键 = local_dir + wiki_url（与配置校验里的任务身份一致），与「配置文件哈希」完全解耦：
// 改 wiki-sync.yaml 的任意其它字段（如 clean、加注释）都不会让基线漂移、丢失增量状态。
func taskReconcileStateDir(q *wikisync.Query) string {
	home, _ := os.UserHomeDir()
	key := wikisync.ContentHash([]byte(q.LocalDir + "\x00" + q.TaskID()))
	return filepath.Join(home, ".feishu-cli", "state", "wiki-sync", "tasks", key)
}

func loadReconcileBaseline(stateDir string) int64 {
	data, err := os.ReadFile(filepath.Join(stateDir, "last-reconcile.json"))
	if err != nil {
		return 0
	}
	var st struct {
		Baseline int64 `json:"baseline"`
	}
	if json.Unmarshal(data, &st) != nil {
		return 0
	}
	return st.Baseline
}

func saveReconcileState(q *wikisync.Query, baseline int64, res *reconcileQueryResult) error {
	st := struct {
		TaskID     string `json:"task_id"`
		WikiURL    string `json:"wiki_url"`
		LocalDir   string `json:"local_dir"`
		LastRun    string `json:"last_run"`
		Baseline   int64  `json:"baseline"`
		Changed    int    `json:"changed"`
		Gone       int    `json:"gone"`
		Reexported int    `json:"reexported"`
		Removed    int    `json:"removed"`
		Skipped    int    `json:"skipped"`
		Failed     int    `json:"failed"`
	}{
		TaskID:     q.TaskID(),
		WikiURL:    q.WikiURL,
		LocalDir:   q.LocalDir,
		LastRun:    time.Now().Format(time.RFC3339),
		Baseline:   baseline,
		Changed:    res.Changed,
		Gone:       res.Gone,
		Reexported: res.Reexported,
		Removed:    res.Removed,
		Skipped:    res.Skipped,
		Failed:     res.Failed,
	}
	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	dir := taskReconcileStateDir(q)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "last-reconcile.json"), append(out, '\n'), 0600)
}

func init() {
	wikiSyncCmd.AddCommand(wikiSyncReconcileCmd)
	wikiSyncReconcileCmd.Flags().String("since", "", `对账基线：today / now / <unix秒> / <RFC3339>（默认读上次存盘基线）`)
	wikiSyncReconcileCmd.Flags().String("user-access-token", "", "User Access Token（可选；默认优先登录态，失败回退 App Token）")
}
