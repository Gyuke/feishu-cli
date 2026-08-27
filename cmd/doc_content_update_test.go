package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
	"github.com/riba2534/feishu-cli/internal/config"
	"github.com/riba2534/feishu-cli/internal/converter"
	"github.com/spf13/viper"
)

func TestResolveMarkdownContentInlineUnescapesNewlines(t *testing.T) {
	got, err := resolveMarkdownContent("# 标题\\n\\n内容", "")
	if err != nil {
		t.Fatalf("resolveMarkdownContent() 返回错误: %v", err)
	}
	want := "# 标题\n\n内容"
	if got != want {
		t.Fatalf("resolveMarkdownContent() = %q，期望 %q", got, want)
	}
}

func TestResolveMarkdownContentFilePreservesLatexBackslash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content.md")
	want := "$$\n\\nu + 1\n$$"
	if err := os.WriteFile(path, []byte(want), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	got, err := resolveMarkdownContent("", path)
	if err != nil {
		t.Fatalf("resolveMarkdownContent() 返回错误: %v", err)
	}
	if got != want {
		t.Fatalf("resolveMarkdownContent() = %q，期望 %q", got, want)
	}
}

func makeHeadingBlock(id string, level int, text string) *larkdocx.Block {
	bt := int(converter.BlockTypeHeading1) + level - 1
	txt := &larkdocx.Text{
		Elements: []*larkdocx.TextElement{
			{TextRun: &larkdocx.TextRun{Content: &text}},
		},
	}
	b := &larkdocx.Block{
		BlockId:   &id,
		BlockType: &bt,
	}
	switch level {
	case 1:
		b.Heading1 = txt
	case 2:
		b.Heading2 = txt
	case 3:
		b.Heading3 = txt
	case 4:
		b.Heading4 = txt
	case 5:
		b.Heading5 = txt
	case 6:
		b.Heading6 = txt
	}
	return b
}

func makeTextBlock(id string, text string) *larkdocx.Block {
	bt := int(converter.BlockTypeText)
	return &larkdocx.Block{
		BlockId:   &id,
		BlockType: &bt,
		Text: &larkdocx.Text{
			Elements: []*larkdocx.TextElement{
				{TextRun: &larkdocx.TextRun{Content: &text}},
			},
		},
	}
}

func TestFindByTitleWithoutHashMatchesAnyHeadingLevel(t *testing.T) {
	children := []*larkdocx.Block{
		makeHeadingBlock("b0", 1, "总览"),
		makeTextBlock("b1", "这是总览介绍"),
		makeHeadingBlock("b2", 2, "架构设计"),
		makeTextBlock("b3", "架构内容描述"),
		makeHeadingBlock("b4", 3, "实现细节"),
		makeTextBlock("b5", "细节A"),
		makeHeadingBlock("b6", 2, "总结与展望"),
		makeTextBlock("b7", "结论"),
	}

	// 1. 无 # 匹配 H2 标题 "架构设计"
	r2, err := findByTitle(children, "架构设计")
	if err != nil {
		t.Fatalf("findByTitle(架构设计) 返回错误: %v", err)
	}
	if len(r2) != 1 || r2[0].startIndex != 2 || r2[0].endIndex != 6 {
		t.Fatalf("findByTitle(架构设计) = %+v, 期望 [2, 6)", r2)
	}

	// 2. 无 # 匹配 H3 标题 "实现细节"
	r3, err := findByTitle(children, "实现细节")
	if err != nil {
		t.Fatalf("findByTitle(实现细节) 返回错误: %v", err)
	}
	if len(r3) != 1 || r3[0].startIndex != 4 || r3[0].endIndex != 6 {
		t.Fatalf("findByTitle(实现细节) = %+v, 期望 [4, 6)", r3)
	}

	// 3. 无 # 匹配 H1 标题 "总览"
	r1, err := findByTitle(children, "总览")
	if err != nil {
		t.Fatalf("findByTitle(总览) 返回错误: %v", err)
	}
	if len(r1) != 1 || r1[0].startIndex != 0 || r1[0].endIndex != 8 {
		t.Fatalf("findByTitle(总览) = %+v, 期望 [0, 8)", r1)
	}

	// 4. 带 ## 仅匹配 H2
	rHash2, err := findByTitle(children, "## 架构设计")
	if err != nil {
		t.Fatalf("findByTitle(## 架构设计) 返回错误: %v", err)
	}
	if len(rHash2) != 1 || rHash2[0].startIndex != 2 || rHash2[0].endIndex != 6 {
		t.Fatalf("findByTitle(## 架构设计) = %+v, 期望 [2, 6)", rHash2)
	}

	// 5. 带 ### 匹配 H2 应该失败
	if _, err := findByTitle(children, "### 架构设计"); err == nil {
		t.Fatalf("findByTitle(### 架构设计) 应该未找到，但未报错")
	}

	// 6. 普通文本内容不应该被当作标题匹配
	if _, err := findByTitle(children, "总览介绍"); err == nil {
		t.Fatalf("findByTitle(总览介绍) 不应匹配非标题文本块")
	}
}

func TestParseTitleSelectorLevels(t *testing.T) {
	tests := []struct {
		input     string
		wantLevel int
		wantText  string
	}{
		{"## 二级标题", 2, "二级标题"},
		{"# 一级标题", 1, "一级标题"},
		{"###   三级标题  ", 3, "三级标题"},
		{"纯文本标题", 0, "纯文本标题"},
	}

	for _, tt := range tests {
		lvl, txt := parseTitleSelector(tt.input)
		if lvl != tt.wantLevel || txt != tt.wantText {
			t.Errorf("parseTitleSelector(%q) = (%d, %q), 期望 (%d, %q)",
				tt.input, lvl, txt, tt.wantLevel, tt.wantText)
		}
	}
}

func initDocUpdateTestConfig(t *testing.T, baseURL string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("FEISHU_USER_ACCESS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("app_id: cli_test\napp_secret: test_secret\nbase_url: %q\n", baseURL)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试配置失败: %v", err)
	}
	if err := config.Init(configPath); err != nil {
		t.Fatalf("初始化测试配置失败: %v", err)
	}
}

// TestOverwriteDoesNotDeleteOnConversionFailure 验证当 Markdown 解析/转换失败时，绝不调用任何删除 API，保护原内容
func TestOverwriteDoesNotDeleteOnConversionFailure(t *testing.T) {
	deleteCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "batch_delete") {
			deleteCalls++
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok"}`)
			return
		}
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
			return
		}
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
	}))
	defer server.Close()
	initDocUpdateTestConfig(t, server.URL)

	// 传入空内容触发 validateAndPreconvertMarkdown 错误
	err := doOverwrite("doc-123", "", false, "", "", "auto", nil, -1)
	if err == nil {
		t.Fatal("空 Markdown 内容应当报错，但返回了 nil")
	}

	if deleteCalls != 0 {
		t.Fatalf("转换失败前不应触发删除！实际调用 batch_delete %d 次", deleteCalls)
	}
}

// TestReplaceRangeDoesNotDeleteOnConversionFailure 验证 replace_range 在内容转换失败前不删除原内容
func TestReplaceRangeDoesNotDeleteOnConversionFailure(t *testing.T) {
	deleteCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "batch_delete") {
			deleteCalls++
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok"}`)
			return
		}
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
			return
		}
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
	}))
	defer server.Close()
	initDocUpdateTestConfig(t, server.URL)

	err := doReplaceRange("doc-123", "", "## 标题", "", false, "", "", "auto", nil, -1)
	if err == nil {
		t.Fatal("空 Markdown 内容应当报错，但返回了 nil")
	}

	if deleteCalls != 0 {
		t.Fatalf("转换失败前不应触发删除！实际调用 batch_delete %d 次", deleteCalls)
	}
}

// TestOverwriteRevisionConflictAbortsBeforeWriting 验证 revision 冲突时删除报错且后续写入被阻止
func TestOverwriteRevisionConflictAbortsBeforeWriting(t *testing.T) {
	deleteCalls := 0
	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
		case r.URL.Path == "/open-apis/docx/v1/documents/doc-conflict":
			// 返回当前 revision_id 为 5
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"document":{"document_id":"doc-conflict","revision_id":5}}}`)
		case strings.HasSuffix(r.URL.Path, "/children") && r.Method == "GET":
			// 返回现有 2 个子块
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"items":[{"block_id":"b1"},{"block_id":"b2"}],"has_more":false}}`)
		case strings.Contains(r.URL.Path, "batch_delete"):
			deleteCalls++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			// 服务端模拟 revision 冲突
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"code":1770034,"msg":"document revision not match"}`)
		case strings.HasSuffix(r.URL.Path, "/children") && r.Method == "POST":
			createCalls++
			_, _ = fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"children":[{"block_id":"new1"}]}}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	initDocUpdateTestConfig(t, server.URL)

	err := doOverwrite("doc-conflict", "# 新内容\n\n测试", false, "", "", "auto", nil, 5)
	if err == nil {
		t.Fatal("revision 冲突时应报错，但返回了 nil")
	}

	if !strings.Contains(err.Error(), "1770034") && !strings.Contains(err.Error(), "revision not match") {
		t.Fatalf("错误应包含 revision 冲突信息，实际得到: %v", err)
	}

	if createCalls != 0 {
		t.Fatalf("删除发生 revision 冲突后，绝对不应调用创建新块！实际调用 %d 次", createCalls)
	}
}
