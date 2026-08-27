package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
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

var wikiURLMarkers = []struct {
	Marker  string
	ObjType string
}{
	{"/wiki/", "wiki"},
	{"/docx/", "docx"},
	{"/sheets/", "sheet"},
	{"/base/", "bitable"},
	{"/bitable/", "bitable"},
	{"/mindnote/", "mindnote"},
	{"/slides/", "slides"},
	{"/file/", "file"},
	{"/doc/", "doc"},
}

// isValidFeishuLarkHost 检查是否属于飞书/Lark 文档域名或本地测试地址
func isValidFeishuLarkHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.0.0.") {
		return true
	}
	validSuffixes := []string{
		".feishu.cn", "feishu.cn",
		".larksuite.com", "larksuite.com",
		".larkoffice.com", "larkoffice.com",
	}
	for _, suffix := range validSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) || (strings.HasPrefix(suffix, ".") && strings.HasSuffix(host, suffix)) {
			return true
		}
	}
	return false
}

// parseWikiDeleteInput 对齐官方输入契约：URL 路径推断 obj_type，裸 token 必须显式传 --obj-type
func parseWikiDeleteInput(rawInput, flagObjType string) (token, objType string, err error) {
	rawInput = strings.TrimSpace(rawInput)
	if rawInput == "" {
		return "", "", fmt.Errorf("<node_token> 不能为空")
	}

	flagObjType = strings.ToLower(strings.TrimSpace(flagObjType))

	if strings.Contains(rawInput, "://") {
		u, err := url.Parse(rawInput)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", "", fmt.Errorf("URL 格式无效: %q", rawInput)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", "", fmt.Errorf("不支持的 URL 协议 %q，仅支持 http/https", u.Scheme)
		}
		if u.User != nil {
			return "", "", fmt.Errorf("URL 包含非法的用户信息 (userinfo): %q", rawInput)
		}
		if !isValidFeishuLarkHost(u.Host) {
			return "", "", fmt.Errorf("不支持的域名 %q，仅接受飞书/Lark 文档域名 (*.feishu.cn, *.larksuite.com, *.larkoffice.com)", u.Host)
		}

		inferredType := ""
		extractedToken := ""
		for _, m := range wikiURLMarkers {
			if strings.HasPrefix(u.Path, m.Marker) {
				rest := strings.TrimPrefix(u.Path, m.Marker)
				if idx := strings.IndexByte(rest, '/'); idx >= 0 {
					rest = rest[:idx]
				}
				if rest != "" {
					unescaped, err := url.PathUnescape(rest)
					if err != nil {
						return "", "", fmt.Errorf("URL token 解码失败: %w", err)
					}
					extractedToken = unescaped
					inferredType = m.ObjType
					break
				}
			}
		}
		if extractedToken == "" {
			return "", "", fmt.Errorf("无法从 URL 路径 %q 推断有效文档 token，期望以 /wiki/, /docx/, /sheets/, /base/, /mindnote/, /slides/, /file/, /doc/ 开头", u.Path)
		}
		if flagObjType != "" && flagObjType != inferredType {
			return "", "", fmt.Errorf("--obj-type %q 与从 URL 推断的文档类型 %q 冲突；请二选一", flagObjType, inferredType)
		}
		token = extractedToken
		objType = inferredType
	} else {
		if strings.ContainsAny(rawInput, "/?#") {
			return "", "", fmt.Errorf("参数 %q 既非完整 URL 亦非合法 token，不支持带部分路径的输入", rawInput)
		}
		token = rawInput
		if flagObjType == "" {
			return "", "", fmt.Errorf("当输入为裸 token 时，--obj-type 为必填项（无法从 URL 自动推断文档类型）；可选值: wiki, doc, docx, sheet, bitable, mindnote, slides, file")
		}
		objType = flagObjType
	}

	if !validWikiNodeDeleteObjTypes[objType] {
		return "", "", fmt.Errorf("不支持的 --obj-type %q；可选值: wiki, doc, docx, sheet, bitable, mindnote, slides, file", objType)
	}
	return token, objType, nil
}

var (
	wikiDeleteNodePollAttempts = 30
	wikiDeleteNodePollInterval = 2 * time.Second
)

var deleteWikiNodeCmd = &cobra.Command{
	Use:   "delete <node_token>",
	Short: "删除知识库节点",
	Long: `删除知识库节点（通过官方 Wiki 节点删除 API）。
URL 输入（/wiki/, /docx/, /sheets/ 等）自动推断文档类型；裸 token 输入必须显式指定 --obj-type。
若节点包含子节点或数据量较大，后端可能转为异步任务，本命令会自动轮询任务直至完成。

参数:
  node_token    节点 Token 或知识库完整 URL（必填）

可选参数:
  --space-id            知识空间 ID（可选，未指定时自动通过 get_node 解析）
  --obj-type            文档类型（裸 token 必填，URL 输入自动推断；可选: wiki, doc, docx, sheet, bitable, mindnote, slides, file）
  --include-children    是否级联删除子节点（默认 true）
  --force, -f           跳过确认直接删除
  --output, -o          输出格式 (json)

示例:
  # 通过 URL 删除节点（自动推断空间和类型）
  feishu-cli wiki delete https://sample.feishu.cn/wiki/wikcnXXXXXX

  # 通过裸 token 删除（必须指定 --obj-type）
  feishu-cli wiki delete wikcnXXXXXX --obj-type wiki

  # 指定空间 ID 删除并跳过确认
  feishu-cli wiki delete wikcnXXXXXX --obj-type wiki --space-id 7012345678901234567 -f

  # JSON 格式输出
  feishu-cli wiki delete https://sample.feishu.cn/wiki/wikcnXXXXXX -o json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}

		rawObjType, _ := cmd.Flags().GetString("obj-type")
		nodeToken, objType, err := parseWikiDeleteInput(args[0], rawObjType)
		if err != nil {
			return err
		}

		spaceID, _ := cmd.Flags().GetString("space-id")
		spaceID = strings.TrimSpace(spaceID)
		if spaceID != "" {
			if strings.ContainsAny(spaceID, "/?#\n\r") {
				return fmt.Errorf("非法的 --space-id: %q", spaceID)
			}
		}
		includeChildren, _ := cmd.Flags().GetBool("include-children")
		force, _ := cmd.Flags().GetBool("force")
		output, _ := cmd.Flags().GetString("output")

		token, err := resolveIdentityToken(cmd)
		if err != nil {
			return err
		}
		identity := "bot"
		if token != "" {
			identity = "user"
		}

		nodeTitle := ""
		if spaceID == "" {
			// 未指定 space-id 时通过 get_node 解析 space_id（对 wiki token 省略 obj_type，对 non-wiki token 传 obj_type）
			node, err := client.GetWikiNodeWithOptions(nodeToken, objType, token)
			if err != nil {
				return fmt.Errorf("获取节点信息失败: %w", err)
			}
			spaceID = strings.TrimSpace(node.SpaceID)
			if spaceID == "" {
				return fmt.Errorf("未能通过 get_node 获取 space_id，请通过 --space-id 显式指定")
			}
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
		status, err := pollDeleteWikiNodeTask(ctx, taskID, token, identity)
		if err != nil {
			return err
		}
		result["ready"] = true
		result["status"] = status.Status
		result["status_msg"] = status.StatusMsg
		return printDeleteWikiNodeResult(result, output)
	},
}

func buildWikiDeleteNodeResumeCmd(taskID, identity string) string {
	return fmt.Sprintf("feishu-cli drive task-result --scenario wiki_delete_node --task-id %s --as %s", strconv.Quote(taskID), identity)
}

func pollDeleteWikiNodeTask(ctx context.Context, taskID, userToken, identity string) (*client.WikiDeleteNodeTaskStatus, error) {
	resumeCmd := buildWikiDeleteNodeResumeCmd(taskID, identity)
	var last client.WikiDeleteNodeTaskStatus
	var lastErr error
	hadSuccessfulPoll := false

	for attempt := 1; attempt <= wikiDeleteNodePollAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return &last, fmt.Errorf("知识库节点删除轮询被取消 (task_id=%s): %w\n可通过以下命令继续查询: %s", taskID, ctx.Err(), resumeCmd)
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
	deleteWikiNodeCmd.Flags().String("obj-type", "", "文档类型（裸 token 必填，URL 输入自动推断；可选: wiki, doc, docx, sheet, bitable, mindnote, slides, file）")
	deleteWikiNodeCmd.Flags().String("as", "auto", "操作身份：bot|user|auto（默认 auto: User 优先，回退 Bot）")
	deleteWikiNodeCmd.Flags().Bool("include-children", true, "是否级联删除子节点（默认 true）")
	deleteWikiNodeCmd.Flags().BoolP("force", "f", false, "跳过确认直接删除")
	deleteWikiNodeCmd.Flags().StringP("output", "o", "", "输出格式 (json)")
	deleteWikiNodeCmd.Flags().String("user-access-token", "", "User Access Token（可选，用于访问个人知识库）")
}
