package cmd

import (
	"fmt"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/cobra"
)

var approvalInstanceInitiatedCmd = &cobra.Command{
	Use:   "initiated",
	Short: "查询当前用户已发起的审批实例",
	Long: `查询当前登录用户已发起的审批实例列表，对齐官方 approval.instances.initiated。

底层接口:
  GET /open-apis/approval/v4/instances/initiated

权限:
  User Token，scope: approval:instance:read

示例:
  feishu-cli approval instance initiated
  feishu-cli approval instance initiated --definition-code <code> --output json
  feishu-cli approval instance initiated --page-size 20 --output raw-json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}
		token, err := requireUserToken(cmd, "approval instance initiated")
		if err != nil {
			return err
		}

		pageSize, _ := cmd.Flags().GetInt("page-size")
		pageToken, _ := cmd.Flags().GetString("page-token")
		locale, _ := cmd.Flags().GetString("locale")
		definitionCode, _ := cmd.Flags().GetString("definition-code")
		startTimestamp, _ := cmd.Flags().GetString("start-timestamp")
		endTimestamp, _ := cmd.Flags().GetString("end-timestamp")
		userIDType, _ := cmd.Flags().GetString("user-id-type")
		output, _ := cmd.Flags().GetString("output")
		if err := validateApprovalWriteUserIDType(userIDType); err != nil {
			return err
		}

		opts := client.ListInitiatedApprovalInstancesOptions{
			PageSize:       pageSize,
			PageToken:      pageToken,
			Locale:         locale,
			DefinitionCode: definitionCode,
			StartTimestamp: startTimestamp,
			EndTimestamp:   endTimestamp,
			UserIDType:     userIDType,
		}

		if output == "raw-json" {
			raw, err := client.ListInitiatedApprovalInstancesRaw(opts, token)
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}

		result, err := client.ListInitiatedApprovalInstances(opts, token)
		if err != nil {
			return err
		}
		if output == "json" {
			return printJSON(result)
		}

		if len(result.Instances) == 0 {
			fmt.Println("没有找到已发起的审批实例")
			return nil
		}
		if result.Count != nil {
			fmt.Printf("已发起审批实例，总数约 %d\n\n", *result.Count)
		} else {
			fmt.Printf("已发起审批实例，当前页 %d 条\n\n", len(result.Instances))
		}
		for idx, item := range result.Instances {
			fmt.Printf("[%d] %s\n", idx+1, item.DefinitionName)
			fmt.Printf("    实例 Code: %s\n", item.InstanceCode)
			if item.InstanceStatus != "" {
				fmt.Printf("    实例状态: %s\n", item.InstanceStatus)
			}
			if item.InitiatorName != "" {
				fmt.Printf("    发起人: %s\n", item.InitiatorName)
			}
			if item.Link != "" {
				fmt.Printf("    链接: %s\n", item.Link)
			}
			fmt.Println()
		}
		if result.HasMore {
			fmt.Printf("还有更多实例，使用 --page-token %s 获取下一页\n", result.PageToken)
		}
		return nil
	},
}

func init() {
	approvalInstanceCmd.AddCommand(approvalInstanceInitiatedCmd)
	approvalInstanceInitiatedCmd.Flags().Int("page-size", 50, "每页数量")
	approvalInstanceInitiatedCmd.Flags().String("page-token", "", "分页标记")
	approvalInstanceInitiatedCmd.Flags().String("locale", "", "语言，如 zh-CN / en-US / ja-JP")
	approvalInstanceInitiatedCmd.Flags().String("definition-code", "", "审批定义 Code，用于筛选")
	approvalInstanceInitiatedCmd.Flags().String("start-timestamp", "", "发起时间范围开始（秒级时间戳）")
	approvalInstanceInitiatedCmd.Flags().String("end-timestamp", "", "发起时间范围结束（秒级时间戳）")
	approvalInstanceInitiatedCmd.Flags().String("user-id-type", "open_id", "用户 ID 类型：open_id/user_id/union_id")
	approvalInstanceInitiatedCmd.Flags().StringP("output", "o", "", "输出格式（json/raw-json）")
	approvalInstanceInitiatedCmd.Flags().String("user-access-token", "", "User Access Token（用户授权令牌）")
}
