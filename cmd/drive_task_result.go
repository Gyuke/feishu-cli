package cmd

import (
	"fmt"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/cobra"
)

var driveTaskScenarios = []string{"import", "export", "task_check", "wiki_delete_node"}

var driveTaskResultCmd = &cobra.Command{
	Use:   "task-result",
	Short: "通用异步任务查询（import / export / task_check / wiki_delete_node）",
	Long: `统一查询异步任务状态，用于 drive import / export / move 与 wiki 节点删除等异步任务的 resume。

必填:
  --scenario     任务场景: import / export / task_check / wiki_delete_node

对应入参:
  --ticket       import/export 场景必填
  --file-token   export 场景必填（原始文档 token）
  --task-id      task_check 与 wiki_delete_node 场景必填（异步任务 ID）

权限:
  - User Access Token

示例:
  feishu-cli drive task-result --scenario export --ticket abcxxx --file-token docxxx
  feishu-cli drive task-result --scenario import --ticket abcxxx
  feishu-cli drive task-result --scenario task_check --task-id xxx
  feishu-cli drive task-result --scenario wiki_delete_node --task-id xxx`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}

		token, err := requireUserToken(cmd, "drive task-result")
		if err != nil {
			return err
		}

		scenario, _ := cmd.Flags().GetString("scenario")
		ticket, _ := cmd.Flags().GetString("ticket")
		fileToken, _ := cmd.Flags().GetString("file-token")
		taskID, _ := cmd.Flags().GetString("task-id")
		output, _ := cmd.Flags().GetString("output")

		if err := validateEnum(scenario, "--scenario", driveTaskScenarios); err != nil {
			return err
		}

		var result map[string]any

		switch scenario {
		case "import":
			if ticket == "" {
				return fmt.Errorf("--ticket 在 import 场景必填")
			}
			status, err := client.GetDriveImportStatus(ticket, token)
			if err != nil {
				return err
			}
			result = map[string]any{
				"scenario":      "import",
				"ticket":        ticket,
				"ready":         status.Ready(),
				"pending":       status.Pending(),
				"failed":        status.Failed(),
				"job_status":    status.JobStatus,
				"job_error_msg": status.JobErrorMsg,
				"doc_token":     status.DocToken,
				"doc_url":       status.DocURL,
				"type":          status.Type,
			}
		case "export":
			if ticket == "" {
				return fmt.Errorf("--ticket 在 export 场景必填")
			}
			if fileToken == "" {
				return fmt.Errorf("--file-token 在 export 场景必填（原始文档 token）")
			}
			status, err := client.GetDriveExportStatus(ticket, fileToken, token)
			if err != nil {
				return err
			}
			result = map[string]any{
				"scenario":         "export",
				"ticket":           ticket,
				"ready":            status.Ready(),
				"pending":          status.Pending(),
				"failed":           status.Failed(),
				"job_status":       status.JobStatus,
				"job_status_label": status.StatusLabel(),
				"job_error_msg":    status.JobErrorMsg,
				"file_token":       status.FileToken,
				"file_name":        status.FileName,
				"file_size":        status.FileSize,
				"doc_type":         status.DocType,
				"file_extension":   status.FileExtension,
			}
		case "task_check":
			if taskID == "" {
				return fmt.Errorf("--task-id 在 task_check 场景必填")
			}
			status, err := client.GetDriveTaskCheck(taskID, token)
			if err != nil {
				return err
			}
			result = map[string]any{
				"scenario": "task_check",
				"task_id":  taskID,
				"status":   status.Status,
				"ready":    status.Status == "success",
				"failed":   status.Status == "failed",
			}
		case "wiki_delete_node":
			if taskID == "" {
				return fmt.Errorf("--task-id 在 wiki_delete_node 场景必填")
			}
			status, err := client.GetWikiDeleteNodeTask(taskID, token)
			if err != nil {
				return err
			}
			result = map[string]any{
				"scenario":   "wiki_delete_node",
				"task_id":    taskID,
				"ready":      status.Ready(),
				"failed":     status.Failed(),
				"pending":    !status.Ready() && !status.Failed(),
				"status":     status.Status,
				"status_msg": status.StatusMsg,
			}
		}

		if output == "json" {
			return printJSON(result)
		}

		fmt.Printf("scenario: %s\n", scenario)
		for k, v := range result {
			if k == "scenario" {
				continue
			}
			fmt.Printf("  %s: %v\n", k, v)
		}
		return nil
	},
}

func init() {
	driveCmd.AddCommand(driveTaskResultCmd)
	driveTaskResultCmd.Flags().String("scenario", "", "任务场景: import/export/task_check/wiki_delete_node（必填）")
	driveTaskResultCmd.Flags().String("ticket", "", "异步任务 ticket（import/export 必填）")
	driveTaskResultCmd.Flags().String("file-token", "", "原始文档 token（export 必填）")
	driveTaskResultCmd.Flags().String("task-id", "", "异步任务 ID（task_check 与 wiki_delete_node 场景必填）")
	driveTaskResultCmd.Flags().StringP("output", "o", "", "输出格式（json）")
	driveTaskResultCmd.Flags().String("user-access-token", "", "User Access Token（覆盖登录态）")
	mustMarkFlagRequired(driveTaskResultCmd, "scenario")
}
