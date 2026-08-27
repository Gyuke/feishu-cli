package cmd

import (
	"fmt"

	"github.com/riba2534/feishu-cli/internal/client"
)

// listMessagesViaSearch 通过搜索 + mget 获取消息列表。
// 当 ListMessages API 返回空结果（bot 不在群）时作为降级方案。
//
// cardContentType 透传到 /im/v1/messages/mget，fallback 路径与主路径保持一致。
func listMessagesViaSearch(chatID string, pageSize int, pageToken, userAccessToken, cardContentType string) (*client.ListMessagesResult, error) {
	if pageSize <= 0 {
		pageSize = 20
	}

	// Search API query 参数不能为空，传空格作为通配
	searchOpts := client.SearchMessagesOptions{
		Query:     " ",
		ChatIDs:   []string{chatID},
		PageSize:  pageSize,
		PageToken: pageToken,
	}

	searchResult, err := client.SearchMessages(searchOpts, userAccessToken)
	if err != nil {
		return nil, fmt.Errorf("搜索消息失败: %w", err)
	}

	if len(searchResult.MessageIDs) == 0 {
		return &client.ListMessagesResult{}, nil
	}

	batch, err := client.BatchGetMessagesBestEffort(searchResult.MessageIDs, userAccessToken, cardContentType)
	if err != nil {
		return nil, fmt.Errorf("批量获取消息失败: %w", err)
	}

	result := &client.ListMessagesResult{
		HasMore:   searchResult.HasMore,
		PageToken: searchResult.PageToken,
	}
	if batch != nil {
		for _, msg := range batch.Messages {
			if msg != nil {
				result.Items = append(result.Items, msg)
			}
		}
		result.MergeForwardSubMessages = batch.MergeForwardSubMessages
	}
	return result, nil
}
