package cmd

import (
	"github.com/spf13/cobra"
)

// wikiSyncCmd 是知识库增量同步的命令组父命令。
//
// 相关子命令在后续阶段逐步挂载：
//   - pull       Phase 1
//   - subscribe  Phase 2
//   - watch      Phase 3
//   - reconcile  Phase 4
//   - status     Phase 4
//
// 纯分组命令不写 RunE（命令组守卫会自动注入未知子命令报错）。
var wikiSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "知识库自动化增量同步",
	Long: `知识库自动化增量同步：基于配置批量拉取、批量订阅、事件监听定向重导、定时对账。

配置默认读取 ~/.feishu-cli/wiki-sync.yaml，可用 --config 显式覆盖。`,
}

func init() {
	wikiCmd.AddCommand(wikiSyncCmd)
	wikiSyncCmd.PersistentFlags().String("config", "", "wiki-sync.yaml 配置路径（默认 ~/.feishu-cli/wiki-sync.yaml）")
	wikiSyncCmd.PersistentFlags().Bool("dry-run", false, "只校验配置并打印将执行的命令，不调用 API、不写文件")
}
