package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/cobra"
)

var validWikiNodeDeleteObjTypes = map[string]bool{
	"wiki":     true,
	"doc":      true,
	"docx":     true,
	"sheet":    true,
	"bitable":  true,
	"mindnote": true,
	"slides":   true,
	"file":     true,
}

var (
	wikiDeleteNodePollAttempts = 30
	wikiDeleteNodePollInterval = 2 * time.Second
)

var deleteWikiNodeCmd = &cobra.Command{
	Use:   "delete <node_token>",
	Short: "删除知识库节点",
	Long: `删除知识库节点（通过官方 Wiki 节点删除 API）。
若节点包含子节点或数据量较大，后端可能转为异步任务，本命令会自动轮询任务直至完成。

参数:
  node_token    节点 Token 或知识库 URL（必填）

可选参数:
  --space-id            知识空间 ID（可选，未指定时自动通过 get_node 解析）
  --obj-type            文档类型（默认 wiki）
  --include-children    是否级联删除子节点（默认 true）
  --force, -f           跳过确认直接删除
  --output, -o          输出格式 (json)

示例:
  # 删除节点（自动解析空间）
  feishu-cli wiki delete wikcnXXXXXX

  # 指定空间 ID 删除并跳过确认
  feishu-cli wiki delete wikcnXXXXXX --space-id 7012345678901234567 -f

  # JSON 格式输出
  feishu-cli wiki delete wikcnXXXXXX -o json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}

		nodeToken, err := extractWikiToken(args[0])
		if err != nil {
			return err
		}
		spaceID, _ := cmd.Flags().GetString("space-id")
		objType, _ := cmd.Flags().GetString("obj-type")
		includeChildren, _ := cmd.Flags().GetBool("include-children")
		force, _ := cmd.Flags().GetBool("force")
		output, _ := cmd.Flags().GetString("output")

		objType = strings.ToLower(strings.TrimSpace(objType))
		if !validWikiNodeDeleteObjTypes[objType] {
			return fmt.Errorf("不支持的 --obj-type %q；可选值: wiki, doc, docx, sheet, bitable, mindnote, slides, file", objType)
		}

		token := resolveOptionalUserToken(cmd)

		nodeTitle := ""
		if spaceID == "" {
			// 未指定 space-id 时先获取节点信息解析 space_id
			node, err := client.GetWikiNode(nodeToken, token)
			if err != nil {
				return fmt.Errorf("获取节点信息失败: %w", err)
			}
			spaceID = node.SpaceID
			nodeTitle = node.Title
		}

		// 危险操作确认
		if !force {
			prompt := fmt.Sprintf("确定要删除知识库节点 %s 吗？此操作不可恢复", nodeToken)
			if nodeTitle != "" {
				prompt = fmt.Sprintf("确定要删除知识库节点 \"%s\" (%s) 吗？此操作不可恢复", nodeTitle, nodeToken)
			}
			if !confirmAction(prompt) {
				fmt.Println("操作已取消")
				return nil
			}
		}

		fmt.Fprintf(os.Stderr, "提交删除知识库节点请求 space_id=%s, node_token=%s ...\n", spaceID, nodeToken)
		taskID, err := client.DeleteWikiNode(spaceID, nodeToken, objType, includeChildren, token)
		if err != nil {
			return err
		}

		result := map[string]any{
			"space_id":         spaceID,
			"node_token":       nodeToken,
			"obj_type":         objType,
			"include_children": includeChildren,
			"ready":            false,
			"failed":           false,
			"status":           "success",
		}

		if taskID == "" {
			// 同步删除完成
			result["ready"] = true
			result["status"] = "success"
			return printDeleteWikiNodeResult(result, output)
		}

		// 异步任务：轮询
		result["task_id"] = taskID
		result["status"] = "processing"
		fmt.Fprintf(os.Stderr, "后端转为异步任务 task_id=%s，开始轮询...\n", taskID)

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		status, err := pollDeleteWikiNodeTask(ctx, taskID, token)
		if err != nil {
			return err
		}
		result["ready"] = true
		result["status"] = status.Status
		result["status_msg"] = status.StatusMsg
		return printDeleteWikiNodeResult(result, output)
	},
}

func pollDeleteWikiNodeTask(ctx context.Context, taskID, userToken string) (*client.WikiDeleteNodeTaskStatus, error) {
	resumeCmd := fmt.Sprintf("feishu-cli drive task-result --task-id %s", taskID)
	var last client.WikiDeleteNodeTaskStatus
	var lastErr error
	hadSuccessfulPoll := false

	for attempt := 1; attempt <= wikiDeleteNodePollAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return &last, ctx.Err()
			case <-time.After(wikiDeleteNodePollInterval):
			}
		}
		st, err := client.GetWikiDeleteNodeTask(taskID, userToken)
		if err != nil {
			lastErr = err
			fmt.Fprintf(os.Stderr, "  [%d/%d] 查询失败: %v\n", attempt, wikiDeleteNodePollAttempts, err)
			continue
		}
		last = *st
		hadSuccessfulPoll = true

		if st.Ready() {
			fmt.Fprintf(os.Stderr, "任务完成 ✅\n")
			return st, nil
		}
		if st.Failed() {
			return st, fmt.Errorf("delete_node 任务失败 (task_id=%s): status=%s, msg=%s", taskID, st.Status, st.StatusMsg)
		}
		fmt.Fprintf(os.Stderr, "  [%d/%d] status=%s\n", attempt, wikiDeleteNodePollAttempts, st.Status)
	}

	if !hadSuccessfulPoll && lastErr != nil {
		return &last, fmt.Errorf("知识库节点删除任务已提交，但状态查询全部失败 (task_id=%s): %w\n后续可通过以下命令查询任务状态: %s", taskID, lastErr, resumeCmd)
	}

	return &last, fmt.Errorf("知识库节点删除任务仍在执行中或轮询超时 (task_id=%s, 当前状态=%s)\n请勿重复提交，可通过以下命令继续查询: %s", taskID, last.Status, resumeCmd)
}

func printDeleteWikiNodeResult(result map[string]any, output string) error {
	if output == "json" {
		return printJSON(result)
	}
	fmt.Printf("知识库节点删除成功！\n")
	fmt.Printf("  空间 ID:    %s\n", result["space_id"])
	fmt.Printf("  节点 Token: %s\n", result["node_token"])
	if tid, ok := result["task_id"].(string); ok && tid != "" {
		fmt.Printf("  任务 ID:    %s\n", tid)
		fmt.Printf("  任务状态:   %v\n", result["status"])
	}
	return nil
}

func init() {
	wikiCmd.AddCommand(deleteWikiNodeCmd)
	deleteWikiNodeCmd.Flags().String("space-id", "", "知识空间 ID（可选，未指定时自动解析）")
	deleteWikiNodeCmd.Flags().String("obj-type", "wiki", "文档类型（默认 wiki）")
	deleteWikiNodeCmd.Flags().Bool("include-children", true, "是否级联删除子节点（默认 true）")
	deleteWikiNodeCmd.Flags().BoolP("force", "f", false, "跳过确认直接删除")
	deleteWikiNodeCmd.Flags().StringP("output", "o", "", "输出格式 (json)")
	deleteWikiNodeCmd.Flags().String("user-access-token", "", "User Access Token（可选，用于访问个人知识库）")
}
