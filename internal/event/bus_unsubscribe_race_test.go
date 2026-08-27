package event

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestUnsubscribeRaceRestoresNewConsumerSubscription 验证 last-consumer 注销后的复检补订阅。
//
// 回归防护：unsubscribe 是 bus 文件锁之外的网络调用，新 consumer 可在注销在途期间
// 完成 claim+subscribe+ready，随后被同伴迟到的 unsubscribe 抹掉服务端订阅——
// 它仍在运行且已 ready，却静默收不到任何事件
// （实测序列 subscribe → unsubscribe-start → subscribe → unsubscribe-applied，
// 最终 serverSubscribed=false）。
// 修复：注销后复检存活 consumer 数，>0 则幂等重新订阅。
func TestUnsubscribeRaceRestoresNewConsumerSubscription(t *testing.T) {
	stubConsumerAlive(t)
	bus := setupBus(t)

	unsubStarted := make(chan struct{})
	holdUnsub := make(chan struct{})

	var mu sync.Mutex
	var seq []string
	serverSubscribed := false // 服务端订阅状态模型：subscribe=true / unsubscribe=false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/vc/v1/meetings/subscription":
			mu.Lock()
			seq = append(seq, "subscribe-applied")
			serverSubscribed = true
			mu.Unlock()
		case "/open-apis/vc/v1/meetings/unsubscription":
			mu.Lock()
			seq = append(seq, "unsubscribe-start")
			mu.Unlock()
			close(unsubStarted)
			<-holdUnsub // 模拟服务端处理慢 / 高 RTT
			mu.Lock()
			seq = append(seq, "unsubscribe-applied")
			serverSubscribed = false
			mu.Unlock()
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	// consumer A (pid 901)
	var readyA concurrentBuffer
	cancelA, doneA := startVCConsumer(t, srv.URL, bus, 901, &readyA)
	waitRuntimeReady(t, &readyA, "vc.meeting.participant_meeting_started_v1")

	// A 收到 SIGTERM：ReleaseConsumer 返回 last=true → 发出 unsubscribe（阻塞在网上）
	cancelA()
	select {
	case <-unsubStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("A 的 unsubscribe 未发出")
	}

	// 注销在途期间：consumer B (pid 902) 启动 → claim 成功 + subscribe 成功 + ready
	var readyB concurrentBuffer
	cancelB, doneB := startVCConsumer(t, srv.URL, bus, 902, &readyB)
	defer cancelB()
	waitRuntimeReady(t, &readyB, "vc.meeting.participant_meeting_started_v1")

	mu.Lock()
	subscribedAfterB := serverSubscribed
	snapshot := append([]string(nil), seq...)
	mu.Unlock()
	t.Logf("B ready 时序列 = %v, serverSubscribed=%v", snapshot, subscribedAfterB)

	// A 的注销此刻才在服务端落地
	close(holdUnsub)
	select {
	case <-doneA:
	case <-time.After(5 * time.Second):
		t.Fatal("A 未退出")
	}
	// 等注销落地
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := len(seq) > 0 && seq[len(seq)-1] == "unsubscribe-applied"
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	final := append([]string(nil), seq...)
	stillSubscribed := serverSubscribed
	mu.Unlock()
	t.Logf("最终序列 = %v, serverSubscribed=%v", final, stillSubscribed)

	if !stillSubscribed {
		t.Errorf("REPRO 成立：B 已 ready 且存活，但服务端订阅被 A 迟到的 unsubscribe 抹掉（serverSubscribed=false），序列=%v", final)
	}

	cancelB()
	select {
	case <-doneB:
	case <-time.After(3 * time.Second):
		t.Fatal("B 未退出")
	}
}
