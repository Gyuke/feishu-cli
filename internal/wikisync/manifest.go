package wikisync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest 记录单个任务实际写入的文件清单，供 clean 只删本任务文件、事件调度反查。
type Manifest struct {
	TaskID   string   `json:"task_id"`
	WikiURL  string   `json:"wiki_url"`
	LocalDir string   `json:"local_dir"`
	Files    []string `json:"files"`
}

// ManifestPath 返回该 task 的 manifest 文件路径。
// layout: <local_dir>/.feishu-cli/manifests/<task_hash>.json
func (m *Manifest) ManifestPath() string {
	return filepath.Join(m.LocalDir, ".feishu-cli", "manifests", m.TaskID+".json")
}

// SaveManifest 写入任务 manifest，确保父目录存在。
func SaveManifest(m *Manifest) error {
	path := m.ManifestPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("创建 manifest 目录失败: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 manifest 失败: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("写入 manifest %s 失败: %w", path, err)
	}
	return nil
}

// LoadManifest 读取单任务 manifest。
// identity 是任务的稳定身份字符串（TaskID 的哈希输入）：单节点任务传 wiki_url，space 任务传 "space:"+space_id。
func LoadManifest(identity, localDir string) (*Manifest, error) {
	m := &Manifest{TaskID: TaskID(identity), WikiURL: identity, LocalDir: localDir}
	data, err := os.ReadFile(m.ManifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 manifest %s 失败: %w", m.ManifestPath(), err)
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("解析 manifest %s 失败: %w", m.ManifestPath(), err)
	}
	return m, nil
}
