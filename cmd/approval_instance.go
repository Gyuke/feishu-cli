package cmd

import "github.com/spf13/cobra"

var approvalInstanceCmd = &cobra.Command{
	Use:     "instance",
	Aliases: []string{"instances"},
	Short:   "审批实例相关命令",
	Long: `审批实例相关命令，用于获取、创建、取消、抄送和查询已发起的审批实例。

示例:
  # 获取审批实例详情
  feishu-cli approval instance get --instance-code <ic>

  # 查询我发起的审批实例
  feishu-cli approval instance initiated

  # 创建审批实例
  feishu-cli approval instance create --approval-code <code> --form-file form.json

  # 取消审批实例
  feishu-cli approval instance cancel --instance-code <ic>

  # 抄送审批实例
  feishu-cli approval instance cc --instance-code <ic> --cc-user-ids ou_a,ou_b`,
}

func init() {
	approvalCmd.AddCommand(approvalInstanceCmd)
}
