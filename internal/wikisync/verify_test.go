package wikisync

import (
	"errors"
	"testing"
)

func verifySampleEntries() []IndexEntry {
	return []IndexEntry{
		{ObjToken: "t1", ObjType: "docx", Title: "已订", SubscribeStatus: SubscribeStatusSubscribed},
		{ObjToken: "t2", ObjType: "docx", Title: "未订", SubscribeStatus: SubscribeStatusNone},
		{ObjToken: "t3", ObjType: "docx", Title: "本地已订但服务端失效", SubscribeStatus: SubscribeStatusSubscribed},
		{ObjToken: "t4", ObjType: "docx", Title: "回查出错", SubscribeStatus: SubscribeStatusNone},
		{ObjToken: "s1", ObjType: "sheet", Title: "sheet 不入回查", SubscribeStatus: SubscribeStatusSubscribed},
	}
}

func TestVerifyServerSubscriptions(t *testing.T) {
	entries := verifySampleEntries()

	// stub：t1 服务端已订；t2/t3 服务端未订（t3 是漂移/静默失明）；t4 查询出错；
	// sheet 不入回查（不会触发 check）。
	check := func(e IndexEntry) (bool, error) {
		switch e.ObjToken {
		case "t1":
			return true, nil
		case "t2", "t3":
			return false, nil
		case "t4":
			return false, errors.New("回查失败")
		default:
			return false, nil
		}
	}

	vr := VerifyServerSubscriptions(entries, check)

	if vr.Total != 4 {
		t.Fatalf("Total 期望 4（只算 docx，t1/t2/t3/t4），实际 %d", vr.Total)
	}
	if vr.Subscribed != 1 {
		t.Fatalf("Subscribed 期望 1（t1），实际 %d", vr.Subscribed)
	}
	if vr.Unsubscribed != 2 {
		t.Fatalf("Unsubscribed 期望 2（t2、t3 服务端均未订），实际 %d", vr.Unsubscribed)
	}
	if vr.Failed != 1 {
		t.Fatalf("Failed 期望 1（t4），实际 %d", vr.Failed)
	}
	// Targets = 服务端未订阅的 docx（t2、t3）；不含已订的 t1、不含回查出错的 t4、sheet 不回查。
	if len(vr.Targets) != 2 {
		t.Fatalf("Targets 期望 2（t2、t3），实际 %d", len(vr.Targets))
	}

	// Drift：本地与服务端不一致的条目 = t3（本地 subscribed、服务端 false）。
	// t1 一致（都订）、t2 一致（都未订）不计。
	if len(vr.Drift) != 1 {
		t.Fatalf("Drift 期望 1（只有 t3），实际 %d: %+v", len(vr.Drift), vr.Drift)
	}
	d := vr.Drift[0]
	if d.ObjToken != "t3" || d.LocalStatus != SubscribeStatusSubscribed || d.ServerStatus != SubscribeStatusNone {
		t.Fatalf("Drift 内容异常: %+v", d)
	}
}

func TestVerifyServerSubscriptionsNoDrift(t *testing.T) {
	entries := []IndexEntry{
		{ObjToken: "a", ObjType: "docx", Title: "已订", SubscribeStatus: SubscribeStatusSubscribed},
	}
	vr := VerifyServerSubscriptions(entries, func(IndexEntry) (bool, error) { return true, nil })
	if len(vr.Drift) != 0 {
		t.Fatalf("一致时应无漂移，实际 %+v", vr.Drift)
	}
	if len(vr.Targets) != 0 {
		t.Fatalf("已订时应无 Target，实际 %+v", vr.Targets)
	}
}

func TestReconcileSubscriptions(t *testing.T) {
	entries := []IndexEntry{
		{ObjToken: "t1", ObjType: "docx", Title: "服务端已订", SubscribeStatus: SubscribeStatusSubscribed},
		{ObjToken: "t2", ObjType: "docx", Title: "漂移：本地已订服务端失效", SubscribeStatus: SubscribeStatusSubscribed},
		{ObjToken: "t3", ObjType: "docx", Title: "新建未订", SubscribeStatus: SubscribeStatusNone},
		{ObjToken: "s1", ObjType: "sheet", Title: "sheet 跳过", SubscribeStatus: SubscribeStatusSubscribed},
	}

	// check：t1 服务端已订；t2、t3 服务端未订（t2 是漂移）。
	check := func(e IndexEntry) (bool, error) {
		return e.ObjToken == "t1", nil
	}
	// subscribe：只允许 t3 成功；t2 补订失败。
	subscribed := map[string]bool{}
	subscribe := func(e IndexEntry) error {
		if e.ObjToken == "t2" {
			return errors.New("补订失败：无权限")
		}
		subscribed[e.ObjToken] = true
		return nil
	}

	res, updated := ReconcileSubscriptions(entries, check, subscribe, "2026-08-31T10:00:00Z")

	if res.Total != 4 {
		t.Fatalf("Total 期望 4，实际 %d", res.Total)
	}
	// docx = 3（t1/t2/t3），都已回查 pending；sheet 跳过。
	if res.Pending != 3 {
		t.Fatalf("Pending 期望 3，实际 %d", res.Pending)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped 期望 1（sheet），实际 %d", res.Skipped)
	}
	// 服务端确认已订：t1（原已订）+ t3（本次补订成功）= 2。
	if res.Subscribed != 2 {
		t.Fatalf("Subscribed 期望 2，实际 %d（%+v）", res.Subscribed, res.Failures)
	}
	// 失败：t2 补订失败。
	if res.Failed != 1 {
		t.Fatalf("Failed 期望 1，实际 %d", res.Failed)
	}
	if len(res.Failures) != 1 || res.Failures[0].ObjToken != "t2" {
		t.Fatalf("Failures 异常: %+v", res.Failures)
	}

	// 索引回溯：t1 保持 subscribed、t2 保持 subscribed（补订失败不回溯）、t3 转 subscribed、sheet 不变。
	byToken := map[string]IndexEntry{}
	for _, e := range updated {
		byToken[e.ObjToken] = e
	}
	if byToken["t1"].SubscribeStatus != SubscribeStatusSubscribed {
		t.Fatalf("t1 应为 subscribed")
	}
	if byToken["t2"].SubscribeStatus != SubscribeStatusSubscribed {
		t.Fatalf("t2 补订失败应保持原 subscribed 状态（不回滚）")
	}
	if byToken["t3"].SubscribeStatus != SubscribeStatusSubscribed {
		t.Fatalf("t3 应转 subscribed")
	}
	if byToken["s1"].SubscribeStatus != SubscribeStatusSubscribed {
		t.Fatalf("sheet 不应被改动")
	}
}
