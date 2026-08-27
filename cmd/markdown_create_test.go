package cmd

import "testing"

// TestMarkdownCreateCmdRegistered 验证 create 子命令注册
func TestMarkdownCreateCmdRegistered(t *testing.T) {
	if markdownCreateCmd.Use != "create" {
		t.Fatalf("Use = %q, want create", markdownCreateCmd.Use)
	}
	if markdownCreateCmd.Short == "" {
		t.Fatal("Short should not be empty")
	}
	found := false
	for _, sub := range markdownCmd.Commands() {
		if sub == markdownCreateCmd {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("markdownCreateCmd should be child of markdownCmd")
	}
}

// TestMarkdownCreateFlagsDefaults 验证 flag 注册（content/content-file 二选一）
func TestMarkdownCreateFlagsDefaults(t *testing.T) {
	for _, n := range []string{"name", "content", "content-file", "folder-token", "wiki-token", "dry-run", "output", "user-access-token"} {
		if markdownCreateCmd.Flags().Lookup(n) == nil {
			t.Errorf("--%s missing on create", n)
		}
	}
	if out := markdownCreateCmd.Flags().Lookup("output"); out != nil && out.Shorthand != "o" {
		t.Errorf("--output shorthand=%q, want o", out.Shorthand)
	}
}

// TestMarkdownFetchCmdRegistered 验证 fetch 子命令注册 + file-token 必填
func TestMarkdownFetchCmdRegistered(t *testing.T) {
	if markdownFetchCmd.Use != "fetch" {
		t.Fatalf("Use = %q, want fetch", markdownFetchCmd.Use)
	}
	found := false
	for _, sub := range markdownCmd.Commands() {
		if sub == markdownFetchCmd {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("markdownFetchCmd should be child of markdownCmd")
	}
	f := markdownFetchCmd.Flags().Lookup("file-token")
	if f == nil {
		t.Fatal("--file-token missing")
	}
	ann := f.Annotations["cobra_annotation_bash_completion_one_required_flag"]
	if len(ann) == 0 || ann[0] != "true" {
		t.Errorf("--file-token should be required, ann=%v", ann)
	}
	for _, n := range []string{"output-path", "overwrite", "output", "version", "dry-run"} {
		if markdownFetchCmd.Flags().Lookup(n) == nil {
			t.Errorf("--%s missing on fetch", n)
		}
	}
	if out := markdownFetchCmd.Flags().Lookup("output"); out != nil && out.Shorthand != "o" {
		t.Errorf("--output shorthand=%q, want o", out.Shorthand)
	}
}

func TestMarkdownPatchCmdRegistered(t *testing.T) {
	if markdownPatchCmd.Use != "patch" {
		t.Fatalf("Use = %q, want patch", markdownPatchCmd.Use)
	}
	found := false
	for _, sub := range markdownCmd.Commands() {
		if sub == markdownPatchCmd {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("markdownPatchCmd should be child of markdownCmd")
	}
	for _, n := range []string{"file-token", "pattern", "content", "regex", "dry-run"} {
		if markdownPatchCmd.Flags().Lookup(n) == nil {
			t.Errorf("--%s missing on patch", n)
		}
	}
}

func TestMarkdownOverwriteFlagsIncludeDryRun(t *testing.T) {
	if markdownOverwriteCmd.Flags().Lookup("dry-run") == nil {
		t.Fatal("--dry-run missing on overwrite")
	}
}
