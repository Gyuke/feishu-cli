package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/riba2534/feishu-cli/internal/event"
	"github.com/riba2534/feishu-cli/internal/wikisync"
	"github.com/spf13/cobra"
)

// wikiSyncWatchCmd 是 wiki sync 的子命令：常驻 WS 长连接消费 drive 编辑事件，
// 收到后按节点定向重导被改的文档。
var wikiSyncWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "常驻监听文档编辑事件并定向重导被改的文档（增量更新的消费端）",
	Long: `建立一条 WebSocket 长连接消费 drive.file.edit_v1（含 deleted_v1），收到编辑事件后，
按索引中的 obj_token 反查本地路径，仅重导被改的那个节点到本地，并把结果写入
<local_dir>/.feishu-cli/changes/<date>.jsonl。

前置条件（默认自动处理）：
  1. 已在 Phase 2 对索引中的 docx 建立 drive 事件订阅（watch 启动时会默认做一次
     "回查 + 补订" 预检，以服务端为真值纠正漂移；--no-precheck 可跳过）。
  2. 需要文档拥有者的 User Access Token（订阅与读取文档均需）。

去抖：同一文档的连续编辑在窗口内只聚合成一次重导（trailing edge），窗口取各 query 的
debounce 配置（默认 10s），默认由内部 ticker 驱动。

退出：Ctrl-C / SIGTERM；--timeout D 运行 D 时长后自动退出；--max-events N 处理 N 次重导后
自动退出。三者可用于定时巡检或测试。

用法：
  feishu-cli wiki sync watch                         # 常驻（Ctrl-C 退出）
  feishu-cli wiki sync watch --timeout 30s           # 跑 30s 后退出（定时重导）
  feishu-cli wiki sync watch --max-events 1          # 处理 1 次重导后退出（自检）`,
	RunE: runWikiSyncWatch,
}

// watchQuery 一个待监听的 query 及其运行时状态。
type watchQuery struct {
	q         *wikisync.Query
	debouncer *wikisync.Debouncer
	targets   map[string][]wikisync.IndexEntry // obj_token -> 该 query 下命中的 docx 条目
}

// wikiWatchRun 聚合 watch 运行期间的共享状态（调度、退出条件、配置）。
type wikiWatchRun struct {
	cfg          *wikisync.Config
	userToken    string
	queries      []*watchQuery
	tokenQueries map[string][]*watchQuery // obj_token -> 包含该 token 的 query 列表

	processed  atomic.Int64
	maxEvents  int
	cancel     context.CancelFunc
	allTargets int // 本次监听的总 docx 数（ready/报告用）
}

func runWikiSyncWatch(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".feishu-cli", "wiki-sync.yaml")
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noPrecheck, _ := cmd.Flags().GetBool("no-precheck")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	maxEvents, _ := cmd.Flags().GetInt("max-events")

	cfg, err := wikisync.LoadConfig(configPath)
	if err != nil {
		return err
	}

	// watch 是订阅的消费端，底层读文档 + 订阅都要求拥有者身份：User Token 缺失直接报错，
	// 不静默回退到 App Token（否则连接成功却永远收不到 / 读不到文档）。
	token, err := requireUserToken(cmd, "wiki sync watch")
	if err != nil {
		return err
	}

	r := &wikiWatchRun{
		cfg:          cfg,
		userToken:    token,
		tokenQueries: map[string][]*watchQuery{},
		maxEvents:    maxEvents,
	}

	// 为每个 query 建运行时状态：加载索引 → 收集 docx 目标 → 预检订阅。
	for i := range cfg.Queries {
		q := &cfg.Queries[i]
		indexPath := q.IndexFilePath()
		entries, err := wikisync.LoadIndex(indexPath)
		if err != nil {
			return fmt.Errorf("任务 %q 读取索引失败: %w", q.Name, err)
		}

		byToken := map[string][]wikisync.IndexEntry{}
		for _, e := range entries {
			if e.ObjType != "docx" {
				continue
			}
			byToken[e.ObjToken] = append(byToken[e.ObjToken], e)
		}

		// 预检：以服务端为真值回查并补订，确保订阅关系在开始监听前是对的。
		if !noPrecheck && !dryRun {
			if err := precheckQuery(cmd, q, entries, token); err != nil {
				return fmt.Errorf("任务 %q 订阅预检失败: %w", q.Name, err)
			}
		}

		wq := &watchQuery{q: q, targets: byToken}
		interval := parseDebounce(q.Debounce)
		wq.debouncer = wikisync.NewDebouncer(interval, func(fileToken string, last wikisync.EditEvent) {
			r.reexportAll(fileToken, last)
		})
		r.queries = append(r.queries, wq)
		for t := range byToken {
			r.tokenQueries[t] = append(r.tokenQueries[t], wq)
		}
		r.allTargets += len(byToken)
	}

	if r.allTargets == 0 {
		return fmt.Errorf("索引中没有可监听的 docx 条目（请先运行 `wiki sync pull` 并确认 include_types 含 docx）")
	}

	if dryRun {
		fmt.Printf("[wiki-sync] (dry-run) 将常驻监听 %d 个 docx（%d 个 query），事件: drive.file.edit_v1/deleted_v1\n",
			r.allTargets, len(r.queries))
		for _, q := range r.queries {
			fmt.Printf("  - %s: %d 个 docx（debounce=%s）\n", q.q.Name, len(q.targets), q.q.Debounce)
		}
		return nil
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	r.cancel = cancel

	// 信号处理：Ctrl-C / SIGTERM 优雅退出。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case sig := <-sigCh:
			fmt.Fprintf(os.Stderr, "[wiki-sync] 收到 %s，正在关闭...\n", sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	// --timeout：跑 D 时长后自动退出（定时巡检 / 测试）。
	if timeout > 0 {
		go func() {
			select {
			case <-time.After(timeout):
				fmt.Fprintf(os.Stderr, "[wiki-sync] 达到 --timeout=%s，自动退出\n", timeout)
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	// 去抖 ticker：周期驱动各 debouncer，触发到点的聚合重导。
	stopTicker := make(chan struct{})
	defer close(stopTicker)
	go func() {
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				now := time.Now()
				for _, wq := range r.queries {
					wq.debouncer.Drain(now)
				}
			case <-ctx.Done():
				return
			case <-stopTicker:
				return
			}
		}
	}()

	// 建 WS 长连接并阻塞，直到 ctx 取消或连接持续失败。
	apicfg := config.Get()
	baseURL := apicfg.BaseURL
	if baseURL == "" {
		baseURL = "https://open.feishu.cn"
	}
	fmt.Fprintf(os.Stderr, "[wiki-sync] 开始监听 %d 个 docx（%d 个 query）...\n", r.allTargets, len(r.queries))
	err = event.RunWS(ctx, event.RunWSOptions{
		AppID:     apicfg.AppID,
		AppSecret: apicfg.AppSecret,
		BaseURL:   baseURL,
		EventTypes: []string{
			"drive.file.edit_v1",
			"drive.file.deleted_v1",
		},
		Handler: r.onEvent,
		ErrOut:  os.Stderr,
		Ready: func() {
			fmt.Fprintf(os.Stderr, "[wiki-sync] ready (WS handshake in progress; edit a doc to trigger re-export)\n")
		},
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[wiki-sync] watch 已退出（本次重导 %d 次）\n", r.processed.Load())
	return nil
}

// onEvent 是 WS 事件的统一入口：edit_v1 去抖聚合，deleted_v1 立即记为 missing。
func (r *wikiWatchRun) onEvent(ctx context.Context, eventType string, body []byte) error {
	ev, err := wikisync.ParseEditEvent(body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[wiki-sync] 事件解析失败: %v\n", err)
		return nil
	}
	if eventType == "drive.file.deleted_v1" {
		r.onDeleted(ev)
		return nil
	}
	// edit_v1：只关心 docx（订阅是按 docx 建立的；sheet 等事件忽略）。
	if ev.FileType != "" && ev.FileType != "docx" {
		return nil
	}
	wqs := r.tokenQueries[ev.FileToken]
	if len(wqs) == 0 {
		return nil // 未跟踪的文档（理论上不会发生，因为只为跟踪的 docx 订阅）
	}
	now := time.Now()
	for _, wq := range wqs {
		wq.debouncer.Add(ev, now)
	}
	return nil
}

// onDeleted 处理文档删除：找不到可重导的内容，记为 missing 并告警。
func (r *wikiWatchRun) onDeleted(ev wikisync.EditEvent) {
	for _, wq := range r.tokenQueries[ev.FileToken] {
		for _, e := range wq.targets[ev.FileToken] {
			localRel := e.LocalPath
			if clean := wikisync.CleanRelPath(wq.q.LocalDir, filepath.Join(wq.q.LocalDir, e.LocalPath)); clean != "" {
				localRel = clean
			}
			_ = wikisync.AppendChange(wq.q.LocalDir, wikisync.Change{
				FileToken: e.ObjToken, ObjToken: e.ObjToken, NodeToken: e.NodeToken,
				LocalPath: localRel, Title: e.Title, EventID: ev.EventID,
				ChangedAt: time.Now().Format(time.RFC3339), Status: "missing",
			})
			fmt.Fprintf(os.Stderr, "[wiki-sync] 文档已删除（标记 missing 待对账）: %s (%s)\n", e.Title, e.ObjToken)
		}
	}
}

// reexportAll 是 debouncer 触发后的动作：对该 token 在本 query 命中的所有 docx 定向重导。
// 在独立 goroutine 中执行，避免阻塞去抖 ticker。每个重导完成会递增 processed 计数，
// 若达到 --max-events 则取消整个 watch。
func (r *wikiWatchRun) reexportAll(fileToken string, last wikisync.EditEvent) {
	for _, wq := range r.tokenQueries[fileToken] {
		wq := wq
		for _, e := range wq.targets[fileToken] {
			e := e
			go func() {
				r.reexportOne(wq, e, last)
				if n := r.processed.Add(1); r.maxEvents > 0 && int(n) >= r.maxEvents {
					fmt.Fprintf(os.Stderr, "[wiki-sync] 达到 --max-events=%d，自动退出\n", r.maxEvents)
					if r.cancel != nil {
						r.cancel()
					}
				}
			}()
		}
	}
}

// reexportOne 重导单个节点：拉最新节点 → 复用导出管线 → 落盘 → 记录变更。
func (r *wikiWatchRun) reexportOne(wq *watchQuery, e wikisync.IndexEntry, last wikisync.EditEvent) {
	node, err := client.GetWikiNode(e.NodeToken, r.userToken)
	if err != nil {
		r.recordFailure(wq, e, last, fmt.Sprintf("获取节点失败: %v", err))
		return
	}

	outputPath := filepath.Join(wq.q.LocalDir, e.LocalPath)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0700); err != nil {
		r.recordFailure(wq, e, last, fmt.Sprintf("创建目录失败: %v", err))
		return
	}

	exportCmd := exportCmdForQuery(wq.q)
	job := treeJob{Node: node, OutputPath: outputPath}
	assetsDirOverride := wikiTreeNodeAssetsDir(exportCmd, wq.q.LocalDir, job)

	markdown, err := exportWikiNodeMarkdown(node, r.userToken, exportCmd, assetsDirOverride)
	if err != nil {
		r.recordFailure(wq, e, last, fmt.Sprintf("导出失败: %v", err))
		return
	}
	if assetsDirOverride != "" && markdown != "" {
		markdown = makeImagePathsDocumentRelative(markdown, assetsDirOverride, outputPath)
	}
	if err := os.WriteFile(outputPath, []byte(markdown), 0600); err != nil {
		r.recordFailure(wq, e, last, fmt.Sprintf("写入文件失败: %v", err))
		return
	}

	localRel := wikisync.CleanRelPath(wq.q.LocalDir, outputPath)
	_ = wikisync.AppendChange(wq.q.LocalDir, wikisync.Change{
		FileToken: e.ObjToken, ObjToken: e.ObjToken, NodeToken: e.NodeToken,
		LocalPath: localRel, Title: e.Title, EventID: last.EventID,
		ChangedAt: time.Now().Format(time.RFC3339), Status: "updated",
	})
	fmt.Printf("[wiki-sync] 已重导 %s → %s (%s)\n", e.Title, localRel, e.ObjToken)
}

// recordFailure 把一次重导失败写入 changes，并打 stderr 告警（不中断 watch）。
func (r *wikiWatchRun) recordFailure(wq *watchQuery, e wikisync.IndexEntry, last wikisync.EditEvent, msg string) {
	localRel := wikisync.CleanRelPath(wq.q.LocalDir, filepath.Join(wq.q.LocalDir, e.LocalPath))
	_ = wikisync.AppendChange(wq.q.LocalDir, wikisync.Change{
		FileToken: e.ObjToken, ObjToken: e.ObjToken, NodeToken: e.NodeToken,
		LocalPath: localRel, Title: e.Title, EventID: last.EventID,
		ChangedAt: time.Now().Format(time.RFC3339), Status: "failed", Error: msg,
	})
	fmt.Fprintf(os.Stderr, "[wiki-sync] ✗ 重导失败 %s (%s): %s\n", e.Title, e.ObjToken, msg)
}

// precheckQuery 对单任务的索引做"回查 + 补订"预检，写入更新后的索引。
func precheckQuery(cmd *cobra.Command, q *wikisync.Query, entries []wikisync.IndexEntry, token string) error {
	checkFn := func(e wikisync.IndexEntry) (bool, error) {
		return client.GetDriveFileSubscribeStatus(e.ObjToken, client.DriveFileTypeFromObjType(e.ObjType), token)
	}
	subscribeFn := func(e wikisync.IndexEntry) error {
		return client.SubscribeDriveFile(e.ObjToken, client.DriveFileTypeFromObjType(e.ObjType), token)
	}
	ts := time.Now().Format(time.RFC3339)
	res, updated := wikisync.ReconcileSubscriptions(entries, checkFn, subscribeFn, ts)
	if err := wikisync.SaveIndex(q.IndexFilePath(), updated); err != nil {
		return fmt.Errorf("写回索引失败: %w", err)
	}
	fmt.Printf("[wiki-sync] 任务 %q 订阅预检完成: 已订=%d 补订=%d 跳过=%d 失败=%d\n",
		q.Name, res.Subscribed, res.Pending-res.Subscribed-res.Failed, res.Skipped, res.Failed)
	for _, f := range res.Failures {
		fmt.Fprintf(os.Stderr, "[wiki-sync]   ✗ 预检 %s (%s): %s\n", f.ObjToken, f.Title, f.Error)
	}
	return nil
}

// parseDebounce 解析 query 的 debounce 字符串为 time.Duration，缺省/非法时用 10s。
func parseDebounce(s string) time.Duration {
	if s == "" {
		return 10 * time.Second
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 10 * time.Second
	}
	return d
}

func init() {
	wikiSyncCmd.AddCommand(wikiSyncWatchCmd)
	wikiSyncWatchCmd.Flags().String("user-access-token", "", "User Access Token（可选；优先登录态，缺失报错）")
	wikiSyncWatchCmd.Flags().Bool("no-precheck", false, "跳过启动前的订阅回查+补订预检（默认开启）")
	wikiSyncWatchCmd.Flags().Duration("timeout", 0, "运行 D 时长后自动退出（0=不限制；用于测试/定时巡检）")
	wikiSyncWatchCmd.Flags().Int("max-events", 0, "处理 N 次重导后自动退出（0=不限制；用于自检）")
}
