package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riba2534/feishu-cli/internal/profile"
)

const (
	defaultMetaTTL     = 86400    // seconds (24h)
	defaultMaxResponse = 10 << 20 // 10 MB
	defaultFetchTO     = 5 * time.Second
	brandFeishu        = "feishu"
	brandLark          = "lark"
)

// CacheMeta 描述 remote_meta.json 旁路的版本/品牌/检查时间。
type CacheMeta struct {
	LastCheckAt int64  `json:"last_check_at"`
	Version     string `json:"version,omitempty"`
	Brand       string `json:"brand,omitempty"`
}

type remoteResponse struct {
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type httpError struct {
	StatusCode int
}

func (e *httpError) Error() string {
	return "remote meta: HTTP " + strconv.Itoa(e.StatusCode)
}

var (
	configuredBrand  = brandFeishu
	embeddedVersion  string
	embeddedServices = make(map[string]map[string]interface{})
	overlaySource    string // "", "cache", "runtime"
	runtimeVersion   string

	enableRemoteMeta = true
	remoteForcedOff  bool
	testMetaURL      string
	maxResponseSize  = int64(defaultMaxResponse)
	fetchTimeout     = defaultFetchTO

	atomicWriteFn = atomicWriteFile

	refreshOnce       sync.Once
	bgRefreshInFlight sync.WaitGroup
)

func remoteEnabled() bool {
	if remoteForcedOff || !enableRemoteMeta {
		return false
	}
	if os.Getenv("FEISHU_CLI_REMOTE_META") == "off" {
		return false
	}
	// 单元测试默认不打真实网络；显式 META_URL 或 REMOTE_META=on 才开启。
	if testing.Testing() && os.Getenv("FEISHU_CLI_REMOTE_META") != "on" && os.Getenv("FEISHU_CLI_META_URL") == "" && testMetaURL == "" {
		return false
	}
	return true
}

// DisableRemoteForProcess 供 doctor --offline 等场景关闭远端 overlay（不影响已 Init 的结果）。
func DisableRemoteForProcess() {
	remoteForcedOff = true
}

func resolveBrand() string {
	if b := strings.ToLower(strings.TrimSpace(os.Getenv("FEISHU_CLI_BRAND"))); b == brandLark || b == "larksuite" {
		return brandLark
	}
	if b := strings.ToLower(strings.TrimSpace(os.Getenv("FEISHU_CLI_BRAND"))); b == brandFeishu {
		return brandFeishu
	}
	base := strings.ToLower(os.Getenv("FEISHU_BASE_URL"))
	if strings.Contains(base, "larksuite.com") {
		return brandLark
	}
	return brandFeishu
}

func remoteMetaURL(version string) string {
	if testMetaURL != "" {
		return testMetaURL
	}
	if u := strings.TrimSpace(os.Getenv("FEISHU_CLI_META_URL")); u != "" {
		return u
	}
	host := "https://open.feishu.cn"
	if configuredBrand == brandLark {
		host = "https://open.larksuite.com"
	}
	q := "protocol=meta&client_version=" + url.QueryEscape(clientVersion())
	if version != "" {
		q += "&data_version=" + url.QueryEscape(version)
	}
	return host + "/api/tools/open/api_definition?" + q
}

func clientVersion() string {
	if v := strings.TrimSpace(os.Getenv("FEISHU_CLI_VERSION")); v != "" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	return "dev"
}

func metaTTL() time.Duration {
	if s := os.Getenv("FEISHU_CLI_META_TTL"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return defaultMetaTTL * time.Second
}

func fetchTimeoutDuration() time.Duration {
	if s := os.Getenv("FEISHU_CLI_META_TIMEOUT_MS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return fetchTimeout
}

func cacheDir() string {
	if d := strings.TrimSpace(os.Getenv("FEISHU_CLI_CONFIG_DIR")); d != "" {
		return filepath.Join(d, "cache")
	}
	root, err := profile.RootDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "feishu-cli-cache")
	}
	return filepath.Join(root, "cache")
}

func cachePath() string {
	return filepath.Join(cacheDir(), "remote_meta.json")
}

func cacheMetaPath() string {
	return filepath.Join(cacheDir(), "remote_meta.meta.json")
}

func cacheWritable() bool {
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".probe")
	if err := os.WriteFile(probe, []byte{}, 0644); err != nil {
		return false
	}
	_ = os.Remove(probe)
	return true
}

func loadCacheMeta() (CacheMeta, error) {
	var cm CacheMeta
	data, err := os.ReadFile(cacheMetaPath())
	if err != nil {
		return cm, err
	}
	if err = json.Unmarshal(data, &cm); err != nil {
		return cm, err
	}
	return cm, nil
}

func saveCacheMeta(cm CacheMeta) error {
	if err := os.MkdirAll(cacheDir(), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(cm)
	if err != nil {
		return err
	}
	return atomicWriteFn(cacheMetaPath(), data, 0644)
}

func loadCachedMerged() (*MergedRegistry, error) {
	path := cachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reg MergedRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		// 损坏 cache fail-closed：删除后回退 embedded
		_ = os.Remove(path)
		_ = os.Remove(cacheMetaPath())
		return nil, err
	}
	return &reg, nil
}

func saveCachedMerged(data []byte, cm CacheMeta) error {
	if err := os.MkdirAll(cacheDir(), 0700); err != nil {
		return err
	}
	if err := atomicWriteFn(cachePath(), data, 0644); err != nil {
		return err
	}
	return saveCacheMeta(cm)
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	success = true
	return nil
}

func newMetaHTTPClient() *http.Client {
	return &http.Client{
		Timeout: fetchTimeoutDuration(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// overlay 请求禁止携带凭证；重定向时剥离 Authorization。
			req.Header.Del("Authorization")
			if len(via) >= 5 {
				return fmt.Errorf("remote meta: too many redirects")
			}
			return nil
		},
	}
}

// fetchRemoteMerged 拉取官方 public api_definition?protocol=meta。
// 不携带 App/User 凭证。HTTP 304 或空 services 视为 unchanged（reg==nil）。
func fetchRemoteMerged(localVersion string) (data []byte, reg *MergedRegistry, err error) {
	req, err := http.NewRequest(http.MethodGet, remoteMetaURL(localVersion), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Del("Authorization")
	req.Header.Set("Accept", "application/json")

	resp, err := newMetaHTTPClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, &httpError{StatusCode: resp.StatusCode}
	}

	limited := io.LimitReader(resp.Body, maxResponseSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(body)) > maxResponseSize {
		return nil, nil, fmt.Errorf("remote meta: response exceeds %d bytes", maxResponseSize)
	}

	var envelope remoteResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, err
	}
	if envelope.Msg != "succeeded" {
		return nil, nil, fmt.Errorf("remote meta: unexpected msg %q", envelope.Msg)
	}

	var parsed MergedRegistry
	if len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, &parsed); err != nil {
			return nil, nil, fmt.Errorf("remote meta: parse data: %w", err)
		}
	}
	if len(parsed.Services) == 0 {
		return nil, nil, nil
	}
	return envelope.Data, &parsed, nil
}

func overlayMergedServices(reg *MergedRegistry) {
	if reg == nil {
		return
	}
	for _, svc := range reg.Services {
		name, ok := svc["name"].(string)
		if !ok || name == "" {
			continue
		}
		mergedServices[name] = svc
	}
}

func shouldRefresh(cm CacheMeta) bool {
	if cm.LastCheckAt == 0 {
		return true
	}
	return time.Since(time.Unix(cm.LastCheckAt, 0)) > metaTTL()
}

func applyRemoteOverlay() {
	if !remoteEnabled() || !cacheWritable() {
		return
	}
	cm, metaErr := loadCacheMeta()
	brandChanged := metaErr == nil && cm.Brand != "" && cm.Brand != configuredBrand

	if !brandChanged {
		if cached, err := loadCachedMerged(); err == nil && isNewer(cached.Version, embeddedVersion) {
			overlayMergedServices(cached)
			overlaySource = "cache"
			runtimeVersion = cached.Version
		}
	}

	if len(mergedServices) == 0 || brandChanged {
		doSyncFetch()
		return
	}
	if shouldRefresh(cm) || metaErr != nil {
		// 无 cache 的首次拉取走同步（5s 超时，失败静默回退），其余 TTL 到期走后台。
		if metaErr != nil {
			doSyncFetch()
			return
		}
		triggerBackgroundRefresh()
	}
}

func doSyncFetch() {
	version := embeddedVersion
	if cm, err := loadCacheMeta(); err == nil && cm.Version != "" {
		version = cm.Version
	}
	data, reg, err := fetchRemoteMerged(version)
	now := time.Now().Unix()
	if err != nil || reg == nil {
		_ = saveCacheMeta(CacheMeta{
			LastCheckAt: now,
			Version:     version,
			Brand:       configuredBrand,
		})
		return
	}
	cm := CacheMeta{LastCheckAt: now, Version: reg.Version, Brand: configuredBrand}
	if err := saveCachedMerged(data, cm); err != nil {
		// 原子写失败：不应用 overlay、保留 embedded。
		return
	}
	overlayMergedServices(reg)
	overlaySource = "runtime"
	runtimeVersion = reg.Version
}

func triggerBackgroundRefresh() {
	refreshOnce.Do(func() {
		bgRefreshInFlight.Add(1)
		go func() {
			defer bgRefreshInFlight.Done()
			doBackgroundRefresh()
		}()
	})
}

func doBackgroundRefresh() {
	defer func() { _ = recover() }()
	cm, _ := loadCacheMeta()
	version := cm.Version
	if version == "" {
		version = embeddedVersion
	}
	data, reg, err := fetchRemoteMerged(version)
	now := time.Now().Unix()
	if err != nil {
		cm.LastCheckAt = now
		cm.Brand = configuredBrand
		_ = saveCacheMeta(cm)
		return
	}
	if reg == nil {
		cm.LastCheckAt = now
		cm.Brand = configuredBrand
		_ = saveCacheMeta(cm)
		return
	}
	newMeta := CacheMeta{LastCheckAt: now, Version: reg.Version, Brand: configuredBrand}
	_ = saveCachedMerged(data, newMeta)
}

func waitBackgroundRefresh() {
	bgRefreshInFlight.Wait()
}

// ResetForTest 重置 registry 进程级状态，仅供测试使用。
func ResetForTest() {
	waitBackgroundRefresh()
	initOnce = sync.Once{}
	mergedServices = make(map[string]map[string]interface{})
	embeddedServices = make(map[string]map[string]interface{})
	mergedProjectList = nil
	embeddedVersion = ""
	overlaySource = ""
	runtimeVersion = ""
	refreshOnce = sync.Once{}
	configuredBrand = brandFeishu
	enableRemoteMeta = true
	remoteForcedOff = false
	testMetaURL = ""
	maxResponseSize = int64(defaultMaxResponse)
	fetchTimeout = defaultFetchTO
	atomicWriteFn = atomicWriteFile
	cachedScopePriorities = nil
	cachedAutoApproveSet = nil
}
