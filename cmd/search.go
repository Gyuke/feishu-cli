package cmd

import (
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "搜索相关命令",
	Long: `搜索相关命令，支持搜索消息、应用和文档。

子命令:
  messages  搜索消息
  apps      搜索应用
  docs      搜索文档和 Wiki

注意:
  search docs / search apps 必须使用 User Access Token。
  search messages 使用 --as bot|user|auto（默认 auto；已配置 User 刷新失败 fail-closed）。
  推荐先用 auth check 预检 scope，再通过 Device Flow 登录。

获取 User Access Token:
  feishu-cli auth check --scope "search:docs:read search:message"
  feishu-cli auth login --domain search --recommend

示例:
  # 搜索消息
  feishu-cli search messages "关键词"

  # 搜索应用
  feishu-cli search apps "关键词"

  # 搜索文档
  feishu-cli search docs "产品需求"

  # 搜索特定类型的文档
  feishu-cli search docs "季度报告" --docs-types docx,sheet`,
}

func init() {
	rootCmd.AddCommand(searchCmd)
}
