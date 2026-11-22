//go:build !windows

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	adbExeName = "adb"
	mirrorHint = "未找到 scrcpy（sudo apt install scrcpy 可安装）。现在可先用「内置预览」查看设备画面，并配合虚拟遥控器操作"
)

// configDir follows the XDG Base Directory specification on Unix-like systems.
func configDir() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, dataDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), dataDirName)
	}
	return filepath.Join(home, ".config", dataDirName)
}

func extraAdbCandidates(exeDir string) []string {
	return []string{
		"/usr/lib/pcwebadb/adb",
		"/usr/share/pcwebadb/adb",
		filepath.Join(exeDir, "..", "lib", "pcwebadb", "adb"),
		filepath.Join(exeDir, "..", "share", "pcwebadb", "adb"),
		filepath.Join(exeDir, "..", "vendor", "platform-tools", "adb"),
		filepath.Join(exeDir, "adb"),
	}
}

func isExecutableFile(fi os.FileInfo, _ string) bool {
	return !fi.IsDir() && fi.Mode()&0o111 != 0
}

// The .deb ships adb at /usr/lib/pcwebadb/adb, so no extraction is needed on
// Unix. These platform hooks keep main.go free of GOOS conditionals.
func bundledAdbCandidates() []string    { return nil }
func adbBundleFailure() string          { return "" }
func platformStartupWarnings() []string { return nil }

func adbNotFoundHelp() string {
	return "未找到 adb。请重新安装本程序（安装包内置 Android SDK Platform Tools），" +
		"或用 --adb <路径> 指定 adb 可执行文件。"
}

// tuneCommand is a no-op on Unix: child processes inherit no console window.
func tuneCommand(_ *exec.Cmd) {}

func fatal(msg string) {
	appLogf("error", "%s", msg)
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

// platformFatalReport mirrors the Windows panic path on Unix.
func platformFatalReport(msg string) {
	appLogf("error", "%s", msg)
	fmt.Fprintln(os.Stderr, msg)
	closeAppLog()
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// Single instance
//
// A second GUI launch must not start a second adb service on another port.
// Instead it asks the running instance to show its window through the private
// loopback endpoint. This is also the recovery path on desktops that hide the
// notification area entirely (GNOME without an extension, some Wayland
// sessions): launching the app again always brings the window back.
// ---------------------------------------------------------------------------

var instanceLock *os.File

func instancePaths() (string, string) {
	d := configDir()
	return filepath.Join(d, "instance.lock"), filepath.Join(d, "instance.addr")
}

func claimSingleInstance() bool {
	lockPath, addrPath := instancePaths()
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return true // no writable config dir: do not block startup
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if data, e := os.ReadFile(addrPath); e == nil && len(data) > 0 {
			u := strings.TrimSpace(string(data))
			if strings.HasPrefix(u, "http://127.0.0.1:") {
				cli := &http.Client{Timeout: 2 * time.Second}
				if resp, e := cli.Get(u + "/api/raise"); e == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}
		}
		fmt.Println(appName + " 已在运行：已请求显示其窗口（不会启动第二个服务）。")
		return false
	}
	instanceLock = f // the lock is held until the process exits
	return true
}

func publishInstance(url string) {
	if instanceLock == nil {
		return
	}
	_, p := instancePaths()
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimSuffix(url, "/")), 0o600); err == nil {
		_ = os.Rename(tmp, p)
	}
}

// ---------------------------------------------------------------------------
// Desktop shell
//
// Default: a real GTK3 window with an embedded WebKitGTK view. It has its own
// title bar, taskbar entry and WM_CLASS, no address bar and no browser tabs.
// The loopback HTTP endpoint is an internal transport between the Go backend
// and the embedded view - it is never exposed to the user or to the network.
//
// Closing the window HIDES it: the service stays alive in the background with
// a notification-area icon. Only the tray menu, a termination signal or an
// explicit quit stops it. If GTK/WebKitGTK is missing, we degrade to a
// Chromium "--app" window (also chrome-less and standalone), then to the
// default browser, and finally to a pure service mode.
// ---------------------------------------------------------------------------

var chromiumBrowsers = []string{
	"google-chrome-stable", "google-chrome", "chromium", "chromium-browser",
	"microsoft-edge-stable", "microsoft-edge", "brave-browser", "vivaldi", "opera",
}

var geckoBrowsers = []string{"firefox", "firefox-esr"}

// appWindowCommand builds a chrome-less, standalone window command.
func appWindowCommand(url string) (string, []string, bool) {
	if v := os.Getenv("SKYADB_BROWSER"); v != "" {
		fields := strings.Fields(v)
		if len(fields) > 0 {
			if p, err := exec.LookPath(fields[0]); err == nil {
				return p, append(fields[1:], url), true
			}
		}
	}
	profile := filepath.Join(configDir(), "app-window-profile")
	_ = os.MkdirAll(profile, 0o700)
	for _, name := range chromiumBrowsers {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		args := []string{
			"--app=" + url,
			"--window-size=1280,820",
			"--class=" + wmClass,
			"--user-data-dir=" + profile,
			"--no-first-run",
			"--no-default-browser-check",
			"--disable-session-crashed-bubble",
			"--disable-features=Translate,MediaRouter",
		}
		return p, args, true
	}
	for _, name := range geckoBrowsers {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		args := []string{"--new-window", url, "--width=1280", "--height=820", "--class=" + wmClass}
		return p, args, true
	}
	for _, name := range []string{"xdg-open", "gio"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		args := []string{url}
		if filepath.Base(p) == "gio" {
			args = []string{"open", url}
		}
		return p, args, true
	}
	return "", nil, false
}

// spawnAppWindow starts the standalone browser window without blocking. It is
// used by /api/raise when the native GTK window is not available.
func spawnAppWindow(url string) bool {
	bin, args, ok := appWindowCommand(url)
	if !ok {
		return false
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { _ = cmd.Wait() }()
	return true
}

// launchAppWindow runs the standalone browser window and blocks until it is
// closed (or a termination signal arrives). It reports whether it was actually
// launched and whether the caller asked to quit via a signal. Closing the
// browser window no longer stops the service.
func launchAppWindow(url string) (bool, bool) {
	bin, args, ok := appWindowCommand(url)
	if !ok {
		return false, false
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return false, false
	}
	fmt.Printf("%s：已打开独立应用窗口（%s）\n", appName, filepath.Base(bin))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	select {
	case <-sig:
		_ = cmd.Process.Kill()
		<-exited
		return true, true
	case <-exited:
		return true, false
	}
}

// runNativeWindowWithSignals lets SIGINT/SIGTERM close the GTK window cleanly.
func runNativeWindowWithSignals(url string) int {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sig:
			requestNativeStop()
		case <-done:
		}
	}()
	return runNativeWindow(url)
}

func nativeWindowError(rc int) string {
	switch rc {
	case 1:
		return "系统缺少 GTK3 / WebKitGTK 运行库"
	case 2:
		return "当前没有可用的图形显示（DISPLAY 未设置或 X11 不可访问）"
	case 3:
		return "GTK/WebKit 关键符号缺失"
	default:
		return fmt.Sprintf("原生窗口退出，代码 %d", rc)
	}
}

// runApp opens the desktop UI and keeps the process alive after the UI is
// closed. Only an explicit quit (tray menu / signal) stops the service.
// windowMode is set from --window in main.go.
func runApp(url, bin string, shutdown func()) {
	mode := windowMode
	if *flagNoBrowser || *flagHeadless {
		mode = "none"
	}
	if mode != "none" {
		fmt.Println(appName + "：关闭窗口不会停止服务，程序会留在后台继续运行。")
		fmt.Println("          通知区域小图标右键可「显示主界面」或「退出」；若桌面环境不显示托盘，再次启动本程序即可恢复窗口。")
	}

	switch mode {
	case "gtk":
		setRaiseHandler(requestNativeShow)
		rc := runNativeWindowWithSignals(url)
		if rc != 0 {
			fmt.Fprintln(os.Stderr, "无法启动原生窗口："+nativeWindowError(rc))
			fmt.Fprintln(os.Stderr, "可改用 --window=browser 或 --window=none。")
		}
		shutdown()
		return

	case "browser":
		setRaiseHandler(func() { spawnAppWindow(url) })
		launched, bySignal := launchAppWindow(url)
		if launched && bySignal {
			shutdown()
			return
		}
		if !launched {
			fmt.Fprintln(os.Stderr, "未找到可用浏览器，转为仅服务模式。")
		}

	case "none":
		// pure service mode, no UI at all

	default: // auto
		setRaiseHandler(requestNativeShow)
		rc := runNativeWindowWithSignals(url)
		if rc == 0 {
			shutdown()
			return
		}
		fmt.Fprintln(os.Stderr, "原生窗口不可用（"+nativeWindowError(rc)+"），改用独立应用窗口。")
		setRaiseHandler(func() { spawnAppWindow(url) })
		launched, bySignal := launchAppWindow(url)
		if launched && bySignal {
			shutdown()
			return
		}
		if !launched {
			fmt.Fprintln(os.Stderr, "未找到可用浏览器，转为仅服务模式。")
		}
	}

	// The window was closed (or no UI could be created). Deliberately keep
	// serving: the already-open browser page and every /api call still work.
	fmt.Printf("%s %s · 后台服务运行中\n  本地地址: %s\n  adb:      %s\n  配置:     %s\n按 Ctrl+C 退出。\n",
		appName, version, url, bin, configDir())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("\n正在退出…")
	shutdown()
}
