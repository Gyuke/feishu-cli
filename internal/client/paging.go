package client

import "fmt"

// ResolvePageSize 把 CLI 的 page-size 归一到 API 合法范围。
// n==0 表示未指定，使用 def；越界必须报错，禁止静默截断。
func ResolvePageSize(n, def, min, max int) (int, error) {
	if n == 0 {
		return def, nil
	}
	if n < min || n > max {
		return 0, fmt.Errorf("每页数量必须在 %d–%d 之间，得到 %d", min, max, n)
	}
	return n, nil
}

// PaginationCursor 合并 page_token / next_page_token，并在 --page-all 时检测无进展游标。
// prev 为上一页实际使用的 cursor（首页为请求里带的 page_token，可为空）。
func PaginationCursor(hasMore bool, pageToken, nextPageToken, prev string) (more bool, token string, err error) {
	token = pageToken
	if token == "" {
		token = nextPageToken
	}
	if !hasMore {
		return false, token, nil
	}
	if token == "" {
		return false, "", fmt.Errorf("分页失败: 服务端 has_more=true 但未返回 page_token/next_page_token，已停止以免重复拉取")
	}
	if prev != "" && token == prev {
		return false, "", fmt.Errorf("分页失败: page_token %q 未前进，已停止以免死循环", token)
	}
	return true, token, nil
}
