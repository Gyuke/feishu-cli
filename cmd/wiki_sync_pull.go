package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/wikisync"
	"github.com/spf13/cobra"
)

// wikiSyncPullCmd 是 wiki sync 的子命令：全量拉取并根据配置生成索引/manifest。
var wikiSyncPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "全量拉取知识库并生成索引与任务 manifest",
	Long: `按配置逐个拉取知识库到本地目录，并生成：

  <local_dir>/.feishu-cli/wiki-index.json      合并索引（RAG 来源 URL 查询）
  <local_dir>/.feishu-cli/manifests/<hash>.json  单任务文件清单（clean 用）
  <local_dir>/.feishu-cli-manifests/<hash>.map.json  legacy 映射（脚本回退通道）

复用 wiki export-tree 的遍历与导出能力，只做一次遍历。`,
	RunE: runWikiSyncPull,
}

func runWikiSyncPull(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".feishu-cli", "wiki-sync.yaml")
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	cfg, err := wikisync.LoadConfig(configPath)
	if err != nil {
		return err
	}
	userAccessToken := resolveOptionalUserTokenWithFallback(cmd)

	total, ok, failed := 0, 0, 0
	for i := range cfg.Queries {
		q := &cfg.Queries[i]
		total++
		res, err := pullOneQuery(q, userAccessToken, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[wiki-sync] 任务 %q 失败: %v\n", q.Name, err)
			failed++
			if !q.ContinueOnErr {
				return err
			}
			continue
		}
		if res.Failed > 0 {
			failed++
		} else {
			ok++
		}
		fmt.Printf("[wiki-sync] 任务 %q 完成: 导出=%d 跳过=%d 失败=%d\n", q.Name, res.Exported, res.Skipped, res.Failed)
	}

	if failed > 0 {
		return fmt.Errorf("wiki sync pull 完成但有任务失败（成功 %d / 失败 %d）", ok, failed)
	}
	return nil
}

// pullQueryResult 记录单个 query 的导出结果统计。
type pullQueryResult struct {
	QueryName string
	Exported  int
	Skipped   int
	Failed    int
}

// pullOneQuery 处理单个 query：遍历 + 导出 + 写索引/manifest/legacy map。
// 复用 export_tree.go 同包函数，避免重复实现遍历与转换逻辑。
func pullOneQuery(q *wikisync.Query, userAccessToken string, dryRun bool) (*pullQueryResult, error) {
	rootToken, err := extractWikiToken(q.WikiURL)
	if err != nil {
		return nil, err
	}
	if err := validateOutputPath(q.LocalDir, ""); err != nil {
		return nil, fmt.Errorf("输出目录不安全: %w", err)
	}

	// 用与 export-tree 相同的导出函数构建 cmd，并把 query 配置注入 flag。
	exportCmd := exportCmdForQuery(q)

	if dryRun {
		fmt.Printf("[wiki-sync] (dry-run) 将导出 %s → %s（include: %v）\n", q.WikiURL, q.LocalDir, q.IncludeTypes)
		return &pullQueryResult{QueryName: q.Name}, nil
	}

	fmt.Printf("[wiki-sync] 解析根节点 %s\n", rootToken)
	root, err := client.GetWikiNode(rootToken, userAccessToken)
	if err != nil {
		return nil, fmt.Errorf("获取根节点失败: %w", err)
	}

	jobs, err := collectWikiTree(root, q.LocalDir, 0, userAccessToken)
	if err != nil {
		return nil, fmt.Errorf("收集子树失败: %w", err)
	}
	fmt.Printf("[wiki-sync] %s：共 %d 个节点，开始导出\n", q.Name, len(jobs))

	res := &pullQueryResult{QueryName: q.Name}
	var indexEntries []wikisync.IndexEntry
	var files []string

	for _, job := range jobs {
		if !isExportableWikiType(job.Node.ObjType, q.IncludeTypes) {
			res.Skipped++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(job.OutputPath), 0700); err != nil {
			res.Failed++
			if !q.ContinueOnErr {
				return res, fmt.Errorf("创建目录失败: %w", err)
			}
			continue
		}
		rel, _ := filepath.Rel(q.LocalDir, job.OutputPath)

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

		res.Exported++
		files = append(files, filepath.ToSlash(rel))
		files = append(files, collectAssetFiles(assetsDirOverride, q.LocalDir)...)
		indexEntries = append(indexEntries, indexEntryFromNode(q, job, rel))
	}

	// 写单任务 manifest（先排序去重，保证稳定输出）。
	manifest := &wikisync.Manifest{
		TaskID:   q.TaskID(),
		WikiURL:  q.WikiURL,
		LocalDir: q.LocalDir,
		Files:    dedupeAndSort(files),
	}
	if err := wikisync.SaveManifest(manifest); err != nil {
		return res, err
	}

	// 写合并索引：只替换本任务条目，保留其它任务。
	if err := mergeIndexForTask(q, indexEntries); err != nil {
		return res, err
	}

	// 写 legacy map：让 scripts/wiki_event_watch.sh 回退通道继续可用。
	if err := writeLegacyMap(q, indexEntries); err != nil {
		return res, err
	}

	return res, nil
}

// indexEntryFromNode 用内存里的 *WikiNode 组装索引项（含完整元数据字段）。
func indexEntryFromNode(q *wikisync.Query, job treeJob, rel string) wikisync.IndexEntry {
	return wikisync.IndexEntry{
		TaskID:          q.TaskID(),
		SpaceID:         job.Node.SpaceID,
		NodeToken:       job.Node.NodeToken,
		ObjToken:        job.Node.ObjToken,
		ObjType:         job.Node.ObjType,
		Title:           job.Node.Title,
		ParentNodeToken: job.Node.ParentNodeToken,
		HasChild:        job.Node.HasChild,
		NodeType:        job.Node.NodeType,
		ObjEditTime:     job.Node.ObjEditTime,
		WikiURL:         buildWikiNodeURL(job.Node.NodeToken, q.WikiURL),
		LocalPath:       filepath.ToSlash(rel),
	}
}

// exportCmdForQuery 构造一个仅含配置值的 c *cobra.Command，供导出函数读取 flag。
// export-tree 的导出函数通过 cmd.Flags() 读 download-images/assets-dir/expand-*。
func exportCmdForQuery(q *wikisync.Query) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("assets-dir", q.AssetsDir, "")
	c.Flags().Bool("download-images", q.DownloadImages, "")
	c.Flags().Bool("expand-sheets", q.ExpandSheets, "")
	c.Flags().Bool("expand-mentions", q.ExpandMentions, "")
	return c
}

// mergeIndexForTask 读取合并索引，去掉本任务旧条目后并入新条目再写回。
func mergeIndexForTask(q *wikisync.Query, newEntries []wikisync.IndexEntry) error {
	indexPath := q.IndexFilePath()
	existing, err := wikisync.LoadIndex(indexPath)
	if err != nil {
		return err
	}
	taskID := q.TaskID()
	kept := make([]wikisync.IndexEntry, 0, len(existing)+len(newEntries))
	for _, e := range existing {
		if e.TaskID != taskID {
			kept = append(kept, e)
		}
	}
	return wikisync.SaveIndex(indexPath, append(kept, newEntries...))
}

// writeLegacyMap 写出与 scripts/wiki_export_batch.sh / wiki_event_watch.sh 兼容的映射文件。
// 键文件名 = sha256(wiki_url)，与该脚本一致。
func writeLegacyMap(q *wikisync.Query, entries []wikisync.IndexEntry) error {
	path := filepath.Join(q.LocalDir, ".feishu-cli-manifests", q.TaskID()+".map.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	type legacyEntry struct {
		NodeToken string `json:"node_token"`
		ObjToken  string `json:"obj_token"`
		ObjType   string `json:"obj_type"`
		Title     string `json:"title"`
		LocalPath string `json:"local_path"`
		WikiURL   string `json:"wiki_url"`
	}
	list := make([]legacyEntry, 0, len(entries))
	for _, e := range entries {
		list = append(list, legacyEntry{
			NodeToken: e.NodeToken, ObjToken: e.ObjToken, ObjType: e.ObjType,
			Title: e.Title, LocalPath: e.LocalPath, WikiURL: e.WikiURL,
		})
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

// collectAssetFiles 收集某节点下载的 assets 文件（相对 local_dir），保证 manifest 完整。
func collectAssetFiles(assetsDir, baseDir string) []string {
	if assetsDir == "" {
		return nil
	}
	var out []string
	err := filepath.WalkDir(assetsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if rel, err := filepath.Rel(baseDir, path); err == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return out
}

// dedupeAndSort 去重并按字典序排序，保证 manifest 输出稳定。
func dedupeAndSort(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func init() {
	wikiSyncCmd.AddCommand(wikiSyncPullCmd)
	wikiSyncPullCmd.Flags().String("user-access-token", "", "User Access Token（可选；默认优先登录态，失败回退 App Token）")
}
