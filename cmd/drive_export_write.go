package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var markdownExportAtomicWrite = atomicWriteFile

func exportPathEscapes(outputDir, fileName string) bool {
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		return true
	}
	candidate := filepath.Clean(filepath.Join(absDir, fileName))
	rel, err := filepath.Rel(absDir, candidate)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolveSafeMarkdownExportPath(outputDir, fileName string) (string, error) {
	if strings.TrimSpace(outputDir) == "" {
		outputDir = "."
	}
	if exportPathEscapes(outputDir, fileName) {
		return "", fmt.Errorf("导出文件名不安全，越出 --output-dir: %s", fileName)
	}
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("解析 --output-dir 失败: %w", err)
	}
	name := sanitizeExportName(filepath.Base(fileName), "export.md")
	target := filepath.Join(absDir, name)
	rel, err := filepath.Rel(absDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("导出路径越出 --output-dir: %s", fileName)
	}
	return target, nil
}

func writeMarkdownExportFile(outputDir, fileName string, data []byte, overwrite bool) (string, error) {
	target, err := resolveSafeMarkdownExportPath(outputDir, fileName)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(target); err == nil && !overwrite {
		return "", fmt.Errorf("文件已存在: %s（使用 --overwrite 覆盖）", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("创建 --output-dir 失败: %w", err)
	}
	if err := markdownExportAtomicWrite(target, data); err != nil {
		return "", fmt.Errorf("写文件失败: %w", err)
	}
	return target, nil
}

// atomicWriteFile 把导出内容原子落盘：同目录临时文件 → chmod → write → fsync → rename → fsync 目录。
//
// 与 internal/auth/atomic_write.go 保持同等语义（显式权限位、目录 fsync、
// Windows 无法直接覆盖时的 .bak 兜底）。缺任一步都会有实际后果：
// 没有 chmod 时文件停留在 CreateTemp 的 0600；Windows 上裸 os.Rename
// 覆盖既有文件会失败，使 `drive export --overwrite` 报错。
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".feishu-md-export-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(exportFilePermForTest); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceExportFile(tmpName, path); err != nil {
		return err
	}
	success = true
	// 提交成功后 fsync 目录；此处失败不回滚已替换的文件，仅忽略（导出内容已落盘）
	_ = syncExportDir(dir)
	return nil
}

// exportFilePerm 导出文件权限：0600，与 token/缓存一致，避免多用户机器上被旁人读取。
const exportFilePerm os.FileMode = 0600

// exportFilePermForTest 实际使用的权限位，供测试注入以验证 Chmod 步骤真的生效
// （os.CreateTemp 默认恰好也是 0600，用常量断言无法区分）。生产恒等于 exportFilePerm。
var exportFilePermForTest = exportFilePerm

// exportOnWindows 供测试注入，用于在非 Windows 平台验证 Windows 兜底分支。
// 生产恒为 runtime.GOOS == "windows"。
var exportOnWindows = func() bool { return runtime.GOOS == "windows" }

func syncExportDir(dir string) error {
	if exportOnWindows() {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// replaceExportFile 提交临时文件；Windows 上 Rename 不能覆盖已存在目标，需先挪走旧文件。
func replaceExportFile(tmp, dest string) error {
	if !exportOnWindows() {
		return os.Rename(tmp, dest)
	}
	// Windows：Rename 不能覆盖已存在目标，先把旧文件挪到 .bak，失败则回滚
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	bak := dest + ".bak"
	_ = os.Remove(bak)
	if _, statErr := os.Stat(dest); statErr == nil {
		if err := os.Rename(dest, bak); err != nil {
			return err
		}
		if err := os.Rename(tmp, dest); err != nil {
			_ = os.Rename(bak, dest)
			return err
		}
		_ = os.Remove(bak)
		return nil
	}
	return os.Rename(tmp, dest)
}
