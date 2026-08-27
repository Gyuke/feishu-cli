package event

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// 复现：A 是 last-consumer，其 unsubscribe 在 bus flock 之外执行；
// B 在注销在途期间完成 claim+subscribe+ready，随后被 A 的迟到 unsubscribe 抹掉。
func TestProbeLastUnsubscribeRaceWithNewConsumer(t *testing.T) {
	stubConsumerAlive(t)
	bus := setupBus(t)

	unsubStarted := make(chan struct{})
	unsubGate := make(chan struct{}) // 手动放行，模拟服务端处理慢/RTT 高
	var mu sync.Mutex
	var seq []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isUnsub := r.URL.Path == "/open-apis/vc/v1/meetings/unsubscription"
		if isUnsub {
			mu.Lock()
			seq = append(seq, "unsubscribe-start")
			mu.Unlock()
			close(unsubStarted)
			<-unsubGate // 请求在网上飞行 / 服务端排队
			mu.Lock()
			seq = append(seq, "unsubscribe-applied")
			mu.Unlock()
		} else {
			mu.Lock()
			seq = append(seq, "subscribe")
			mu.Unlock()
		}
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	var readyA, readyB concurrentBuffer
	cancelA, doneA := startVCConsumer(t, srv.URL, bus, 901, &readyA)
	waitRuntimeReady(t, &readyA, "vc.meeting.participant_meeting_started_v1")

	// A 收到 SIGTERM 等价物
	cancelA()

	// 等 A 的 unsubscribe 已发出（说明 ReleaseConsumer 已在锁内完成、bus 中已无 A）
	select {
	case <-unsubStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("A 的 unsubscribe 未发出")
	}

	// 注销在途期间启动 B
	cancelB, doneB := startVCConsumer(t, srv.URL, bus, 902, &readyB)
	defer cancelB()
	waitRuntimeReady(t, &readyB, "vc.meeting.participant_meeting_started_v1")
	mu.Lock()
	seq = append(seq, "B-ready")
	mu.Unlock()

	// 现在放行 A 的注销 —— 它会抹掉 B 刚建立的订阅
	close(unsubGate)
	select {
	case <-doneA:
	case <-time.After(8 * time.Second):
		t.Fatal("A 未退出")
	}

	// 验证 B 仍在跑（bus 里有 B，进程存活，已 ready），但服务端订阅已被 A 抹掉
	snap, err := bus.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	bAlive := false
	for _, c := range snap.Consumers {
		if c.PID == 902 {
			bAlive = true
		}
	}
	mu.Lock()
	got := append([]string(nil), seq...)
	mu.Unlock()
	t.Logf("事件顺序 = %v ; B 仍在 bus = %v", got, bAlive)
	if !bAlive {
		t.Fatal("B 应仍在 bus 中")
	}
	// 断言坏顺序真实发生：B-ready 早于 unsubscribe-applied
	idxReady, idxApplied := -1, -1
	for i, s := range got {
		if s == "B-ready" {
			idxReady = i
		}
		if s == "unsubscribe-applied" {
			idxApplied = i
		}
	}
	if idxReady < 0 || idxApplied < 0 || idxReady > idxApplied {
		t.Fatalf("未复现坏顺序: %v", got)
	}
	cancelB()
	select {
	case <-doneB:
	case <-time.After(5 * time.Second):
	}
}
