package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/cobra"
)

var mailMessagesCmd = &cobra.Command{
	Use:   "messages",
	Short: "批量获取多封邮件",
	Long: `批量获取多封邮件（最多 50 条）。

必填:
  --message-ids  邮件 ID 列表（逗号分隔）

可选:
  --mailbox    默认 me
  --format     full / plain_text_full（默认 full）
  -o json      JSON 格式

示例:
  feishu-cli mail messages --message-ids msg_1,msg_2,msg_3`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}
		token, mailbox, err := resolveMailReadIdentity(cmd)
		if err != nil {
			return err
		}

		raw, _ := cmd.Flags().GetString("message-ids")
		format, _ := cmd.Flags().GetString("format")
		output, _ := cmd.Flags().GetString("output")

		ids, err := parseCSVIDs(raw, "message-ids")
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return fmt.Errorf("--message-ids 至少需要一个 ID")
		}

		data, err := client.BatchGetMailMessages(mailbox, ids, format, token)
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
	mailCmd.AddCommand(mailMessagesCmd)
	mailMessagesCmd.Flags().String("mailbox", "me", "邮箱地址（默认 me）")
	mailMessagesCmd.Flags().String("message-ids", "", "邮件 ID 列表，逗号分隔（必填）")
	mailMessagesCmd.Flags().String("format", "full", "格式: full/plain_text_full")
	mailMessagesCmd.Flags().String("as", "auto", "身份选择: bot | user | auto（默认 auto）")
	mailMessagesCmd.Flags().StringP("output", "o", "", "输出格式（json）")
	mailMessagesCmd.Flags().String("user-access-token", "", "User Access Token（覆盖登录态）")
	mustMarkFlagRequired(mailMessagesCmd, "message-ids")
}
