package cmd

import (
	"github.com/spf13/cobra"
)

var slidesCmd = &cobra.Command{
	Use:   "slides",
	Short: "演示文稿（Slides）操作命令",
	Long: `飞书 Slides 演示文稿操作命令。

子命令:
  create        创建新的 Slides 演示文稿
  get           读取 Slides 演示文稿内容（XML 格式）
  media-upload  上传本地图片到演示文稿，返回 file_token（可作为 <img src=...> 使用）

权限要求（User Token 推荐）:
  - slides:presentation:create / slides:presentation:write_only  创建/写入
  - slides:presentation:read                                     读取演示文稿
  - docs:document.media:upload                                   上传媒体

示例:
  # 创建演示文稿
  feishu-cli slides create --title "My Deck"

  # 读取演示文稿 XML
  feishu-cli slides get <xml_presentation_id>

  # 上传图片
  feishu-cli slides media-upload --file ./cover.png --presentation-token <xml_presentation_id>`,
}

func init() {
	rootCmd.AddCommand(slidesCmd)
}
