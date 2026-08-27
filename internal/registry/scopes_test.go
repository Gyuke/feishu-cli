package registry

import "testing"

// TestApprovalDomainRecommendsApprovalRead 验证 approval 域的 --recommend 包含
// approval:approval:read —— 官方 approval get 端点实测要求该 scope
// （报错原文 "required one of these privileges: [approval:approval:read]"）。
//
// 回归防护：scope_priorities.json（官方快照）只有旧名 approval:approval:readonly，
// 该 scope 因此拿不到 recommend=true，--recommend 会静默少授权一个必需 scope，
// 用户登录后仍然无法使用 approval get。修复方式是在 scope_overrides.json 的
// recommend.allow 中显式放行。
func TestApprovalDomainRecommendsApprovalRead(t *testing.T) {
	Init()
	recommended := CollectDomainScopes([]string{"approval"}, true)
	found := false
	for _, s := range recommended {
		if s == "approval:approval:read" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("approval 域 --recommend 应包含 approval:approval:read，实际: %v", recommended)
	}

	// 同时确认其余审批 scope 未被误删
	for _, want := range []string{"approval:instance:read", "approval:instance:write", "approval:task:read", "approval:task:write"} {
		hit := false
		for _, s := range recommended {
			if s == want {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("approval 域 --recommend 应包含 %s，实际: %v", want, recommended)
		}
	}
}
