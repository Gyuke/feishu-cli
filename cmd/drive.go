package cmd

import (
	"github.com/spf13/cobra"
)

// driveCmd Drive（云盘）命令组 — 对齐飞书官方 drive 子命令
// 与 file / media / comment / doc 的相关子命令互补，提供更强能力：
// 分块上传、异步任务 resume、富文本评论、markdown 快捷导出等
var driveCmd = &cobra.Command{
	Use:   "drive",
	Short: "云盘（Drive）操作命令 - 与 file/media 互补",
	Long: `Drive 云盘操作，覆盖上传、下载、导出、导入、移动、评论、异步任务管理。

相对 file/media 命令的增强能力：
  - drive upload    支持自动分块（>20MB）
  - drive download  流式下载 + 路径校验 + --overwrite
  - drive export    支持 sheet/bitable CSV 带 sub-id，docx markdown 走 docs_ai fetch，有界轮询 + resume
  - drive import    ccm_import_open 临时媒体（省略 parent_node）+ 官方格式/大小矩阵 + 有界轮询
  - drive move      省略目标时先取真实 root token；folder 移动时轮询 task_check
  - drive add-comment 支持局部评论、wiki URL 解析、富文本 reply_elements
  - drive task-result 通用异步任务查询（import/export/task_check）
  - drive pull/push/status 云盘 ↔ 本地单向镜像（SHA-256 diff + 安全 --delete-* --yes 双确认）
  - drive search    v2 doc_wiki/search 扁平 filter，支持 folder-tokens / space-ids / creator-ids / time windows

所有子命令默认走 User Access Token，先 feishu-cli auth login。

子命令:
  upload          上传本地文件（大文件自动分块）
  download        下载云盘文件
  export          导出文档为本地文件（doc/docx/sheet/bitable → pdf/docx/xlsx/csv/markdown）
  export-download 通过 file_token 下载已完成的导出任务（配合 export 超时后 resume）
  import          导入本地文件为云文档
  move            移动文件/文件夹
  add-comment     添加文件评论（支持局部、wiki 解析、富文本）
  task-result     通用异步任务查询
  pull            把云盘文件夹镜像到本地（Drive → 本地）
  push            把本地目录镜像到云盘文件夹（本地 → Drive）
  status          本地 ↔ 云盘 SHA-256 内容对照（不修改）
  search          v2 端点搜索文档 / 知识库（扁平 filter）

示例:
  feishu-cli drive upload --file big.zip --folder-token fldxxx
  feishu-cli drive export --token docxxx --doc-type docx --file-extension markdown --output-dir ./exports
  feishu-cli drive add-comment --doc https://xxx.feishu.cn/docx/yyy --content '[{"type":"text","text":"评论内容"}]'
  feishu-cli drive status --folder-token fldxxx --local-dir ./mirror
  feishu-cli drive pull --folder-token fldxxx --local-dir ./mirror --if-exists overwrite
  feishu-cli drive push --folder-token fldxxx --local-dir ./mirror --delete-remote --yes`,
}

func init() {
	rootCmd.AddCommand(driveCmd)
}
