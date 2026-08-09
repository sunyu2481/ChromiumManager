package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

type runningProfile struct {
	cmd        *exec.Cmd
	done       chan struct{}
	profileID  string
	cookiePath string

	// devtoolsPort 是 Chromium 自选的远程调试端口，0 表示尚未就绪。
	// 仅在 cdpReady 关闭后有效，读写需持有 runningMu。
	devtoolsPort int
	// cdpReady 在 devtoolsPort 填充完毕后关闭；未启用 agent 面时永不关闭。
	cdpReady chan struct{}
	// cdpClients 记录当前通过代理连接的 CDP 客户端数，供 /agent/browsers 观测。
	cdpClients atomic.Int32
}

// cdpPort 返回已就绪的调试端口，未就绪时返回 0。
func (rp *runningProfile) cdpPort() int {
	runningMu.Lock()
	defer runningMu.Unlock()
	return rp.devtoolsPort
}

var (
	runningMu       sync.Mutex
	runningProfiles = map[string]*runningProfile{}
)

// getRunningIDs returns a snapshot of currently running profile IDs.
// Caller must NOT hold runningMu.
func getRunningIDs() []string {
	runningMu.Lock()
	ids := make([]string, 0, len(runningProfiles))
	for id := range runningProfiles {
		ids = append(ids, id)
	}
	runningMu.Unlock()
	slices.Sort(ids)
	return ids
}

// getRunning 返回指定 profile 的运行态句柄。
func getRunning(id string) (*runningProfile, bool) {
	runningMu.Lock()
	defer runningMu.Unlock()
	rp, ok := runningProfiles[id]
	return rp, ok
}

func findBrowserPath() string {
	local := filepath.Join("bin", "chromium", "chrome-wrapper")
	if runtime.GOOS == "windows" {
		local = filepath.Join("bin", "chromium", "chrome.exe")
	}
	for _, name := range []string{"chrome", local} {
		if p, err := exec.LookPath(name); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	return ""
}

// proxyInfo holds the fields we need from a proxy record.
type proxyInfo struct {
	URL      string
	Timezone string
	Lang     string
	Location string
}

// getProxyInfo fetches proxy info (url, timezone, lang) by proxy rowid.
// Returns zero-value struct when proxyRawID <= 0.
func getProxyInfo(proxyRawID int64) proxyInfo {
	if proxyRawID <= 0 {
		return proxyInfo{}
	}
	var info proxyInfo
	if err := db.QueryRow("SELECT url, timezone, lang, location FROM proxies WHERE id=?", proxyRawID).
		Scan(&info.URL, &info.Timezone, &info.Lang, &info.Location); err != nil {
		log.Printf("[DB] getProxyInfo(%d) error: %v", proxyRawID, err)
	}
	return info
}

// enrichFingerprint fills in Seed on new profiles.
func enrichFingerprint(p *Profile, isNew bool) {
	if isNew {
		p.Fingerprint.Seed = rand.Int32()
	}
}

// splitArgs splits a command-line string into arguments.
func splitArgs(s string) []string {
	parts := strings.Split(s, "--")
	var args []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			args = append(args, "--"+p)
		}
	}
	return args
}

// buildLaunchArgs 拼装 Chromium 启动参数。
// 指纹相关开关为 fingerprint-chromium 分支专有，标准 Chromium 会忽略。
func buildLaunchArgs(p *Profile, proxy proxyInfo, userDataDir string) []string {
	args := []string{
		"--user-data-dir=" + userDataDir,
		"--profile-name=" + p.Name,
		"--no-first-run",
		"--no-default-browser-check",
		"--password-store=basic",
	}

	if proxy.URL != "" {
		args = append(args, "--proxy-server="+proxy.URL)
	}

	fp := p.Fingerprint
	if fp.RandomFingerprint && fp.Seed != 0 {
		args = append(args, fmt.Sprintf("--fingerprint=%d", fp.Seed))
	}
	if fp.Platform != "" {
		args = append(args, "--fingerprint-platform="+fp.Platform)
	}
	if fp.Brand != "" {
		args = append(args, "--fingerprint-brand="+fp.Brand)
	}
	if fp.HardwareConcurrency != "" {
		args = append(args, "--fingerprint-hardware-concurrency="+fp.HardwareConcurrency)
	}
	if fp.DeviceMemory != "" {
		args = append(args, "--fingerprint-device-memory="+fp.DeviceMemory)
	}
	if fp.Screen != "" {
		args = append(args, "--fingerprint-screen="+fp.Screen)
	}
	for _, feature := range fp.DisableFeatures {
		switch feature {
		case "webrtc":
			args = append(args, "--force-webrtc-ip-handling-policy")
			args = append(args, "--webrtc-ip-handling-policy=disable_non_proxied_udp")
		}
	}

	// 代理继承：勾选后用代理记录上的值覆盖配置自身的语言/时区/位置
	effLang := fp.Lang
	if fp.ProxyLang && proxy.Lang != "" {
		effLang = proxy.Lang
	}
	effTimezone := fp.Timezone
	if fp.ProxyTimezone && proxy.Timezone != "" {
		effTimezone = proxy.Timezone
	}
	effLocation := fp.Location
	if fp.ProxyLocation && proxy.Location != "" {
		effLocation = proxy.Location
	}
	if effLang != "" {
		args = append(args, "--lang="+effLang)
		args = append(args, "--accept-lang="+effLang)
	}
	if effTimezone != "" {
		args = append(args, "--fingerprint-timezone="+effTimezone)
	}
	if effLocation != "" {
		args = append(args, "--fingerprint-location="+effLocation)
	}
	if len(fp.DisableFingerprint) > 0 {
		args = append(args, "--disable-fingerprint="+strings.Join(fp.DisableFingerprint, ","))
	}

	// 仅在开启 agent 面时才暴露远程调试，避免无谓扩大被检测面。
	// 端口交由 Chromium 自选（写入 DevToolsActivePort），规避端口分配竞态。
	if agentEnabled() {
		args = append(args,
			"--remote-debugging-port=0",
			"--remote-allow-origins=*",
		)
	}

	if p.Args != "" {
		args = append(args, splitArgs(p.Args)...)
	}
	return args
}

// startProfile 启动指定配置的浏览器实例并登记到运行表。
// 已在运行时直接返回现有句柄，调用方可据 started 判断是否为本次拉起。
func startProfile(idStr string) (rp *runningProfile, started bool, err error) {
	if existing, ok := getRunning(idStr); ok {
		return existing, false, nil
	}

	rawID := decodeID(idStr)
	if rawID <= 0 {
		return nil, false, fmt.Errorf("invalid profile id")
	}

	var p Profile
	var rawProxy int64
	var cookieStr string
	if err := db.QueryRow("SELECT id, name, proxy, args, fingerprint, cookie FROM profiles WHERE id=?", rawID).
		Scan(&rawID, &p.Name, &rawProxy, &p.Args, &p.Fingerprint, &cookieStr); err != nil {
		return nil, false, fmt.Errorf("profile not found: %w", err)
	}
	p.ID = idStr

	// 用户数据目录由指纹种子派生，与 deleteProfile 的清理规则保持一致
	userDataDir := filepath.Join(dataDir, "profiles", encodeID(int64(p.Fingerprint.Seed)))
	absUserDataDir, absErr := filepath.Abs(userDataDir)
	if absErr != nil {
		absUserDataDir = userDataDir
	}
	os.MkdirAll(absUserDataDir, 0755)

	// 清理上次异常退出的残留：单例锁会阻止启动，
	// 陈旧的 DevToolsActivePort 会让端口轮询读到已失效的端口
	singletonFiles, _ := filepath.Glob(filepath.Join(absUserDataDir, "Singleton*"))
	for _, f := range singletonFiles {
		os.Remove(f)
	}
	os.Remove(filepath.Join(absUserDataDir, devToolsPortFile))

	cookiePath := filepath.Join(absUserDataDir, "Default", "Cookies")
	if _, statErr := os.Stat(cookiePath); os.IsNotExist(statErr) && cookieStr != "" {
		var cookies []Cookie
		if json.Unmarshal([]byte(cookieStr), &cookies) == nil && len(cookies) > 0 {
			writeCookiesToFile(cookiePath, cookies)
		}
	}

	proxy := getProxyInfo(rawProxy)
	cmd := exec.Command(findBrowserPath(), buildLaunchArgs(&p, proxy, absUserDataDir)...)
	if err := cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("failed to launch: %w", err)
	}

	done := make(chan struct{})
	rp = &runningProfile{
		cmd:        cmd,
		done:       done,
		profileID:  idStr,
		cookiePath: cookiePath,
		cdpReady:   make(chan struct{}),
	}
	runningMu.Lock()
	runningProfiles[idStr] = rp
	runningMu.Unlock()
	broadcastRunning()

	// 异步探测调试端口：UI 启动无需等待，agent 侧则可 select 等 cdpReady
	if agentEnabled() {
		go func() {
			port, err := waitDevToolsPort(absUserDataDir, cdpReadyTimeout, done)
			if err != nil {
				log.Printf("[CDP] profile %s devtools port unavailable: %v", idStr, err)
				return
			}
			runningMu.Lock()
			rp.devtoolsPort = port
			runningMu.Unlock()
			close(rp.cdpReady)
			log.Printf("[CDP] profile %s devtools port ready: %d", idStr, port)
		}()
	}

	// 回收：进程退出后导出 Cookie 并从运行表摘除
	go func() {
		cmd.Wait()
		close(done)
		log.Printf("[Chrome] profile %s (%s) exited", p.Name, idStr)

		if cookies := readCookiesFromFile(cookiePath); len(cookies) > 0 {
			if data, err := json.Marshal(cookies); err == nil {
				db.Exec("UPDATE profiles SET cookie=? WHERE id=?", string(data), rawID)
				log.Printf("[Chrome] exported %d cookies for profile %s", len(cookies), idStr)
			}
		}

		runningMu.Lock()
		delete(runningProfiles, idStr)
		runningMu.Unlock()
		broadcastRunning()
	}()

	return rp, true, nil
}
