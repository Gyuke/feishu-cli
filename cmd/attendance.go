package cmd

import "github.com/spf13/cobra"

var attendanceCmd = &cobra.Command{
	Use:     "attendance",
	Aliases: []string{"att"},
	Short:   "考勤打卡操作命令",
	Long: `考勤打卡操作命令，对接飞书考勤 OpenAPI。

子命令:
  user-task query    查询用户考勤打卡记录（user_tasks.query）
  user-stats query   查询用户考勤统计数据（user_stats_datas.query）

身份要求:
  支持 User Access Token 与 Tenant Access Token。
  使用 User Token 查询本人考勤时，无需指定 --user-ids（走 employee_no 自查路径）。

Scope（在飞书开放平台「应用权限管理」页面授予应用或用户）:
  attendance:task:readonly  打卡 / 统计查询（推荐）
  attendance:task           打卡读写

日期格式:
  接受 YYYY-MM-DD 或 YYYYMMDD（飞书 API 内部统一用 yyyyMMdd 整数）。

示例:
  # 查询本人打卡（User Token 自动自查）
  feishu-cli attendance user-task query \
      --start 2026-05-01 --end 2026-05-18

  # 查询指定员工打卡（employee_id 或 employee_no）
  feishu-cli attendance user-task query \
      --employee-type employee_id \
      --user-ids 2847xxxx \
      --start 2026-05-01 --end 2026-05-18

  # 查询本月日度统计
  feishu-cli attendance user-stats query \
      --employee-type employee_no \
      --user-ids 10001 \
      --stats-type daily --start 2026-05-01 --end 2026-05-18

  # JSON 输出（适合 AI Agent 解析）
  feishu-cli attendance user-task query --start 2026-05-01 --end 2026-05-18 -o json

注意:
  - 考勤 API 涉及员工隐私，需开通 attendance:task* scope。
  - employee_type 仅支持 employee_id 和 employee_no。
  - user_ids 单次最多 50 个（user-task）/ 200 个（user-stats）。
  - user-stats 起止日期跨度不超过 31 天。`,
}

func init() {
	rootCmd.AddCommand(attendanceCmd)
}
