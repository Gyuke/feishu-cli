package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
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
// Digest 是 cached payload 的 SHA-256 hex；缺 digest 的旧 meta 不得 overlay。
type CacheMeta struct {
	LastCheckAt int64  `json:"last_check_at"`
	Version     string `json:"version,omitempty"`
	Brand       string `json:"brand,omitempty"`
	Digest      string `json:"digest,omitempty"`
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

func officialMetaHost() string {
	if configuredBrand == brandLark {
		return "open.larksuite.com"
	}
	return "open.feishu.cn"
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func metaURLOverride() (string, error) {
	raw := strings.TrimSpace(testMetaURL)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FEISHU_CLI_META_URL"))
	}
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("remote meta: 无效的 FEISHU_CLI_META_URL: %w", err)
	}
	if !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("remote meta: FEISHU_CLI_META_URL 仅允许 loopback，得到 %q", u.Hostname())
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("remote meta: FEISHU_CLI_META_URL 仅允许 http/https")
	}
	return raw, nil
}

func validateMetaRequestURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("remote meta: 空 URL")
	}
	host := u.Hostname()
	if isLoopbackHost(host) {
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("remote meta: loopback 仅允许 http/https")
		}
		return nil
	}
	want := officialMetaHost()
	if u.Scheme != "https" {
		return fmt.Errorf("remote meta: 非测试请求仅允许 https://%s，得到 %s://%s", want, u.Scheme, host)
	}
	if !strings.EqualFold(host, want) {
		return fmt.Errorf("remote meta: host %q 不是当前品牌官方 host %q", host, want)
	}
	return nil
}

func checkMetaRedirect(req *http.Request, via []*http.Request) error {
	if req != nil {
		req.Header.Del("Authorization")
	}
	if req == nil || req.URL == nil {
		return fmt.Errorf("remote meta: 无效重定向")
	}
	if err := validateMetaRequestURL(req.URL); err != nil {
		return err
	}
	if len(via) == 0 {
		return nil
	}
	if len(via) >= 5 {
		return fmt.Errorf("remote meta: too many redirects")
	}
	prev := via[len(via)-1]
	if prev != nil && prev.URL != nil && prev.URL.Scheme == "https" && req.URL.Scheme == "http" {
		return fmt.Errorf("remote meta: 拒绝 HTTPS→HTTP 重定向")
	}
	if prev != nil && prev.URL != nil && !strings.EqualFold(prev.URL.Hostname(), req.URL.Hostname()) {
		return fmt.Errorf("remote meta: 拒绝跨 host 重定向 %q → %q", prev.URL.Hostname(), req.URL.Hostname())
	}
	return nil
}

func remoteMetaURL(version string) (string, error) {
	override, err := metaURLOverride()
	if err != nil {
		return "", err
	}
	if override != "" {
		return override, nil
	}
	host := "https://" + officialMetaHost()
	q := "protocol=meta&client_version=" + url.QueryEscape(clientVersion())
	if version != "" {
		q += "&data_version=" + url.QueryEscape(version)
	}
	return host + "/api/tools/open/api_definition?" + q, nil
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

func cacheRoot() (string, bool) {
	if d := strings.TrimSpace(os.Getenv("FEISHU_CLI_CONFIG_DIR")); d != "" {
		return filepath.Join(d, "cache"), true
	}
	root, err := profile.RootDir()
	if err != nil || strings.TrimSpace(root) == "" {
		return "", false
	}
	return filepath.Join(root, "cache"), true
}

func cacheDir() string {
	dir, ok := cacheRoot()
	if !ok {
		return ""
	}
	return dir
}

func cacheEnabled() bool {
	_, ok := cacheRoot()
	return ok
}

func cachePath() string {
	dir := cacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "remote_meta.json")
}

func cacheMetaPath() string {
	dir := cacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "remote_meta.meta.json")
}

func cacheWritable() bool {
	if !cacheEnabled() {
		return false
	}
	dir := cacheDir()
	if dir == "" {
		return false
	}
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

func payloadDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadCachedMerged() (payload []byte, reg *MergedRegistry, err error) {
	path := cachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var parsed MergedRegistry
	if err := json.Unmarshal(data, &parsed); err != nil {
		// 损坏 cache fail-closed：删除后回退 embedded
		_ = os.Remove(path)
		_ = os.Remove(cacheMetaPath())
		return nil, nil, err
	}
	return data, &parsed, nil
}

func saveCachedMerged(data []byte, cm CacheMeta) error {
	if err := os.MkdirAll(cacheDir(), 0700); err != nil {
		return err
	}
	cm.Digest = payloadDigest(data)
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
		Timeout:       fetchTimeoutDuration(),
		CheckRedirect: checkMetaRedirect,
	}
}

func validRemoteRegistry(reg *MergedRegistry) bool {
	if reg == nil || strings.TrimSpace(reg.Version) == "" {
		return false
	}
	for _, svc := range reg.Services {
		name, _ := svc["name"].(string)
		if strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

func cachePairEligible(cm CacheMeta, cached *MergedRegistry, payload []byte) bool {
	if cached == nil {
		return false
	}
	if strings.TrimSpace(cm.Digest) == "" {
		return false
	}
	if cm.Digest != payloadDigest(payload) {
		return false
	}
	if cm.Brand != configuredBrand {
		return false
	}
	if strings.TrimSpace(cm.Version) == "" || strings.TrimSpace(cached.Version) == "" {
		return false
	}
	if cm.Version != cached.Version {
		return false
	}
	if !isNewer(cached.Version, embeddedVersion) {
		return false
	}
	return validRemoteRegistry(cached)
}

func invalidateCacheFiles() {
	_ = os.Remove(cachePath())
	_ = os.Remove(cacheMetaPath())
}

// fetchRemoteMerged 拉取官方 public api_definition?protocol=meta。
// 不携带 App/User 凭证。HTTP 304 或空 services 视为 unchanged（reg==nil）。
func fetchRemoteMerged(localVersion string) (data []byte, reg *MergedRegistry, err error) {
	rawURL, err := remoteMetaURL(localVersion)
	if err != nil {
		return nil, nil, err
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	if err := validateMetaRequestURL(parsedURL); err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
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
	if !validRemoteRegistry(&parsed) {
		return nil, nil, fmt.Errorf("remote meta: 缺少 version 或有效 named service")
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

	if brandChanged {
		invalidateCacheFiles()
		if len(mergedServices) == 0 {
			doSyncFetch(embeddedVersion)
		} else {
			// 有 embedded baseline：品牌切换才同步拉取；失败/unchanged 不得让旧品牌数据被信任。
			doSyncFetch(embeddedVersion)
		}
		return
	}

	forceRefresh := false
	if metaErr == nil {
		if strings.TrimSpace(cm.Digest) == "" {
			forceRefresh = true
		} else if payload, cached, err := loadCachedMerged(); err != nil {
			forceRefresh = true
		} else if cm.Digest != payloadDigest(payload) || cm.Version != cached.Version {
			forceRefresh = true
		} else if cachePairEligible(cm, cached, payload) {
			overlayMergedServices(cached)
			overlaySource = "cache"
			runtimeVersion = cached.Version
		} else if !validRemoteRegistry(cached) {
			forceRefresh = true
		}
	}

	if len(mergedServices) == 0 {
		doSyncFetch(embeddedVersion)
		return
	}
	if shouldRefresh(cm) || metaErr != nil || forceRefresh {
		triggerBackgroundRefresh()
	}
}

func doSyncFetch(dataVersion string) {
	data, reg, err := fetchRemoteMerged(dataVersion)
	now := time.Now().Unix()
	if err != nil || reg == nil {
		// unchanged/失败：只记检查时间。Version 仅在确有完整 cache pair 时保留，
		// 品牌切换后文件已删，空 Version 防止下一进程把残缺 pair 当 overlay。
		ver := ""
		if path := cachePath(); path != "" {
			if _, statErr := os.Stat(path); statErr == nil {
				ver = dataVersion
			}
		}
		_ = saveCacheMeta(CacheMeta{
			LastCheckAt: now,
			Version:     ver,
			Brand:       configuredBrand,
		})
		return
	}
	if !validRemoteRegistry(reg) {
		return
	}
	cm := CacheMeta{LastCheckAt: now, Version: reg.Version, Brand: configuredBrand}
	if err := saveCachedMerged(data, cm); err != nil {
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
	cm, metaErr := loadCacheMeta()
	if metaErr == nil && cm.Brand != "" && cm.Brand != configuredBrand {
		return
	}
	version := cm.Version
	if version == "" {
		version = embeddedVersion
	}
	data, reg, err := fetchRemoteMerged(version)
	now := time.Now().Unix()
	if err != nil || reg == nil {
		if metaErr != nil {
			_ = saveCacheMeta(CacheMeta{LastCheckAt: now, Brand: configuredBrand})
			return
		}
		cm.LastCheckAt = now
		_ = saveCacheMeta(cm)
		return
	}
	if !validRemoteRegistry(reg) {
		return
	}
	newMeta := CacheMeta{LastCheckAt: now, Version: reg.Version, Brand: configuredBrand}
	_ = saveCachedMerged(data, newMeta)
}

func waitBackgroundRefresh() {
	bgRefreshInFlight.Wait()
}

func reinitKeepingHooks() {
	waitBackgroundRefresh()
	initOnce = sync.Once{}
	mergedServices = make(map[string]map[string]interface{})
	embeddedServices = make(map[string]map[string]interface{})
	mergedProjectList = nil
	embeddedVersion = ""
	overlaySource = ""
	runtimeVersion = ""
	refreshOnce = sync.Once{}
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
