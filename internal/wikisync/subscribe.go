package wikisync

import "time"

// SubscribeResult 是批量订阅一个索引文件的汇总结果。
type SubscribeResult struct {
	Total      int                `json:"total"`      // 索引条目总数
	Pending    int                `json:"pending"`    // 本次实际尝试订阅的数目
	Subscribed int                `json:"subscribed"` // 成功订阅的数目
	Skipped    int                `json:"skipped"`    // 跳过（非 docx 或已订阅）
	Failed     int                `json:"failed"`     // 失败数目
	Failures   []SubscribeFailure `json:"failures,omitempty"`
}

// SubscribeFailure 记录单个订阅失败原因。
type SubscribeFailure struct {
	ObjToken string `json:"obj_token"`
	Title    string `json:"title"`
	Error    string `json:"error"`
}

// PendingSubscriptions 返回需要订阅的条目：obj_type=docx 且未标记为已订阅。
func PendingSubscriptions(entries []IndexEntry) []IndexEntry {
	out := make([]IndexEntry, 0, len(entries))
	for _, e := range entries {
		if e.ObjType == "docx" && e.SubscribeStatus != SubscribeStatusSubscribed {
			out = append(out, e)
		}
	}
	return out
}

// MarkSubscribed 返回把指定 obj_token 批量标记为已订阅后的新索引切片（不改动原底层数组）。
// ts 为 ISO 时间字符串（如 time.Now().Format(time.RFC3339)），写入 last_subscribed_at。
func MarkSubscribed(entries []IndexEntry, subscribedTokens map[string]bool, ts string) []IndexEntry {
	out := make([]IndexEntry, len(entries))
	copy(out, entries)
	for i := range out {
		if subscribedTokens[out[i].ObjToken] {
			out[i].SubscribeStatus = SubscribeStatusSubscribed
			out[i].LastSubscribedAt = ts
		}
	}
	return out
}

// RunSubscribe 对索引执行批量订阅。subscribe 由调用方注入：生产环境传
// client.SubscribeDriveFile 的包装，测试环境传 stub。返回结果与更新后的索引。
//
// 单个失败不阻断后续条目（继续 on_error），最终由调用方决定退出码。
func RunSubscribe(entries []IndexEntry, subscribe func(e IndexEntry) error, ts string) (SubscribeResult, []IndexEntry) {
	pending := PendingSubscriptions(entries)
	res := SubscribeResult{
		Total:   len(entries),
		Pending: len(pending),
		Skipped: len(entries) - len(pending),
	}
	okTokens := make(map[string]bool, len(pending))
	for _, e := range pending {
		if err := subscribe(e); err != nil {
			res.Failed++
			res.Failures = append(res.Failures, SubscribeFailure{ObjToken: e.ObjToken, Title: e.Title, Error: err.Error()})
			continue
		}
		res.Subscribed++
		okTokens[e.ObjToken] = true
	}
	return res, MarkSubscribed(entries, okTokens, ts)
}

// SubscribeNow 返回当前时间戳（RFC3339），供调用方作为 last_subscribed_at。
func SubscribeNow() string {
	return time.Now().Format(time.RFC3339)
}
