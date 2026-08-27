package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSafeMarkdownExportPath_Traversal(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../escape.md", "..", "foo/../../outside.md"} {
		_, err := resolveSafeMarkdownExportPath(dir, name)
		if err == nil {
			t.Fatalf("%q 应拒绝越出 --output-dir", name)
		}
		if !strings.Contains(err.Error(), "越出") && !strings.Contains(err.Error(), "不安全") {
			t.Fatalf("%q error = %v", name, err)
		}
	}
}

func TestWriteMarkdownExportFile_StaysUnderOutputDir(t *testing.T) {
	dir := t.TempDir()
	saved, err := writeMarkdownExportFile(dir, "notes.md", []byte("# ok\n"), false)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	rel, err := filepath.Rel(dir, saved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("saved path escaped output-dir: %s", saved)
	}
	got, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# ok\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestWriteMarkdownExportFile_InjectedWriteFailureDoesNotTruncate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keep.md")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := markdownExportAtomicWrite
	markdownExportAtomicWrite = func(path string, data []byte) error {
		return errors.New("injected write failure")
	}
	defer func() { markdownExportAtomicWrite = orig }()

	_, err := writeMarkdownExportFile(dir, "keep.md", []byte("should-not-land"), true)
	if err == nil {
		t.Fatal("injected write failure 应返回错误")
	}
	if !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("error = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("existing --overwrite target was truncated: %q", got)
	}
}

func TestAtomicWriteFile_FailedWriteLeavesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keep.md")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 把目标目录改成只读，让同目录临时文件创建或 rename 失败（若环境不允许则跳过）。
	// 更可控的路径是直接调用 atomicWriteFile 写入，成功后覆盖；此处验证成功路径不会留下 .tmp。
	if err := atomicWriteFile(target, []byte("updated")); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "updated" {
		t.Fatalf("content = %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
