package event

import (
	"context"
	"fmt"
	"io"
	"os"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// RunWSOptions 控制一次 WebSocket 事件订阅运行。与 consume 的 Runtime 不同，
// 这里不写 bus、不做 stdout NDJSON，专供"业务处理器需要自己消费事件"的场景复用
// （如 wiki sync watch 的定向重导）。
type RunWSOptions struct {
	AppID     string
	AppSecret string
	BaseURL   string

	// EventTypes 要监听的事件类型（schema.event_type），逐个注册到 dispatcher。
	// 仅为这些类型的事件会触发 Handler。
	EventTypes []string

	// Handler 收到匹配事件时触发；body 为该事件的原始 JSON payload（schema 2.0）。
	// 返回 error 仅记录到 ErrOut，不会断开连接。
	Handler func(ctx context.Context, eventType string, body []byte) error

	// ErrOut 诊断输出（默认 stderr）。
	ErrOut io.Writer

	// Ready 在 WS 客户端初始化完成后被调用（用于输出 ready marker 同步父进程）。
	Ready func()
}

// RunWS 建立一条 WebSocket 长连接并阻塞处理事件，直到 ctx 取消或连接持续失败。
// 复用 oapi-sdk-go ws.Client 的自动重连（WithAutoReconnect(true)，断线无限重试）。
func RunWS(ctx context.Context, opts RunWSOptions) error {
	if opts.AppID == "" || opts.AppSecret == "" {
		return fmt.Errorf("AppID/AppSecret 未配置，请在 config.yaml 或环境变量设置")
	}
	if len(opts.EventTypes) == 0 {
		return fmt.Errorf("至少需要监听一种事件类型")
	}
	if opts.ErrOut == nil {
		opts.ErrOut = os.Stderr
	}
	if opts.BaseURL == "" {
		opts.BaseURL = "https://open.feishu.cn"
	}

	dis := dispatcher.NewEventDispatcher("", "")
	for _, et := range opts.EventTypes {
		et := et // 闭包捕获，避免循环变量别名
		dis.OnCustomizedEvent(et, func(ctx context.Context, ev *larkevent.EventReq) error {
			if opts.Handler != nil {
				return opts.Handler(ctx, et, ev.Body)
			}
			return nil
		})
	}

	cli := larkws.NewClient(
		opts.AppID, opts.AppSecret,
		larkws.WithEventHandler(dis),
		larkws.WithDomain(opts.BaseURL),
		larkws.WithAutoReconnect(true),
		larkws.WithLogger(newQuietLogger(opts.ErrOut)),
		larkws.WithLogLevel(larkcore.LogLevelWarn),
	)

	if opts.Ready != nil {
		opts.Ready()
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- cli.Start(ctx)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if err != nil && !isContextCanceled(err) {
			return fmt.Errorf("WebSocket 连接失败: %w", err)
		}
		return nil
	}
}
