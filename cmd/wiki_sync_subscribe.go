package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/wikisync"
	"github.com/spf13/cobra"
)

// wikiSyncSubscribeCmd 是 wiki sync 的子命令：对已拉取索引中的 docx 建立事件订阅。
var wikiSyncSubscribeCmd = &cobra.Command{
	Use:   "subscribe",
	Short: "批量订阅索引中的文档事件（增量更新的入口）",
	Long: `对每个 query 的合并索引（.<local_dir>/.feishu-cli/wiki-index.json）中的 docx 条目
建立 drive 事件订阅（drive.file.edit_v1）。订阅成功后回写索引的 subscribe_status。

后续 Phase 3 的 watch 正是消费这些订阅：收到文档编辑事件后，按节点定向重导被改的文档。

需要文档拥有者的 User Access Token（仅拥有者可订阅，自动按 --user-access-token /
登录态 / token.json 解析）。调用幂等，重复执行安全的，符合条件的条目会自动跳过。

运行结束后在 ~/.feishu-cli/state/wiki-sync/<config_hash>/subscriptions.json 写一份汇总，
便于 status 与对账读取。`,
	RunE: runWikiSyncSubscribe,
}

func runWikiSyncSubscribe(cmd *cobra.Command, _ []string) error {
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

	// 订阅是拥有者行为：User Token 缺失时报错（不会静默回退到 App Token）。
	token, err := requireUserToken(cmd, "wiki sync subscribe")
	if err != nil {
		return err
	}

	total := wikisync.SubscribeResult{}
	for i := range cfg.Queries {
		q := &cfg.Queries[i]
		indexPath := q.IndexFilePath()
		entries, err := wikisync.LoadIndex(indexPath)
		if err != nil {
			return fmt.Errorf("任务 %q 读取索引失败: %w", q.Name, err)
		}

		if dryRun {
			must := wikisync.PendingSubscriptions(entries)
			if len(must) == 0 {
				fmt.Printf("[wiki-sync] (dry-run) 任务 %q：没有需要订阅的 docx\n", q.Name)
				continue
			}
			fmt.Printf("[wiki-sync] (dry-run) 任务 %q：将订阅 %d 条 docx\n", q.Name, len(must))
			for _, e := range must {
				fmt.Printf("  - %s  %s  (%s)\n", e.ObjToken, e.Title, e.LocalPath)
			}
			continue
		}

		res, updated := wikisync.RunSubscribe(entries, func(e wikisync.IndexEntry) error {
			fileType := client.DriveFileTypeFromObjType(e.ObjType)
			return client.SubscribeDriveFile(e.ObjToken, fileType, token)
		}, time.Now().Format(time.RFC3339))

		if err := wikisync.SaveIndex(indexPath, updated); err != nil {
			return fmt.Errorf("任务 %q 写回索引失败: %w", q.Name, err)
		}

		fmt.Printf("[wiki-sync] 任务 %q 完成: 总=%d 待订=%d 新订=%d 跳过=%d 失败=%d\n",
			q.Name, res.Total, res.Pending, res.Subscribed, res.Skipped, res.Failed)
		for _, f := range res.Failures {
			fmt.Fprintf(os.Stderr, "[wiki-sync]   ✗ %s (%s): %s\n", f.ObjToken, f.Title, f.Error)
		}
		total.Total += res.Total
		total.Pending += res.Pending
		total.Subscribed += res.Subscribed
		total.Skipped += res.Skipped
		total.Failed += res.Failed
		total.Failures = append(total.Failures, res.Failures...)
	}

	if err := writeSubscriptionsSummary(configPath, total); err != nil {
		fmt.Fprintf(os.Stderr, "[wiki-sync] 写订阅汇总失败: %v\n", err)
	}

	if total.Failed > 0 {
		return fmt.Errorf("wiki sync subscribe 完成但有失败（新订 %d / 跳过 %d / 失败 %d）",
			total.Subscribed, total.Skipped, total.Failed)
	}
	return nil
}

// writeSubscriptionsSummary 把本次订阅汇总写到全局状态目录，供 status/对账读取。
func writeSubscriptionsSummary(configPath string, res wikisync.SubscribeResult) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	cfgHash := wikisync.ContentHash(data)
	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".feishu-cli", "state", "wiki-sync", cfgHash)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}
	summary := struct {
		ConfigHash string `json:"config_hash"`
		LastRun    string `json:"last_run"`
		Total      int    `json:"total"`
		Subscribed int    `json:"subscribed"`
		Skipped    int    `json:"skipped"`
		Failed     int    `json:"failed"`
	}{
		ConfigHash: cfgHash,
		LastRun:    time.Now().Format(time.RFC3339),
		Total:      res.Total,
		Subscribed: res.Subscribed,
		Skipped:    res.Skipped,
		Failed:     res.Failed,
	}
	out, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "subscriptions.json"), append(out, '\n'), 0600)
}

func init() {
	wikiSyncCmd.AddCommand(wikiSyncSubscribeCmd)
	wikiSyncSubscribeCmd.Flags().String("user-access-token", "", "User Access Token（可选；优先登录态，缺失报错）")
}
