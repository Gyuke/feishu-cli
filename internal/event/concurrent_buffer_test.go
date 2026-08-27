package event

import (
	"bytes"
	"sync"
)

// concurrentBuffer 供 event 测试在 Runtime 写 ReadyOut 的同时安全读取内容。
type concurrentBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *concurrentBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(p)
}

func (c *concurrentBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}
