package cmd

import (
	"fmt"
	"strings"

	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/riba2534/feishu-cli/internal/converter"
	"github.com/spf13/cobra"
)

var docContentUpdateCmd = &cobra.Command{
	Use:   "content-update <document_id>",
	Short: "更新文档内容（支持 7 种模式）",
	Long: `更新飞书文档内容，支持追加、覆盖、定位替换、插入和删除。

模式:
  append         追加到文档末尾
  overwrite      完全覆盖文档内容
  replace_range  按定位替换一段内容
  replace_all    全文查找替换所有匹配
  insert_before  在定位内容前插入
  insert_after   在定位内容后插入
  delete_range   删除定位的内容

定位方式（replace_range/replace_all/insert_before/insert_after/delete_range 必需）:
  --selection-by-title "## 标题"        按标题定位
  --selection-with-ellipsis "开头...结尾" 按内容范围定位

示例:
  # 追加内容
  feishu-cli doc content-update DOC_ID --mode append --markdown "## 新章节\n\n内容"

  # 完全覆盖
  feishu-cli doc content-update DOC_ID --mode overwrite --markdown "# 新文档\n\n全新内容"

  # 按标题替换章节
  feishu-cli doc content-update DOC_ID --mode replace_range \
    --selection-by-title "## 旧章节" --markdown "## 新章节\n\n更新后的内容"

  # 全文查找替换
  feishu-cli doc content-update DOC_ID --mode replace_all \
    --selection-with-ellipsis "旧文本" --markdown "新文本"

  # 在指定章节前插入
  feishu-cli doc content-update DOC_ID --mode insert_before \
    --selection-by-title "## 目标章节" --markdown "## 插入的章节\n\n内容"

  # 删除一段内容
  feishu-cli doc content-update DOC_ID --mode delete_range \
    --selection-by-title "## 废弃章节"

  # 从文件读取 markdown
  feishu-cli doc content-update DOC_ID --mode append --markdown-file content.md`,
	Args: cobra.ExactArgs(1),
	RunE: runDocContentUpdate,
}

func init() {
	docCmd.AddCommand(docContentUpdateCmd)
	docContentUpdateCmd.Flags().String("mode", "", "更新模式: append/overwrite/replace_range/replace_all/insert_before/insert_after/delete_range")
	docContentUpdateCmd.Flags().String("markdown", "", "Markdown 内容")
	docContentUpdateCmd.Flags().String("markdown-file", "", "从文件读取 Markdown")
	docContentUpdateCmd.Flags().String("selection-by-title", "", "按标题定位（如 \"## 章节标题\"）")
	docContentUpdateCmd.Flags().String("selection-with-ellipsis", "", "按内容范围定位（如 \"开头内容...结尾内容\"）")
	docContentUpdateCmd.Flags().StringP("output", "o", "", "输出格式 (json)")
	docContentUpdateCmd.Flags().String("user-access-token", "", "User Access Token")
	docContentUpdateCmd.Flags().Bool("upload-images", false, "上传 Markdown 中的本地图片")
	docContentUpdateCmd.Flags().String("table-column-width", "auto",
		"Markdown 表格列宽策略：auto | fixed | 像素列表如 80,200,*,120（* 表示该列走 auto）")
	docContentUpdateCmd.Flags().Int("revision-id", -1, "文档版本号（用于并发冲突保护，-1 表示自动基于当前版本）")
	mustMarkFlagRequired(docContentUpdateCmd, "mode")
}

// runDocContentUpdate 是 content-update 命令的主入口
func runDocContentUpdate(cmd *cobra.Command, args []string) error {
	if err := config.Validate(); err != nil {
		return err
	}

	documentID := args[0]
	mode, _ := cmd.Flags().GetString("mode")
	markdownStr, _ := cmd.Flags().GetString("markdown")
	markdownFile, _ := cmd.Flags().GetString("markdown-file")
	selByTitle, _ := cmd.Flags().GetString("selection-by-title")
	selWithEllipsis, _ := cmd.Flags().GetString("selection-with-ellipsis")
	output, _ := cmd.Flags().GetString("output")
	uploadImages, _ := cmd.Flags().GetBool("upload-images")
	colWidthRaw, _ := cmd.Flags().GetString("table-column-width")
	colWidthMode, colWidthValues, errFlag := parseTableColumnWidthFlag(colWidthRaw)
	if errFlag != nil {
		return errFlag
	}
	userAccessToken := resolveOptionalUserToken(cmd)
	revisionID, _ := cmd.Flags().GetInt("revision-id")

	// 解析 Markdown 内容
	markdownContent, err := resolveMarkdownContent(markdownStr, markdownFile)
	if err != nil && mode != "delete_range" {
		return err
	}

	// 验证参数
	if err := validateContentUpdateParams(mode, markdownContent, selByTitle, selWithEllipsis); err != nil {
		return err
	}

	switch mode {
	case "append":
		return doAppend(documentID, markdownContent, uploadImages, output, userAccessToken, colWidthMode, colWidthValues)
	case "overwrite":
		return doOverwrite(documentID, markdownContent, uploadImages, output, userAccessToken, colWidthMode, colWidthValues, revisionID)
	case "replace_range":
		return doReplaceRange(documentID, markdownContent, selByTitle, selWithEllipsis, uploadImages, output, userAccessToken, colWidthMode, colWidthValues, revisionID)
	case "replace_all":
		return doReplaceAll(documentID, markdownContent, selByTitle, selWithEllipsis, uploadImages, output, userAccessToken, colWidthMode, colWidthValues, revisionID)
	case "insert_before":
		return doInsertBefore(documentID, markdownContent, selByTitle, selWithEllipsis, uploadImages, output, userAccessToken, colWidthMode, colWidthValues)
	case "insert_after":
		return doInsertAfter(documentID, markdownContent, selByTitle, selWithEllipsis, uploadImages, output, userAccessToken, colWidthMode, colWidthValues)
	case "delete_range":
		return doDeleteRange(documentID, selByTitle, selWithEllipsis, output, userAccessToken, revisionID)
	}
	return nil // validateContentUpdateParams 已确保 mode 合法
}

// resolveMarkdownContent 从 --markdown 或 --markdown-file 获取内容
func resolveMarkdownContent(markdownStr, markdownFile string) (string, error) {
	if markdownStr != "" && markdownFile != "" {
		return "", fmt.Errorf("--markdown 和 --markdown-file 不能同时使用")
	}
	if markdownFile != "" {
		return loadJSONInput("", markdownFile, "markdown", "markdown-file", "Markdown 内容")
	}

	content, err := loadJSONInput(markdownStr, "", "markdown", "markdown-file", "Markdown 内容")
	if err != nil {
		return "", err
	}
	// 处理命令行中的 \n 转义；文件读取必须保持 LaTeX 反斜杠原样。
	return strings.ReplaceAll(content, "\\n", "\n"), nil
}

// validateContentUpdateParams 验证参数组合是否合法
func validateContentUpdateParams(mode, markdown, selByTitle, selWithEllipsis string) error {
	validModes := map[string]bool{
		"append": true, "overwrite": true, "replace_range": true,
		"replace_all": true, "insert_before": true, "insert_after": true,
		"delete_range": true,
	}
	if !validModes[mode] {
		return fmt.Errorf("不支持的模式: %s", mode)
	}

	// 需要 markdown 内容的模式
	needsMarkdown := mode != "delete_range"
	if needsMarkdown && markdown == "" {
		return fmt.Errorf("模式 %s 需要 --markdown 或 --markdown-file", mode)
	}

	// 需要定位的模式
	needsSelection := mode != "append" && mode != "overwrite"
	if needsSelection {
		if selByTitle == "" && selWithEllipsis == "" {
			return fmt.Errorf("模式 %s 需要 --selection-by-title 或 --selection-with-ellipsis", mode)
		}
		if selByTitle != "" && selWithEllipsis != "" {
			return fmt.Errorf("--selection-by-title 和 --selection-with-ellipsis 不能同时使用")
		}
	}

	return nil
}

// ============================================================
// 块文本提取（用于定位匹配）
// ============================================================

// getBlockText 提取块的纯文本内容
func getBlockText(block *larkdocx.Block) string {
	if block == nil || block.BlockType == nil {
		return ""
	}

	extractFromText := func(t *larkdocx.Text) string {
		if t == nil || len(t.Elements) == 0 {
			return ""
		}
		var sb strings.Builder
		for _, elem := range t.Elements {
			if elem == nil {
				continue
			}
			if elem.TextRun != nil && elem.TextRun.Content != nil {
				sb.WriteString(*elem.TextRun.Content)
			}
			if elem.MentionUser != nil && elem.MentionUser.UserId != nil {
				sb.WriteString("@" + *elem.MentionUser.UserId)
			}
			if elem.MentionDoc != nil && elem.MentionDoc.Title != nil {
				sb.WriteString(*elem.MentionDoc.Title)
			}
			if elem.Equation != nil && elem.Equation.Content != nil {
				sb.WriteString(*elem.Equation.Content)
			}
		}
		return sb.String()
	}

	bt := converter.BlockType(*block.BlockType)
	switch bt {
	case converter.BlockTypeText:
		return extractFromText(block.Text)
	case converter.BlockTypeHeading1:
		return extractFromText(block.Heading1)
	case converter.BlockTypeHeading2:
		return extractFromText(block.Heading2)
	case converter.BlockTypeHeading3:
		return extractFromText(block.Heading3)
	case converter.BlockTypeHeading4:
		return extractFromText(block.Heading4)
	case converter.BlockTypeHeading5:
		return extractFromText(block.Heading5)
	case converter.BlockTypeHeading6:
		return extractFromText(block.Heading6)
	case converter.BlockTypeHeading7:
		return extractFromText(block.Heading7)
	case converter.BlockTypeHeading8:
		return extractFromText(block.Heading8)
	case converter.BlockTypeHeading9:
		return extractFromText(block.Heading9)
	case converter.BlockTypeBullet:
		return extractFromText(block.Bullet)
	case converter.BlockTypeOrdered:
		return extractFromText(block.Ordered)
	case converter.BlockTypeQuote:
		return extractFromText(block.Quote)
	case converter.BlockTypeTodo:
		return extractFromText(block.Todo)
	case converter.BlockTypeCode:
		return extractFromText(block.Code)
	default:
		return ""
	}
}

// getBlockHeadingLevel 返回块的标题级别（1-9），非标题返回 0
func getBlockHeadingLevel(block *larkdocx.Block) int {
	if block == nil || block.BlockType == nil {
		return 0
	}
	bt := converter.BlockType(*block.BlockType)
	if bt >= converter.BlockTypeHeading1 && bt <= converter.BlockTypeHeading9 {
		return int(bt - converter.BlockTypeHeading1 + 1)
	}
	return 0
}

// ============================================================
// 定位逻辑
// ============================================================

// blockRange 表示匹配到的块范围（在 Page 子块中的索引，左闭右开）
type blockRange struct {
	startIndex int // 起始索引（包含）
	endIndex   int // 结束索引（不包含）
}

// findByTitle 按标题定位块范围
// title 格式如 "## 标题文本"，解析出级别和文本
// 范围：从该标题到下一个同级/更高级标题（或文档末尾）
func findByTitle(children []*larkdocx.Block, title string) ([]blockRange, error) {
	// 解析标题级别和文本
	level, text := parseTitleSelector(title)
	if text == "" {
		return nil, fmt.Errorf("标题选择器格式无效: %q", title)
	}

	var ranges []blockRange
	for i, block := range children {
		blockLevel := getBlockHeadingLevel(block)
		if blockLevel == 0 {
			continue // 非标题块不匹配
		}
		if level > 0 && blockLevel != level {
			continue // 指定级别时不匹配其它级别
		}
		blockText := getBlockText(block)
		if !strings.Contains(blockText, text) {
			continue
		}

		// 找到匹配的标题，确定范围终点（遇到同级或更高级标题结束）
		matchLevel := blockLevel
		end := len(children) // 默认到文档末尾
		for j := i + 1; j < len(children); j++ {
			nextLevel := getBlockHeadingLevel(children[j])
			if nextLevel > 0 && nextLevel <= matchLevel {
				end = j
				break
			}
		}
		ranges = append(ranges, blockRange{startIndex: i, endIndex: end})
	}

	if len(ranges) == 0 {
		return nil, fmt.Errorf("未找到匹配的标题: %q", title)
	}
	return ranges, nil
}

// parseTitleSelector 解析标题选择器，如 "## 标题" → (2, "标题")
func parseTitleSelector(title string) (int, string) {
	title = strings.TrimSpace(title)
	level := 0
	for _, ch := range title {
		if ch == '#' {
			level++
		} else {
			break
		}
	}
	if level == 0 {
		// 没有 # 前缀，当作 text 精确匹配，默认级别 0 表示匹配任意标题
		return 0, title
	}
	text := strings.TrimSpace(title[level:])
	return level, text
}

// findByEllipsis 按省略号定位块范围
// 格式："开头内容...结尾内容" 或不含 "..." 的精确匹配
func findByEllipsis(children []*larkdocx.Block, selector string) ([]blockRange, error) {
	// 检查是否包含非转义的 "..."
	parts := strings.SplitN(selector, "...", 2)
	if len(parts) == 2 {
		startText := strings.TrimSpace(parts[0])
		endText := strings.TrimSpace(parts[1])
		return findByStartEnd(children, startText, endText)
	}

	// 精确匹配：找所有包含该文本的块
	text := strings.TrimSpace(selector)
	var ranges []blockRange
	for i, block := range children {
		if strings.Contains(getBlockText(block), text) {
			ranges = append(ranges, blockRange{startIndex: i, endIndex: i + 1})
		}
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("未找到包含文本的块: %q", text)
	}
	return ranges, nil
}

// findByStartEnd 查找从包含 startText 的块到包含 endText 的块的范围
func findByStartEnd(children []*larkdocx.Block, startText, endText string) ([]blockRange, error) {
	// 查找起始块
	startIdx := -1
	for i, block := range children {
		if strings.Contains(getBlockText(block), startText) {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return nil, fmt.Errorf("未找到包含起始文本的块: %q", startText)
	}

	// 从起始块之后查找结束块
	endIdx := -1
	for i := startIdx; i < len(children); i++ {
		if strings.Contains(getBlockText(children[i]), endText) {
			endIdx = i + 1 // 左闭右开
			// 不 break，取最后一个匹配
		}
	}
	if endIdx < 0 {
		return nil, fmt.Errorf("未找到包含结束文本的块: %q", endText)
	}

	return []blockRange{{startIndex: startIdx, endIndex: endIdx}}, nil
}

// findSelection 统一定位入口
func findSelection(children []*larkdocx.Block, selByTitle, selWithEllipsis string) ([]blockRange, error) {
	if selByTitle != "" {
		return findByTitle(children, selByTitle)
	}
	return findByEllipsis(children, selWithEllipsis)
}

// ============================================================
// 获取文档顶层子块（Page 的直接子块）
// ============================================================

// getPageChildren 获取文档的 Page 块的直接子块
func getPageChildren(documentID, userAccessToken string) ([]*larkdocx.Block, error) {
	return client.GetAllBlockChildren(documentID, documentID, userAccessToken)
}

// ============================================================
// 7 种模式实现
// ============================================================

// doAppend 追加到文档末尾
func doAppend(documentID, markdown string, uploadImages bool, output, userAccessToken, colWidthMode string, colWidthValues []int) error {
	err := addContentMarkdownWithOptions(documentID, documentID, markdown, "", uploadImages, -1, output, userAccessToken, colWidthMode, colWidthValues)
	if err != nil {
		return fmt.Errorf("追加内容失败: %w", err)
	}
	if output != "json" {
		fmt.Println("文档内容追加成功！")
	}
	return nil
}

// validateAndPreconvertMarkdown 先行解析校验 Markdown、转换并预处理本地图片，确保在破坏性删除前拦截所有内容与上传错误
func validateAndPreconvertMarkdown(documentID, markdown string, uploadImages bool, colWidthMode string, colWidthValues []int) error {
	opts := converter.ConvertOptions{
		DocumentID:   documentID,
		UploadImages: uploadImages,
	}
	applyColumnWidthOptions(&opts, colWidthMode, colWidthValues)
	conv := converter.NewMarkdownToBlock([]byte(markdown), opts, "")
	result, err := conv.ConvertWithTableData()
	if err != nil {
		return fmt.Errorf("转换 Markdown 失败: %w", err)
	}
	if len(result.BlockNodes) == 0 {
		return fmt.Errorf("没有内容可添加")
	}
	return nil
}

// resolveBaseRevision 解析用于并发冲突保护的文档基准版本号
func resolveBaseRevision(documentID string, userAccessToken string, revisionID int) (int, error) {
	if revisionID >= 0 {
		return revisionID, nil
	}
	currentRev, err := client.GetDocumentRevision(documentID, userAccessToken)
	if err != nil {
		return -1, fmt.Errorf("获取文档版本失败: %w", err)
	}
	return currentRev, nil
}

// doOverwrite 完全覆盖文档内容
func doOverwrite(documentID, markdown string, uploadImages bool, output, userAccessToken, colWidthMode string, colWidthValues []int, revisionID int) error {
	// 1. 失败前不删除：先行解析转换并预处理图片，任何错误立即终止，绝不删除原内容
	if err := validateAndPreconvertMarkdown(documentID, markdown, uploadImages, colWidthMode, colWidthValues); err != nil {
		return err
	}

	// 2. 获取文档基准版本（并发冲突保护）与全部直接子块（全分页拉取）
	baseRevision, err := resolveBaseRevision(documentID, userAccessToken, revisionID)
	if err != nil {
		return err
	}

	children, err := client.GetAllBlockChildren(documentID, documentID, userAccessToken)
	if err != nil {
		return fmt.Errorf("获取文档子块失败: %w", err)
	}

	// 3. 删除所有现有子块（带版本号冲突保护）
	if len(children) > 0 {
		if _, err := client.DeleteBlocksWithRevision(documentID, documentID, 0, len(children), baseRevision, userAccessToken); err != nil {
			return fmt.Errorf("删除现有内容失败: %w", err)
		}
	}

	// 4. 创建新内容
	err = addContentMarkdownWithOptions(documentID, documentID, markdown, "", uploadImages, -1, output, userAccessToken, colWidthMode, colWidthValues)
	if err != nil {
		return fmt.Errorf("写入新内容失败: %w", err)
	}
	if output != "json" {
		fmt.Println("文档内容覆盖成功！")
	}
	return nil
}

// doReplaceRange 按定位替换一段内容
func doReplaceRange(documentID, markdown, selByTitle, selWithEllipsis string, uploadImages bool, output, userAccessToken, colWidthMode string, colWidthValues []int, revisionID int) error {
	// 1. 失败前不删除：先行解析转换并预处理图片，任何错误立即终止，绝不删除原内容
	if err := validateAndPreconvertMarkdown(documentID, markdown, uploadImages, colWidthMode, colWidthValues); err != nil {
		return err
	}

	// 2. 获取文档基准版本与定位范围
	baseRevision, err := resolveBaseRevision(documentID, userAccessToken, revisionID)
	if err != nil {
		return err
	}

	children, err := getPageChildren(documentID, userAccessToken)
	if err != nil {
		return fmt.Errorf("获取文档内容失败: %w", err)
	}

	ranges, err := findSelection(children, selByTitle, selWithEllipsis)
	if err != nil {
		return err
	}

	// 取第一个匹配范围
	r := ranges[0]

	// 3. 删除匹配范围（带版本号冲突保护）
	if _, err := client.DeleteBlocksWithRevision(documentID, documentID, r.startIndex, r.endIndex, baseRevision, userAccessToken); err != nil {
		return fmt.Errorf("删除目标内容失败: %w", err)
	}

	// 4. 在删除位置插入新内容
	err = addContentMarkdownWithOptions(documentID, documentID, markdown, "", uploadImages, r.startIndex, output, userAccessToken, colWidthMode, colWidthValues)
	if err != nil {
		return fmt.Errorf("插入替换内容失败: %w", err)
	}
	if output != "json" {
		fmt.Printf("已替换索引 %d 到 %d 的内容\n", r.startIndex, r.endIndex)
	}
	return nil
}

// doReplaceAll 全文查找替换所有匹配
func doReplaceAll(documentID, markdown, selByTitle, selWithEllipsis string, uploadImages bool, output, userAccessToken, colWidthMode string, colWidthValues []int, revisionID int) error {
	// 1. 失败前不删除：先行解析转换并预处理图片，任何错误立即终止，绝不删除原内容
	if err := validateAndPreconvertMarkdown(documentID, markdown, uploadImages, colWidthMode, colWidthValues); err != nil {
		return err
	}

	// 2. 获取文档基准版本与定位范围
	baseRevision, err := resolveBaseRevision(documentID, userAccessToken, revisionID)
	if err != nil {
		return err
	}

	children, err := getPageChildren(documentID, userAccessToken)
	if err != nil {
		return fmt.Errorf("获取文档内容失败: %w", err)
	}

	ranges, err := findSelection(children, selByTitle, selWithEllipsis)
	if err != nil {
		return err
	}

	// 从后往前替换，避免索引偏移
	replaced := 0
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]

		// 删除匹配范围（带版本号冲突保护）
		if _, err := client.DeleteBlocksWithRevision(documentID, documentID, r.startIndex, r.endIndex, baseRevision, userAccessToken); err != nil {
			return fmt.Errorf("删除第 %d 个匹配内容失败: %w", i+1, err)
		}

		// 在删除位置插入新内容
		err = addContentMarkdownWithOptions(documentID, documentID, markdown, "", uploadImages, r.startIndex, "", userAccessToken, colWidthMode, colWidthValues)
		if err != nil {
			return fmt.Errorf("插入第 %d 个替换内容失败: %w", i+1, err)
		}
		replaced++
		baseRevision = -1 // 首次操作已完成版本校验，后续操作忽略递增的 revision
	}

	if output == "json" {
		return printJSON(map[string]any{
			"document_id":    documentID,
			"replaced_count": replaced,
		})
	}
	fmt.Printf("全文替换完成，共替换 %d 处\n", replaced)
	return nil
}

// doInsertBefore 在定位内容前插入
func doInsertBefore(documentID, markdown, selByTitle, selWithEllipsis string, uploadImages bool, output, userAccessToken, colWidthMode string, colWidthValues []int) error {
	children, err := getPageChildren(documentID, userAccessToken)
	if err != nil {
		return fmt.Errorf("获取文档内容失败: %w", err)
	}

	ranges, err := findSelection(children, selByTitle, selWithEllipsis)
	if err != nil {
		return err
	}

	// 取第一个匹配范围，在其前面插入
	r := ranges[0]
	err = addContentMarkdownWithOptions(documentID, documentID, markdown, "", uploadImages, r.startIndex, output, userAccessToken, colWidthMode, colWidthValues)
	if err != nil {
		return fmt.Errorf("插入内容失败: %w", err)
	}
	if output != "json" {
		fmt.Printf("已在索引 %d 前插入内容\n", r.startIndex)
	}
	return nil
}

// doInsertAfter 在定位内容后插入
func doInsertAfter(documentID, markdown, selByTitle, selWithEllipsis string, uploadImages bool, output, userAccessToken, colWidthMode string, colWidthValues []int) error {
	children, err := getPageChildren(documentID, userAccessToken)
	if err != nil {
		return fmt.Errorf("获取文档内容失败: %w", err)
	}

	ranges, err := findSelection(children, selByTitle, selWithEllipsis)
	if err != nil {
		return err
	}

	// 取第一个匹配范围，在其后面插入
	r := ranges[0]
	err = addContentMarkdownWithOptions(documentID, documentID, markdown, "", uploadImages, r.endIndex, output, userAccessToken, colWidthMode, colWidthValues)
	if err != nil {
		return fmt.Errorf("插入内容失败: %w", err)
	}
	if output != "json" {
		fmt.Printf("已在索引 %d 后插入内容\n", r.endIndex-1)
	}
	return nil
}

// doDeleteRange 删除定位的内容
func doDeleteRange(documentID, selByTitle, selWithEllipsis string, output, userAccessToken string, revisionID int) error {
	baseRevision, err := resolveBaseRevision(documentID, userAccessToken, revisionID)
	if err != nil {
		return err
	}

	children, err := getPageChildren(documentID, userAccessToken)
	if err != nil {
		return fmt.Errorf("获取文档内容失败: %w", err)
	}

	ranges, err := findSelection(children, selByTitle, selWithEllipsis)
	if err != nil {
		return err
	}

	// 从后往前删除，避免索引偏移
	deleted := 0
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		if _, err := client.DeleteBlocksWithRevision(documentID, documentID, r.startIndex, r.endIndex, baseRevision, userAccessToken); err != nil {
			return fmt.Errorf("删除第 %d 个匹配内容失败: %w", i+1, err)
		}
		deleted += r.endIndex - r.startIndex
		baseRevision = -1
	}

	if output == "json" {
		return printJSON(map[string]any{
			"document_id":    documentID,
			"deleted_blocks": deleted,
			"deleted_ranges": len(ranges),
		})
	}
	fmt.Printf("已删除 %d 个块（%d 个匹配范围）\n", deleted, len(ranges))
	return nil
}
