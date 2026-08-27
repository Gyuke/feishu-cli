package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var sheetProtectCmd = &cobra.Command{
	Use:    "protect <spreadsheet_token> <sheet_id>",
	Short:  "[已废弃/unsupported] 创建保护范围",
	Hidden: true,
	Long:   `创建行或列的保护范围（注意：飞书官方已废弃保护范围 OpenAPI，当前命令为 unsupported）。`,
	Args:   cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("sheet protect 接口已被飞书官方废弃且暂无替代 OpenAPI (unsupported)")
	},
}

var sheetUnprotectCmd = &cobra.Command{
	Use:    "unprotect <spreadsheet_token> <protect_ids...>",
	Short:  "[已废弃/unsupported] 删除保护范围",
	Hidden: true,
	Long:   `删除指定的保护范围（注意：飞书官方已废弃保护范围 OpenAPI，当前命令为 unsupported）。`,
	Args:   cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("sheet unprotect 接口已被飞书官方废弃且暂无替代 OpenAPI (unsupported)")
	},
}

func init() {
	sheetCmd.AddCommand(sheetProtectCmd)
	sheetCmd.AddCommand(sheetUnprotectCmd)

	sheetProtectCmd.Flags().String("dimension", "ROWS", "保护维度: ROWS, COLUMNS")
	sheetProtectCmd.Flags().Int("start", 0, "起始索引")
	sheetProtectCmd.Flags().Int("end", 0, "结束索引")
	sheetProtectCmd.Flags().String("lock-info", "", "锁定说明")
	sheetProtectCmd.Flags().StringP("output", "o", "text", "输出格式: text, json")
	sheetProtectCmd.Flags().String("user-access-token", "", "User Access Token（可选，用于访问无 App 权限的表格）")
	mustMarkFlagRequired(sheetProtectCmd, "end")

	sheetUnprotectCmd.Flags().String("user-access-token", "", "User Access Token（可选，用于访问无 App 权限的表格）")
}
