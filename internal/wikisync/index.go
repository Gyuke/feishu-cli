package wikisync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// 订阅状态常量，用于 IndexEntry.SubscribeStatus。
const (
	SubscribeStatusNone       = ""
	SubscribeStatusSubscribed = "subscribed"
	SubscribeStatusFailed     = "failed"
)

// IndexEntry 对应一条「本地 Markdown ↔ 云端文档」映射，供 RAG 来源查询与事件反查。
type IndexEntry struct {
	TaskID           string `json:"task_id"`
	SpaceID          string `json:"space_id"`
	NodeToken        string `json:"node_token"`
	ObjToken         string `json:"obj_token"`
	ObjType          string `json:"obj_type"`
	Title            string `json:"title"`
	ParentNodeToken  string `json:"parent_node_token"`
	HasChild         bool   `json:"has_child"`
	NodeType         string `json:"node_type"`
	ObjEditTime      string `json:"obj_edit_time"`
	WikiURL          string `json:"wiki_url"`
	LocalPath        string `json:"local_path"`
	SubscribeStatus  string `json:"subscribe_status"`
	LastSubscribedAt string `json:"last_subscribed_at"`
	// SyncStatus 记录条目相对最后一次对账的种类：""（正常/当前）、"gone"（云端已删/移出/回收站）。
	// 由 reconcile 写入；readonly/订阅/重导均忽略。
	SyncStatus string `json:"sync_status,omitempty"`
}

// LocalPath 返回本地相对路径（来自 index 项，适合直接用于事件反查）。
// 字段已在写入时序列化为相对 local_dir 的 / 分隔路径。
func (e *IndexEntry) LocalPathSlash() string {
	return filepath.ToSlash(e.LocalPath)
}

// LoadIndex 读取合并索引；文件不存在时返回空切片（首次运行场景）。
func LoadIndex(path string) ([]IndexEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取索引 %s 失败: %w", path, err)
	}
	var entries []IndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("解析索引 %s 失败: %w", path, err)
	}
	return entries, nil
}

// SaveIndex 写入合并索引，确保父目录存在。
func SaveIndex(path string, entries []IndexEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("创建索引目录失败: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化索引失败: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("写入索引 %s 失败: %w", path, err)
	}
	return nil
}

// FindByObjToken 在索引中按 obj_token 反查条目，命中返回 (条目, true)。
func FindByObjToken(entries []IndexEntry, objToken string) (IndexEntry, bool) {
	for _, e := range entries {
		if e.ObjToken == objToken {
			return e, true
		}
	}
	return IndexEntry{}, false
}
