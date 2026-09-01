package wikisync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EditEvent 是从 drive.file.edit_v1 / deleted_v1 事件里抽出的最小信息。
// 字段来自 keys.go 中登记的 PayloadSchema（schema 2.0）。
type EditEvent struct {
	EventID        string   `json:"event_id"`
	EventType      string   `json:"event_type"`
	CreateTime     string   `json:"create_time"`
	FileType       string   `json:"file_type"`
	FileToken      string   `json:"file_token"` // obj_token（docx 为 document_id，sheet 为 spreadsheet token）
	OperatorIDList []string `json:"operator_id_list"`
}

// ParseEditEvent 从原始事件字节解析出 EditEvent。
// 入参是 ev.Body（[]byte），不依赖 SDK 类型，便于独立单测。
func ParseEditEvent(body []byte) (EditEvent, error) {
	var raw struct {
		Header struct {
			EventID    string `json:"event_id"`
			EventType  string `json:"event_type"`
			CreateTime string `json:"create_time"`
		} `json:"header"`
		Event struct {
			FileType       string `json:"file_type"`
			FileToken      string `json:"file_token"`
			OperatorIDList []struct {
				OpenID string `json:"open_id"`
			} `json:"operator_id_list"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return EditEvent{}, fmt.Errorf("解析事件失败: %w", err)
	}
	if raw.Event.FileToken == "" {
		return EditEvent{}, fmt.Errorf("事件缺少 file_token（event_type=%s）", raw.Header.EventType)
	}
	ev := EditEvent{
		EventID:    raw.Header.EventID,
		EventType:  raw.Header.EventType,
		CreateTime: raw.Header.CreateTime,
		FileType:   raw.Event.FileType,
		FileToken:  raw.Event.FileToken,
	}
	for _, op := range raw.Event.OperatorIDList {
		if op.OpenID != "" {
			ev.OperatorIDList = append(ev.OperatorIDList, op.OpenID)
		}
	}
	return ev, nil
}

// Debouncer 按 file_token 聚合编辑事件：同一 token 的连续编辑只触发一次处理
// （trailing edge debounce）。由外部 ticker 驱动 Drain，纯内存、无计时器/goroutine。
type Debouncer struct {
	mu       sync.Mutex
	interval time.Duration
	action   func(token string, last EditEvent)
	pending  map[string]pendingEdit
}

type pendingEdit struct {
	last   EditEvent
	seenAt time.Time
}

// NewDebouncer 构造一个 debounce 聚合器；action 在窗口安静后触发（在调用 Drain 的协程里执行）。
func NewDebouncer(interval time.Duration, action func(token string, last EditEvent)) *Debouncer {
	return &Debouncer{
		interval: interval,
		action:   action,
		pending:  make(map[string]pendingEdit),
	}
}

// Add 记录一次编辑事件，重置该 token 的窗口计时。
func (d *Debouncer) Add(ev EditEvent, now time.Time) {
	if ev.FileToken == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending[ev.FileToken] = pendingEdit{last: ev, seenAt: now}
}

// Drain 触发所有"已安静 >= interval"的 token，并清空它们。now 由调用方传入（便于测试）。
// 返回本次触发的 token 数。
func (d *Debouncer) Drain(now time.Time) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	fired := 0
	for token, p := range d.pending {
		if now.Sub(p.seenAt) >= d.interval {
			d.action(token, p.last)
			delete(d.pending, token)
			fired++
		}
	}
	return fired
}

// Change 记录一次事件驱动的定向重导结果，写入 .feishu-cli/changes/。
type Change struct {
	FileToken string `json:"file_token"`
	ObjToken  string `json:"obj_token"`
	NodeToken string `json:"node_token"`
	LocalPath string `json:"local_path"`
	Title     string `json:"title"`
	EventID   string `json:"event_id,omitempty"`
	ChangedAt string `json:"changed_at"`
	Status    string `json:"status"` // updated | unchanged | failed | missing
	Error     string `json:"error,omitempty"`
}

// ChangesDir 返回指定本地目录的变更记录目录。
func ChangesDir(localDir string) string {
	return filepath.Join(localDir, ".feishu-cli", "changes")
}

// ChangesFilePath 返回当日变更记录文件（<Y-m-d>.jsonl，逐行追加）。
func ChangesFilePath(localDir string, ts time.Time) string {
	return filepath.Join(ChangesDir(localDir), ts.Format("2006-01-02")+".jsonl")
}

// AppendChange 把一条变更记录追加到当日的 jsonl 文件。
func AppendChange(localDir string, c Change) error {
	if c.ChangedAt == "" {
		c.ChangedAt = time.Now().Format(time.RFC3339)
	}
	path := ChangesFilePath(localDir, time.Now())
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("创建变更目录失败: %w", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化变更记录失败: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("打开变更记录失败: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("写入变更记录失败: %w", err)
	}
	return nil
}

// CleanRelPath 把一个绝对/相对路径规整为相对 local_dir 的 / 分隔路径；不在 local_dir 内则保留原样。
func CleanRelPath(localDir, p string) string {
	rel, err := filepath.Rel(localDir, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}
