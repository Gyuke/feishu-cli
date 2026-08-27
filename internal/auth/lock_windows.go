//go:build windows

package auth

import (
	"fmt"
	"os"
	"time"
)

const (
	tokenLockTimeout      = 60 * time.Second
	tokenLockPollInterval = 20 * time.Millisecond
	tokenLockStaleAfter   = 2 * time.Minute
)

func withOSFileLock(path string, fn func() error) error {
	lockDir := path + ".lockdir"
	deadline := time.Now().Add(tokenLockTimeout)

	for {
		err := os.Mkdir(lockDir, 0700)
		if err == nil {
			defer os.Remove(lockDir)
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("获取 token 文件锁失败: %w", err)
		}
		if info, statErr := os.Stat(lockDir); statErr == nil && time.Since(info.ModTime()) > tokenLockStaleAfter {
			_ = os.Remove(lockDir)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("获取 token 文件锁超时，请稍后重试")
		}
		time.Sleep(tokenLockPollInterval)
	}
}
