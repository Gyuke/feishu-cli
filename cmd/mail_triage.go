package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/cobra"
)

var mailTriageCmd = &cobra.Command{
	Use:   "triage",
	Short: "列出/搜索/过滤邮件（按文件夹/标签/关键词/未读）",
	Long: `列出邮箱中的邮件，支持多种过滤。

可选过滤:
  --folder       文件夹: INBOX/SENT/SPAM/ARCHIVED/STRANGER 或自定义 folder_id
  --label        标签 ID
  --query        关键词搜索
  --unread-only  只显示未读
  --page-size    每页数量
  --page-token   分页标记

Folders / labels 查询:
  feishu-cli mail triage --list-folders  # 列出可用文件夹
  feishu-cli mail triage --list-labels   # 列出可用标签

示例:
  feishu-cli mail triage --folder INBOX --unread-only --page-size 20
  feishu-cli mail triage --query "会议"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}
		token, mailbox, err := resolveMailReadIdentity(cmd)
		if err != nil {
			return err
		}

		folder, _ := cmd.Flags().GetString("folder")
		label, _ := cmd.Flags().GetString("label")
		query, _ := cmd.Flags().GetString("query")
		unreadOnly, _ := cmd.Flags().GetBool("unread-only")
		pageSize, _ := cmd.Flags().GetInt("page-size")
		pageToken, _ := cmd.Flags().GetString("page-token")
		listFolders, _ := cmd.Flags().GetBool("list-folders")
		listLabels, _ := cmd.Flags().GetBool("list-labels")
		output, _ := cmd.Flags().GetString("output")

		// 辅助功能：列出 folders / labels
		if listFolders {
			data, err := client.ListMailFolders(mailbox, token)
			if err != nil {
				return err
			}
			if output == "json" {
				return printJSON(json.RawMessage(data))
			}
			fmt.Println(string(data))
			return nil
		}
		if listLabels {
			data, err := client.ListMailLabels(mailbox, token)
			if err != nil {
				return err
			}
			if output == "json" {
				return printJSON(json.RawMessage(data))
			}
			fmt.Println(string(data))
			return nil
		}

		// 有 --query 时走专用 search 端点；其他场景走 messages 列表过滤（无 label 默认 INBOX）
		var data json.RawMessage
		if query != "" {
			filter := map[string]any{}
			if folder != "" {
				filter["folder"] = []string{folder}
			}
			if label != "" {
				filter["label"] = []string{label}
			}
			if unreadOnly {
				filter["is_unread"] = true
			}
			if pageSize > 0 {
				filter["page_size"] = pageSize
			}
			if pageToken != "" {
				filter["page_token"] = pageToken
			}
			data, err = client.SearchMailMessages(mailbox, query, filter, token)
		} else {
			if folder == "" && label == "" {
				folder = "INBOX"
			}
			params := client.ListMailMessagesParams{
				MailboxID:  mailbox,
				FolderID:   folder,
				LabelID:    label,
				UnreadOnly: unreadOnly,
				PageSize:   pageSize,
				PageToken:  pageToken,
			}
			data, err = client.ListMailMessages(params, token)
		}
		if err != nil {
			return err
		}

		if output == "json" {
			return printJSON(json.RawMessage(data))
		}
		fmt.Println(string(data))
		return nil
	},
}

func init() {
	mailCmd.AddCommand(mailTriageCmd)
	mailTriageCmd.Flags().String("mailbox", "me", "邮箱地址（默认 me）")
	mailTriageCmd.Flags().String("folder", "", "文件夹 ID: INBOX/SENT/SPAM/ARCHIVED/STRANGER 或自定义 ID")
	mailTriageCmd.Flags().String("label", "", "标签 ID")
	mailTriageCmd.Flags().String("query", "", "关键词搜索")
	mailTriageCmd.Flags().Bool("unread-only", false, "只显示未读")
	mailTriageCmd.Flags().Int("page-size", 0, "每页数量")
	mailTriageCmd.Flags().String("page-token", "", "分页标记")
	mailTriageCmd.Flags().Bool("list-folders", false, "列出可用文件夹")
	mailTriageCmd.Flags().Bool("list-labels", false, "列出可用标签")
	mailTriageCmd.Flags().String("as", "auto", "身份选择: bot | user | auto（默认 auto）")
	mailTriageCmd.Flags().StringP("output", "o", "", "输出格式（json）")
	mailTriageCmd.Flags().String("user-access-token", "", "User Access Token（覆盖登录态）")
}
