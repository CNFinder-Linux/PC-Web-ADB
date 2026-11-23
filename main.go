// SkyADB PC - a desktop ADB management tool whose feature set and interaction
// model follow the GPL-3.0 Android app github.com/sky22333/skyadb.
//
// The upstream app is an Android/Kotlin client and cannot be compiled into a
// native desktop package. This program is an independent Go implementation of
// the same workflow, distributed under GPL-3.0-or-later. Platform-specific
// behaviour lives in platform_unix.go / platform_windows.go (+ gui_windows.go).
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed all:web
var embeddedWeb embed.FS

const (
	version        = "1.1.0"
	upstreamCommit = "bbf01698880f3b09c40dd0672b297fe16fd70ef6"
	// appName is the user-facing product name shown in the UI, window titles
	// and packaging metadata.
	appName = "PC Web ADB (PWA)"
	// wmClass matches StartupWMClass in the .desktop launcher so the dock
	// groups the running window under the application icon.
	wmClass = "PCWebADB"
)

var (
	flagPort        = flag.Int("port", 0, "监听端口 (默认自动选择 127.0.0.1 空闲端口)")
	flagNoBrowser   = flag.Bool("no-browser", false, "不打开任何界面，仅运行本地服务")
	flagWindow      = flag.String("window", "auto", "界面模式: auto|gtk|browser|none (仅 Linux)")
	flagHeadless    = flag.Bool("headless", false, "仅服务模式，等价 --window=none")
	flagAdb         = flag.String("adb", "", "指定 adb 可执行文件")
	flagNewInstance = flag.Bool("new-instance", false, "跳过单实例检测，允许再启动一个独立服务")
	flagVersion     = flag.Bool("version", false, "输出版本号并退出")
	windowMode      = "auto"
	// raiseHandler is installed by the platform shell. It only posts a message
	// / sets a flag, never calls GUI APIs from the HTTP goroutine. The mutex
	// guards the assignment (main goroutine) against /api/raise (HTTP goroutine).
	raiseMu      sync.RWMutex
	raiseHandler func()
)

func setRaiseHandler(h func()) {
	raiseMu.Lock()
	raiseHandler = h
	raiseMu.Unlock()
}

func requestAppRaise() {
	raiseMu.RLock()
	h := raiseHandler
	raiseMu.RUnlock()
	if h != nil {
		h()
	}
}

// singleInstanceWanted keeps the pure service modes free to start several
// processes (fixed port forwarding, CI, scripts) while GUI launches share one.
func singleInstanceWanted() bool {
	return !*flagNewInstance && !*flagHeadless && !*flagNoBrowser && windowMode != "none"
}

func looksLikePath(s string) bool {
	return filepath.IsAbs(s) || strings.ContainsAny(s, `/\`)
}

// resolveAdb follows: --adb > $SKYADB_ADB > saved setting > packaged adb >
// dev vendor adb > PATH. The platform files supply the actual candidate paths,
// the executable suffix and the "is this file executable" test.
func resolveAdb(explicit string) string {
	candidates := []string{}
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if v := os.Getenv("SKYADB_ADB"); v != "" {
		candidates = append(candidates, v)
	}
	if st := NewStore().Get(); st.AdbPath != "" {
		candidates = append(candidates, st.AdbPath)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, extraAdbCandidates(filepath.Dir(exe))...)
	}
	candidates = append(candidates,
		filepath.Join("vendor", "platform-tools", adbExeName),
		filepath.Join("vendor", "platform-tools-win", adbExeName),
	)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if looksLikePath(c) {
			if st, err := os.Stat(c); err == nil && isExecutableFile(st, c) {
				abs, _ := filepath.Abs(c)
				return abs
			}
		} else if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}

	// No external adb at all. Only now materialise the embedded copy (Windows
	// standalone exe); the installed layout must never trigger extraction.
	for _, c := range bundledAdbCandidates() {
		if st, err := os.Stat(c); err == nil && isExecutableFile(st, c) {
			return c
		}
	}
	// Last resort: whatever adb happens to be on PATH (may be an old version).
	if p, err := exec.LookPath(adbExeName); err == nil {
		return p
	}
	return ""
}

func main() {
	// The persistent run log is opened first so every later failure - missing
	// adb, bind errors, panics - leaves a trace the user can send us.
	initAppLog()
	// LIFO: panicLog must run while the log file is still open.
	defer closeAppLog()
	defer panicLog()
	flag.Parse()
	if *flagVersion {
		fmt.Printf("%s %s (reference skyadb@%s)\n", appName, version, upstreamCommit[:7])
		return
	}
	windowMode = strings.ToLower(*flagWindow)
	switch windowMode {
	case "auto", "gtk", "browser", "none":
	default:
		fatal("--window 只能是 auto、gtk、browser 或 none")
		return
	}

	// A second GUI launch asks the running instance to show its window and
	// exits, instead of starting a second adb service on a second port.
	if singleInstanceWanted() && !claimSingleInstance() {
		return
	}

	store := NewStore()
	diag := &Diagnostics{}
	for _, warn := range platformStartupWarnings() {
		diag.Add("warn", "startup", warn)
	}
	bin := resolveAdb(*flagAdb)
	if bin == "" {
		appLogf("error", "未能找到 adb：%s", adbNotFoundHelp())
		fatal(adbNotFoundHelp())
		return
	}
	adb := &Adb{Bin: bin, Settings: store.Get, Diag: diag}
	adbVer := adb.Version()
	appLogf("info", "adb=%s (%s)", bin, adbVer)
	diag.Add("info", "startup", appName+" "+version+" 启动，adb="+bin+" ("+adbVer+")")
	if adbVer == "unknown" {
		// Most common cause on Win7 SP1: the Universal CRT is missing, so adb.exe
		// cannot be loaded at all. Say so instead of letting every command fail
		// with an empty error message.
		diag.Add("error", "adb", "adb 无法执行。请确认 adb.exe 完整、未被安全软件隔离；"+
			"Windows 7 SP1 还需 KB2999226 / VC++ 2015-2019 运行库（ucrtbase.dll）。")
	}

	sub, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		fatal("内嵌前端资源错误: " + err.Error())
		return
	}
	srv := NewServer(adb, store, diag, http.FS(sub))

	addr := fmt.Sprintf("127.0.0.1:%d", *flagPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		appLogf("error", "监听 %s 失败: %v", addr, err)
		fatal("监听失败: " + err.Error())
		return
	}
	url := "http://" + ln.Addr().String() + "/"
	// Publish the address so a second launch can reach /api/raise.
	publishInstance(url)

	httpServer := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "服务错误:", err)
		}
	}()

	shutdown := func() {
		diag.Add("info", "shutdown", "服务已停止")
		httpServer.Close()
	}
	runApp(url, bin, shutdown)
}
