package wikisync

import (
	"crypto/sha256"
	"encoding/hex"
)

// TaskID 计算一个同步任务（query）的稳定身份标识。
//
// 规则：hex(sha256(wiki_url))，输入为配置里原始的 wiki_url 字符串。
// 只依赖 wiki_url、不依赖 local_dir 或 name，因此目录移动、进程重启后身份不变；
// 且与 scripts/wiki_export_batch.sh 的 sha256(url) 完全一致，保证 legacy
// .feishu-cli-manifests/<hash>.map.json 文件名可直接复用（Phase 5 兼容）。
func TaskID(wikiURL string) string {
	sum := sha256.Sum256([]byte(wikiURL))
	return hex.EncodeToString(sum[:])
}

// ContentHash 计算任意字节串的 sha256（十六进制）。用于派生"配置身份"
// 的全局状态目录：~/.feishu-cli/state/wiki-sync/<config_hash>。
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
