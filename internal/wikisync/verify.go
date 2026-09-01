package wikisync

// SubscriptionDrift 记录索引本地订阅状态与服务端真实状态不一致的条目。
// LocalStatus 非空（subscribed）而 ServerStatus 为空时，就是"本地以为已订阅、
// 服务端早已失效"的静默失明（plan §11 风险）。
type SubscriptionDrift struct {
	ObjToken     string `json:"obj_token"`
	Title        string `json:"title"`
	LocalStatus  string `json:"local_status"`
	ServerStatus string `json:"server_status"`
}

// VerifyResult 是回查服务端订阅状态的汇总（只读，不做任何写）。
type VerifyResult struct {
	Total        int                 `json:"total"`        // 参与回查的 docx 条目数
	Subscribed   int                 `json:"subscribed"`   // 服务端 is_subscribe=true
	Unsubscribed int                 `json:"unsubscribed"` // 服务端 is_subscribe=false
	Failed       int                 `json:"failed"`       // 回查请求出错
	Drift        []SubscriptionDrift `json:"drift,omitempty"`
	Targets      []IndexEntry        `json:"-"` // 服务端未订阅、需要补订的 docx 条目（内在中间结果）
}

func boolStatus(b bool) string {
	if b {
		return SubscribeStatusSubscribed
	}
	return SubscribeStatusNone
}

// VerifyServerSubscriptions 对索引的 docx 条目逐个回查服务端订阅状态（GET subscribe）。
//
// check 由调用方注入：生产用 client.GetDriveFileSubscribeStatus，测试用 stub。
// 返回服务端真实状态统计，并标出与本地不一致的漂移条目（Drift）与服务端未订阅的
// 待补订目标（Targets）。该函数是纯读取，可被 watch 启动前置检查与 status 对账复用。
func VerifyServerSubscriptions(entries []IndexEntry, check func(IndexEntry) (bool, error)) VerifyResult {
	res := VerifyResult{}
	for _, e := range entries {
		if e.ObjType != "docx" {
			continue
		}
		res.Total++
		subscribed, err := check(e)
		if err != nil {
			res.Failed++
			continue
		}
		if subscribed {
			res.Subscribed++
		} else {
			res.Unsubscribed++
			res.Targets = append(res.Targets, e)
		}
		// 本地记录 vs 服务端真值不一致 → 漂移。
		if subscribed != (e.SubscribeStatus == SubscribeStatusSubscribed) {
			res.Drift = append(res.Drift, SubscriptionDrift{
				ObjToken:     e.ObjToken,
				Title:        e.Title,
				LocalStatus:  e.SubscribeStatus,
				ServerStatus: boolStatus(subscribed),
			})
		}
	}
	return res
}

// ReconcileSubscriptions 对索引做"回查 + 补订"（server-driven），以服务端为真值纠偏。
//
// 与 RunSubscribe（local-driven，只订本地认为未订阅的）不同：本函数对每个 docx 先回查，
// 服务端已订阅→直接标记（纠正本地漏记）；服务端未订阅→调用 subscribe 补订；补订失败→
// 保持未订阅并记入失败。这解决了"本地记为已订阅但服务端早已失效"的静默失明，
// 代价是每个 docx 一次性 GET。
func ReconcileSubscriptions(entries []IndexEntry, check func(IndexEntry) (bool, error), subscribe func(IndexEntry) error, ts string) (SubscribeResult, []IndexEntry) {
	res := SubscribeResult{Total: len(entries)}
	okTokens := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.ObjType != "docx" {
			res.Skipped++
			continue
		}
		res.Pending++
		subscribed, err := check(e)
		if err != nil {
			res.Failed++
			res.Failures = append(res.Failures, SubscribeFailure{ObjToken: e.ObjToken, Title: e.Title, Error: "回查失败: " + err.Error()})
			continue
		}
		if subscribed {
			okTokens[e.ObjToken] = true
			res.Subscribed++
			continue
		}
		if err := subscribe(e); err != nil {
			res.Failed++
			res.Failures = append(res.Failures, SubscribeFailure{ObjToken: e.ObjToken, Title: e.Title, Error: err.Error()})
			continue
		}
		okTokens[e.ObjToken] = true
		res.Subscribed++
	}
	return res, MarkSubscribed(entries, okTokens, ts)
}
