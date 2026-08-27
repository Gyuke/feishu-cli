package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	success = true
	return nil
}
