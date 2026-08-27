package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/cobra"
)

var mailMessageCmd = &cobra.Command{
	Use:   "message",
	Short: "获取单封邮件",
	Long: `获取单封邮件的完整内容（含 HTML body 或纯文本 body）。

必填:
  --message-id

可选:
  --mailbox    邮箱地址（默认 me，即当前登录用户）
  --format     full / plain_text_full / raw（默认 full）
  -o json      JSON 格式输出

示例:
  feishu-cli mail message --message-id msg_xxx
  feishu-cli mail message --message-id msg_xxx --format plain_text_full`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}
		token, mailbox, err := resolveMailReadIdentity(cmd)
		if err != nil {
			return err
		}

		messageID, _ := cmd.Flags().GetString("message-id")
		format, _ := cmd.Flags().GetString("format")
		output, _ := cmd.Flags().GetString("output")

		if messageID == "" {
			return fmt.Errorf("--message-id 必填")
		}

		data, err := client.GetMailMessage(mailbox, messageID, format, token)
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
	mailCmd.AddCommand(mailMessageCmd)
	mailMessageCmd.Flags().String("mailbox", "me", "邮箱地址（默认 me）")
	mailMessageCmd.Flags().String("message-id", "", "邮件 message_id（必填）")
	mailMessageCmd.Flags().String("format", "full", "格式: full/plain_text_full/raw")
	mailMessageCmd.Flags().String("as", "auto", "身份选择: bot | user | auto（默认 auto）")
	mailMessageCmd.Flags().StringP("output", "o", "", "输出格式（json）")
	mailMessageCmd.Flags().String("user-access-token", "", "User Access Token（覆盖登录态）")
	mustMarkFlagRequired(mailMessageCmd, "message-id")
}
