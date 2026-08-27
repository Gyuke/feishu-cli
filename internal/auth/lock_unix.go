//go:build !windows

package auth

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	tokenLockTimeout      = 60 * time.Second
	tokenLockPollInterval = 50 * time.Millisecond
)

func withOSFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	lockFD, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("打开 token 文件锁失败: %w", err)
	}
	defer lockFD.Close()

	deadline := time.Now().Add(tokenLockTimeout)
	for {
		err := syscall.Flock(int(lockFD.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			defer func() {
				_ = syscall.Flock(int(lockFD.Fd()), syscall.LOCK_UN)
			}()
			return fn()
		}
		if err != syscall.EAGAIN && err != syscall.EWOULDBLOCK {
			return fmt.Errorf("获取 token 文件锁失败: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("获取 token 文件锁超时，请稍后重试")
		}
		time.Sleep(tokenLockPollInterval)
	}
}
