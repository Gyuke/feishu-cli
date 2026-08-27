package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var tokenPathLocks sync.Map // path -> *sync.Mutex

func processLockFor(path string) *sync.Mutex {
	v, _ := tokenPathLocks.LoadOrStore(path, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// withTokenFileLock 持有进程内互斥 + 跨进程文件锁后执行 fn。
// fn 内不得再次调用 SaveToken / withTokenFileLock（非可重入）。
func withTokenFileLock(path string, fn func() error) error {
	if path == "" {
		return fn()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("创建 token 目录失败: %w", err)
	}
	mu := processLockFor(path)
	mu.Lock()
	defer mu.Unlock()
	return withOSFileLock(path, fn)
}
