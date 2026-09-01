package wikisync

import (
	"errors"
	"strings"
	"testing"
)

func sampleEntries() []IndexEntry {
	return []IndexEntry{
		{NodeToken: "A", ObjToken: "t1", ObjType: "docx", Title: "doc1"},
		{NodeToken: "B", ObjToken: "t2", ObjType: "docx", Title: "doc2"},
		{NodeToken: "C", ObjToken: "t3", ObjType: "docx", Title: "doc3"},
		{NodeToken: "D", ObjToken: "s1", ObjType: "sheet", Title: "sheet"},
		{NodeToken: "E", ObjToken: "t2", ObjType: "docx", Title: "already", SubscribeStatus: SubscribeStatusSubscribed},
	}
}

func TestPendingSubscriptions(t *testing.T) {
	entries := sampleEntries()
	pending := PendingSubscriptions(entries)
	if len(pending) != 3 {
		t.Fatalf("期望 3 条待订，实际 %d", len(pending))
	}
	// 已订阅的 t2 应被排除，而 docx 的 t1/t2/t3 依然在（排除的是重复 obj_token 中的已订阅项）。
	for _, e := range pending {
		if e.ObjToken == "t2" && e.SubscribeStatus == SubscribeStatusSubscribed {
			t.Fatalf("已订阅条目不应进入待订列表: %+v", e)
		}
	}
}

func TestMarkSubscribed(t *testing.T) {
	entries := sampleEntries()
	updated := MarkSubscribed(entries, map[string]bool{"t1": true, "t2": true}, "2026-08-31T10:00:00Z")

	if entries[0].SubscribeStatus != "" {
		t.Fatalf("原切片不应被改动")
	}
	if updated[0].SubscribeStatus != SubscribeStatusSubscribed || updated[0].LastSubscribedAt != "2026-08-31T10:00:00Z" {
		t.Fatalf("t1 未正确标记: %+v", updated[0])
	}
	if updated[2].SubscribeStatus != "" {
		t.Fatalf("t3 不应被标记: %+v", updated[2])
	}
	if updated[4].SubscribeStatus != SubscribeStatusSubscribed {
		t.Fatalf("已订阅的 E 应保持订阅状态")
	}
}

func TestRunSubscribe(t *testing.T) {
	entries := sampleEntries()

	// stub 让 t2 订阅失败。
	subscribe := func(e IndexEntry) error {
		if e.ObjToken == "t2" {
			return errors.New("订阅失败：无权限")
		}
		return nil
	}

	res, updated := RunSubscribe(entries, subscribe, "2026-08-31T10:00:00Z")

	if res.Total != 5 {
		t.Fatalf("total 期望 5，实际 %d", res.Total)
	}
	if res.Pending != 3 {
		t.Fatalf("pending 期望 3，实际 %d", res.Pending)
	}
	if res.Subscribed != 2 {
		t.Fatalf("subscribed 期望 2，实际 %d", res.Subscribed)
	}
	if res.Skipped != 2 {
		t.Fatalf("skipped 期望 2，实际 %d", res.Skipped)
	}
	if res.Failed != 1 {
		t.Fatalf("failed 期望 1，实际 %d", res.Failed)
	}
	if len(res.Failures) != 1 || res.Failures[0].ObjToken != "t2" {
		t.Fatalf("failures 异常: %+v", res.Failures)
	}
	if !strings.Contains(res.Failures[0].Error, "无权限") {
		t.Fatalf("failed error 未透传: %q", res.Failures[0].Error)
	}

	// t1/t3 标记成功，t2 保持空。
	if updated[0].SubscribeStatus != SubscribeStatusSubscribed {
		t.Fatalf("t1 应成功订阅")
	}
	if updated[1].SubscribeStatus != "" {
		t.Fatalf("t2 应保持未订阅")
	}
	if updated[2].SubscribeStatus != SubscribeStatusSubscribed {
		t.Fatalf("t3 应成功订阅")
	}
}
