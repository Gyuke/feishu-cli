package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// atomicWriteFile 将 data 以 perm 权限原子写入 path：同目录临时文件 → chmod → write → fsync → rename。
// 写入失败时不会改动原文件。
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("设置临时文件权限失败: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("fsync 临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}

	if err := replaceFile(tmpName, path); err != nil {
		return fmt.Errorf("提交 token 文件失败: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("fsync 目录失败: %w", err)
	}
	success = true
	return nil
}

func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func replaceFile(tmp, dest string) error {
	err := os.Rename(tmp, dest)
	if err == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return err
	}
	// Windows 上 Rename 不能覆盖已存在目标：先把旧文件挪到 .bak，失败则回滚。
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
