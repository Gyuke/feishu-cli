package cmd

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/spf13/cobra"
)

// estimateMermaidComplexity 简单估算复杂度，返回非空字符串表示有风险
func estimateMermaidComplexity(source, sourceType string) string {
	content := source
	if sourceType == "" || sourceType == "file" {
		data, err := os.ReadFile(source)
		if err != nil {
			return ""
		}
		content = string(data)
	}
	participantCount := strings.Count(content, "participant ")
	alt := strings.Count(content, "alt ") + strings.Count(content, "\nalt")
	par := strings.Count(content, "par ") + strings.Count(content, "\npar")
	longLabels := 0
	for _, line := range strings.Split(content, "\n") {
		if len(line) > 60 {
			longLabels++
		}
	}
	if par > 0 {
		return fmt.Sprintf("含 par 语法 %d 次（飞书服务端不支持）", par)
	}
	if participantCount >= 10 {
		return fmt.Sprintf("participant 数 %d ≥ 10", participantCount)
	}
	if alt >= 3 {
		return fmt.Sprintf("alt 嵌套 %d 次", alt)
	}
	if longLabels >= 30 {
		return fmt.Sprintf("长标签行 %d 个 ≥ 30", longLabels)
	}
	return ""
}

var importDiagramCmd = &cobra.Command{
	Use:   "import <whiteboard_id> <source>",
	Short: "导入图表到画板",
	Long: `将 PlantUML 或 Mermaid 图表导入到飞书画板。

参数:
  <whiteboard_id>    画板 ID（必填）
  <source>           图表代码或文件路径（必填）
  --source-type      源类型：file/content，默认 file
  --syntax           图表语法：plantuml/mermaid，默认 plantuml
  --diagram-type     图表类型：auto/mindmap/sequence/activity/class/er/flowchart/state/component，默认 auto
  --style            样式类型：board/classic，默认 board
  --output, -o       输出格式 (json)

图表语法:
  plantuml    PlantUML 语法（默认）
  mermaid     Mermaid 语法

样式类型:
  board       画板风格（默认）
  classic     经典风格

示例:
  # 从文件导入 PlantUML 图表
  feishu-cli board import <whiteboard_id> diagram.puml

  # 导入 Mermaid 图表
  feishu-cli board import <whiteboard_id> diagram.mmd --syntax mermaid

  # 直接导入图表代码
  feishu-cli board import <whiteboard_id> "@startuml\nA -> B: hello\n@enduml" --source-type content

  # 使用经典样式
  feishu-cli board import <whiteboard_id> diagram.puml --style classic`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Validate(); err != nil {
			return err
		}

		whiteboardID := args[0]
		source := args[1]
		sourceType, _ := cmd.Flags().GetString("source-type")
		syntax, _ := cmd.Flags().GetString("syntax")
		diagramType, _ := cmd.Flags().GetString("diagram-type")
		style, _ := cmd.Flags().GetString("style")
		parseMode, _ := cmd.Flags().GetInt("parse-mode")
		overwrite, _ := cmd.Flags().GetBool("overwrite")
		engine, _ := cmd.Flags().GetString("engine")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		output, _ := cmd.Flags().GetString("output")
		userAccessToken := resolveOptionalUserToken(cmd)

		// 复杂度估算 + 警告（仅 Mermaid，server engine）
		if syntax == "mermaid" && engine != "local" {
			if warn := estimateMermaidComplexity(source, sourceType); warn != "" {
				fmt.Fprintf(os.Stderr, "⚠ Mermaid 复杂度警告: %s\n  服务端可能渲染失败，建议改 --engine local 或改用 svg-import\n", warn)
			}
		}

		if dryRun {
			r := map[string]any{
				"whiteboard_id": whiteboardID,
				"source_type":   sourceType,
				"syntax":        syntax,
				"engine":        engine,
				"style":         style,
				"diagram_type":  diagramType,
				"parse_mode":    parseMode,
				"overwrite":     overwrite,
				"dry_run":       true,
			}
			if output == "json" {
				return printJSON(r)
			}
			fmt.Printf("[dry-run] board import 将调用 %s 引擎 syntax=%s parse_mode=%d overwrite=%v\n", engine, syntax, parseMode, overwrite)
			return nil
		}

		// 本地引擎路径：通过 whiteboard-cli 把 Mermaid/DSL 转节点 JSON 再 create_nodes
		// 适合复杂 Mermaid（10+ participant / par / 30+ 长标签）服务端会失败的场景
		if engine == "local" {
			if !client.WhiteboardCLIBridgeAvailable() {
				return fmt.Errorf("--engine local 需要 whiteboard-cli。安装：npm install -g @larksuite/whiteboard-cli")
			}
			asFile := (sourceType == "" || sourceType == "file")
			nodesJSON, err := client.RenderDiagramToOpenAPINodes(source, syntax, asFile)
			if err != nil {
				return fmt.Errorf("本地引擎转换失败: %w", err)
			}
			nodeIDs, err := client.CreateBoardNodes(whiteboardID, nodesJSON, client.CreateBoardNotesOptions{
				UserAccessToken: userAccessToken,
				Overwrite:       overwrite,
			})
			if err != nil {
				return err
			}
			if output == "json" {
				return printJSON(map[string]any{
					"whiteboard_id": whiteboardID,
					"engine":        "local",
					"syntax":        syntax,
					"node_count":    len(nodeIDs),
					"node_ids":      nodeIDs,
					"overwrite":     overwrite,
				})
			}
			fmt.Printf("图表导入成功（本地引擎）！\n  画板 ID: %s\n  创建节点数: %d\n  语法: %s\n",
				whiteboardID, len(nodeIDs), syntax)
			return nil
		}

		opts := client.ImportDiagramOptions{
			SourceType:      sourceType,
			Syntax:          syntax,
			DiagramType:     diagramType,
			Style:           style,
			ParseMode:       parseMode,
			Overwrite:       overwrite,
			UserAccessToken: userAccessToken,
		}

		retryResult := client.DoWithRetry(func() (*client.ImportDiagramResult, http.Header, error) {
			return client.ImportDiagram(whiteboardID, source, opts)
		}, client.RetryConfig{
			MaxRetries:       5,
			RetryOnRateLimit: true,
			OnRetry: func(attempt int, err error, wait time.Duration) {
				fmt.Printf("  ⚠ 图表导入重试 %d/5 (等待 %.1fs): %v\n", attempt, wait.Seconds(), err)
			},
		})
		if retryResult.Err != nil {
			return retryResult.Err
		}
		result := retryResult.Value

		if output == "json" {
			if err := printJSON(map[string]any{
				"whiteboard_id": whiteboardID,
				"ticket_id":     result.TicketID,
				"syntax":        syntax,
				"style":         style,
			}); err != nil {
				return err
			}
		} else {
			fmt.Printf("图表导入成功！\n")
			fmt.Printf("  画板 ID: %s\n", whiteboardID)
			if result.TicketID != "" {
				fmt.Printf("  票据 ID: %s\n", result.TicketID)
			}
			fmt.Printf("  语法: %s\n", syntax)
			fmt.Printf("  样式: %s\n", style)
		}

		return nil
	},
}

func init() {
	boardCmd.AddCommand(importDiagramCmd)
	importDiagramCmd.Flags().String("source-type", "file", "源类型 (file/content)")
	importDiagramCmd.Flags().String("syntax", "plantuml", "图表语法 (plantuml/mermaid)")
	importDiagramCmd.Flags().String("diagram-type", "auto", "图表类型 (auto/mindmap/sequence/activity/class/er/flowchart/state/component)")
	importDiagramCmd.Flags().String("style", "board", "样式类型 (board/classic)")
	importDiagramCmd.Flags().Int("parse-mode", 1, "解析模式 (默认 1)")
	importDiagramCmd.Flags().Bool("overwrite", false, "是否覆盖画板已有内容（服务端 overwrite: true）")
	importDiagramCmd.Flags().String("engine", "server", "渲染引擎 (server=飞书服务端 / local=whiteboard-cli 本地转换)")
	importDiagramCmd.Flags().Bool("dry-run", false, "预览不调用 API")
	importDiagramCmd.Flags().String("user-access-token", "", "User Access Token")
	importDiagramCmd.Flags().StringP("output", "o", "", "输出格式 (json)")
}
