package cmd

import (
	"fmt"
	"strings"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/cobra"
)

const (
	approvalTopicTodo = "1"
	approvalTopicDone = "2"
	// approvalTopicStarted (topic=3) 已不被 GET /open-apis/approval/v4/tasks 接受：
	// 服务端回 99992402 "topic is optional, options: [1,2,17,18]"。
	// 保留常量仅为给出明确的迁移提示，不再作为合法输入。
	approvalTopicStarted  = "3"
	approvalTopicCCUnread = "17"
	approvalTopicCCRead   = "18"
)

var approvalTaskQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "查询审批任务列表",
	Long: `查询当前 auth 登录用户的审批任务列表，可用于查看待办审批、已办审批、已发起审批和抄送通知。

底层接口:
  GET /open-apis/approval/v4/tasks

权限:
  User Token，scope: approval:task:read

当前契约不再传 user_id query，身份取 User Token。

参数:
  --topic        任务主题，可选：todo、done、cc-unread、cc-read
                 （started 已被官方下线，请用 approval instance initiated）
  --output, -o   输出格式，可选：json、raw-json

示例:
  # 查询当前登录用户的待我审批（User Token 必需）
  feishu-cli approval task query --topic todo

  # 查询我已审批的任务
  feishu-cli approval task query --topic done

  # 显式使用 User Token
  feishu-cli approval task query --topic todo --user-access-token u-xxx

  # JSON 输出
  feishu-cli approval task query --topic done --output json

  # 原始 API 响应
  feishu-cli approval task query --topic todo --output raw-json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}

		topic, _ := cmd.Flags().GetString("topic")
		topicValue, err := normalizeApprovalTaskTopic(topic)
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

		token, err := requireUserToken(cmd, "approval task query")
		if err != nil {
			return err
		}
		queryOpts := client.ApprovalTaskQueryOptions{
			PageSize:       pageSize,
			PageToken:      pageToken,
			Topic:          topicValue,
			Locale:         locale,
			DefinitionCode: definitionCode,
			StartTimestamp: startTimestamp,
			EndTimestamp:   endTimestamp,
			UserIDType:     userIDType,
		}

		if output == "raw-json" {
			raw, err := client.QueryApprovalTasksRaw(queryOpts, token)
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}

		result, err := client.QueryApprovalTasks(queryOpts, token)
		if err != nil {
			return err
		}

		if output == "json" {
			return printJSON(result)
		}

		if len(result.Tasks) == 0 {
			fmt.Printf("没有找到审批任务（topic: %s）\n", approvalTaskTopicLabel(topicValue))
			return nil
		}

		if result.Count != nil {
			fmt.Printf("审批任务（%s），总数约 %d\n\n", approvalTaskTopicLabel(topicValue), *result.Count)
		} else {
			fmt.Printf("审批任务（%s），当前页 %d 条\n\n", approvalTaskTopicLabel(topicValue), len(result.Tasks))
		}

		for idx, task := range result.Tasks {
			fmt.Printf("[%d] %s\n", idx+1, task.Title)
			fmt.Printf("    任务 ID: %s\n", task.TaskID)
			if task.InstanceCode != "" {
				fmt.Printf("    实例 Code: %s\n", task.InstanceCode)
			}
			if task.DefinitionName != "" {
				fmt.Printf("    审批流: %s\n", task.DefinitionName)
			}
			if task.InitiatorName != "" {
				fmt.Printf("    发起人: %s\n", task.InitiatorName)
			}
			if task.Status != "" {
				fmt.Printf("    任务状态: %s\n", task.Status)
			}
			if task.InstanceStatus != "" {
				fmt.Printf("    实例状态: %s\n", task.InstanceStatus)
			}
			if task.SupportAPIOperate {
				fmt.Printf("    支持 API 操作: true\n")
			}
			if task.Link != "" {
				fmt.Printf("    链接: %s\n", task.Link)
			}
			fmt.Println()
		}

		if result.HasMore {
			fmt.Printf("还有更多任务，使用 --page-token %s 获取下一页\n", result.PageToken)
		}

		return nil
	},
}

func normalizeApprovalTaskTopic(topic string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "1", "todo":
		return approvalTopicTodo, nil
	case "2", "done":
		return approvalTopicDone, nil
	case "3", "started", "initiated":
		return "", fmt.Errorf("topic=started 已不被官方 tasks 接口支持（服务端仅接受 todo/done/cc-unread/cc-read）；" +
			"查询「我发起的审批」请改用 `feishu-cli approval instance initiated`")
	case "17", "cc-unread", "unread-cc":
		return approvalTopicCCUnread, nil
	case "18", "cc-read", "read-cc":
		return approvalTopicCCRead, nil
	default:
		return "", fmt.Errorf("不支持的 topic: %s（可选值: todo, done, cc-unread, cc-read）", topic)
	}
}

func approvalTaskTopicLabel(topic string) string {
	switch topic {
	case approvalTopicTodo:
		return "待我审批"
	case approvalTopicDone:
		return "我已审批"
	case approvalTopicStarted:
		return "我发起的审批"
	case approvalTopicCCUnread:
		return "未读抄送"
	case approvalTopicCCRead:
		return "已读抄送"
	default:
		return topic
	}
}

func init() {
	approvalTaskCmd.AddCommand(approvalTaskQueryCmd)

	approvalTaskQueryCmd.Flags().String("topic", "", "任务主题：todo、done、cc-unread、cc-read（started 已下线，用 approval instance initiated）")
	approvalTaskQueryCmd.Flags().Int("page-size", 50, "每页数量")
	approvalTaskQueryCmd.Flags().String("page-token", "", "分页标记")
	approvalTaskQueryCmd.Flags().String("locale", "", "语言，如 zh-CN / en-US / ja-JP")
	approvalTaskQueryCmd.Flags().String("definition-code", "", "审批定义 Code，用于筛选")
	approvalTaskQueryCmd.Flags().String("start-timestamp", "", "任务时间范围开始（秒级时间戳）")
	approvalTaskQueryCmd.Flags().String("end-timestamp", "", "任务时间范围结束（秒级时间戳）")
	approvalTaskQueryCmd.Flags().String("user-id-type", "open_id", "用户 ID 类型：open_id/user_id/union_id")
	approvalTaskQueryCmd.Flags().StringP("output", "o", "", "输出格式（json/raw-json）")
	approvalTaskQueryCmd.Flags().String("user-access-token", "", "User Access Token（用户授权令牌）")
	mustMarkFlagRequired(approvalTaskQueryCmd, "topic")
}
