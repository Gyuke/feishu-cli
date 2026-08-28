package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAtomicWriteFile_PermAndOverwrite 验证导出原子写入的权限位与覆盖语义。
// 回归防护：cmd 版原子写入曾缺 Chmod（文件停留在 CreateTemp 的默认权限）、
// 缺目录 fsync、缺 Windows 覆盖兜底。
func TestAtomicWriteFile_PermAndOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.md")

	if err := atomicWriteFile(target, []byte("first")); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	if got := st.Mode().Perm(); got != exportFilePerm {
		t.Errorf("权限位 = %o, want %o", got, exportFilePerm)
	}
	// 注：os.CreateTemp 默认也是 0600，与 exportFilePerm 相同，
	// 故上面的断言无法区分"显式 Chmod"与"依赖默认值"。
	// 下面用一个与默认值不同的权限做真实校验。
	origPerm := exportFilePermForTest
	exportFilePermForTest = 0640
	t.Cleanup(func() { exportFilePermForTest = origPerm })
	target2 := filepath.Join(dir, "perm.md")
	if err := atomicWriteFile(target2, []byte("x")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if st2, err := os.Stat(target2); err != nil {
		t.Fatal(err)
	} else if got := st2.Mode().Perm(); got != 0640 {
		t.Errorf("显式权限未生效: %o, want 0640（说明缺 Chmod 步骤）", got)
	}

	// 覆盖既有文件
	if err := atomicWriteFile(target, []byte("second")); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" {
		t.Errorf("内容 = %q, want second", data)
	}

	// 不留临时文件
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
}

// TestReplaceExportFile_WindowsFallback 在非 Windows 平台验证 Windows 覆盖兜底分支。
// 该分支在 Linux/macOS 上永不执行且无测试覆盖，通过注入 exportOnWindows 验证其逻辑。
func TestReplaceExportFile_WindowsFallback(t *testing.T) {
	orig := exportOnWindows
	exportOnWindows = func() bool { return true }
	t.Cleanup(func() { exportOnWindows = orig })

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.md")

	// 目标不存在：直接 rename
	tmp1 := filepath.Join(dir, "t1.tmp")
	if err := os.WriteFile(tmp1, []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replaceExportFile(tmp1, dest); err != nil {
		t.Fatalf("目标不存在时应成功: %v", err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "v1" {
		t.Errorf("内容 = %q, want v1", data)
	}

	// 目标已存在：走 .bak 兜底并成功覆盖
	tmp2 := filepath.Join(dir, "t2.tmp")
	if err := os.WriteFile(tmp2, []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replaceExportFile(tmp2, dest); err != nil {
		t.Fatalf("覆盖既有目标应成功: %v", err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "v2" {
		t.Errorf("覆盖后内容 = %q, want v2", data)
	}
	// .bak 应被清理
	if _, err := os.Stat(dest + ".bak"); err == nil {
		t.Error("成功路径不应残留 .bak")
	}

	// Windows 分支下 syncExportDir 应直接返回 nil（不 fsync 目录）
	if err := syncExportDir(dir); err != nil {
		t.Errorf("Windows 分支 syncExportDir 应返回 nil, got %v", err)
	}
}
