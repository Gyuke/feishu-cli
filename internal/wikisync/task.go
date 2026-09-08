package wikisync

import (
	"crypto/sha256"
	"encoding/hex"
)

// TaskID 计算一个同步任务（query）的稳定身份标识。
//
// 规则：hex(sha256(wiki_url))，输入为配置里原始的 wiki_url 字符串。
// 只依赖 wiki_url、不依赖 local_dir 或 name，因此目录移动、进程重启后身份不变；
// 该值同时用于任务 manifest 路径：<local_dir>/.feishu-cli/manifests/<task_hash>.json。
func TaskID(wikiURL string) string {
	sum := sha256.Sum256([]byte(wikiURL))
	return hex.EncodeToString(sum[:])
}

// TaskIDForSpace 计算"整个知识库（space）"同步任务的身份标识。
// 规则：hex(sha256("space:"+space_id))。与 TaskID 的 wiki_url 规则相互隔离，
// 避免同一字符串在两种语义下撞出相同 ID。space 任务 manifest 路径也用它。
func TaskIDForSpace(spaceID string) string {
	return TaskID("space:" + spaceID)
}

// ContentHash 计算任意字节串的 sha256（十六进制）。用于派生"配置身份"
// 的全局状态目录：~/.feishu-cli/state/wiki-sync/<config_hash>。
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
