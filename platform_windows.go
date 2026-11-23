//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	adbExeName = "adb.exe"
	mirrorHint = "未找到 scrcpy（下载 scrcpy Windows 版并加入 PATH 即可启用）。现在可先用「内置预览」查看设备画面"
)

// configDir uses the per-user roaming AppData folder on Windows. Win7 has no
// XDG layout and a dot-prefixed directory in %USERPROFILE% would be unusual.
func configDir() string {
	if v := os.Getenv("APPDATA"); v != "" {
		return filepath.Join(v, "PC Web ADB")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "AppData", "Roaming", "PC Web ADB")
	}
	return filepath.Join(os.TempDir(), "PC Web ADB")
}

func extraAdbCandidates(exeDir string) []string {
	return []string{
		filepath.Join(exeDir, "adb.exe"),
		filepath.Join(exeDir, "bin", "adb.exe"),
		filepath.Join(exeDir, "platform-tools", "adb.exe"),
		filepath.Join(exeDir, "..", "bin", "adb.exe"),
		filepath.Join(exeDir, "..", "vendor", "platform-tools-win", "adb.exe"),
	}
}

// Windows does not expose POSIX execute bits on FAT/NTFS through os.FileMode,
// so only the filesystem entry type is checked here.
func isExecutableFile(fi os.FileInfo, _ string) bool {
	return !fi.IsDir()
}

// tuneCommand suppresses the console window that Windows would otherwise
// allocate for each short-lived adb.exe / scrcpy.exe child of a GUI-subsystem
// process. CREATE_NO_WINDOW does not affect real GUI child windows (browsers).
func tuneCommand(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

func fatal(msg string) {
	appLogf("error", "%s", msg)
	messageBox(appName, msg, 0x10) // MB_ICONERROR
	closeAppLog()
	os.Exit(1)
}

// platformFatalReport is the panic path: report, then exit. The stack trace is
// already in the log file when this is called.
func platformFatalReport(msg string) {
	messageBox(appName, msg, 0x10)
	closeAppLog()
	os.Exit(1)
}

// runApp hosts the HTTP service inside a native Win32 window so the GUI
// subsystem executable is visible, closable and never flashes a console.
func runApp(url, bin string, shutdown func()) {
	runGUI(url, bin, shutdown)
}

// browserCandidates lists modern browsers that can render the embedded Web UI.
// Win7's own IE (8/11) cannot render it, so we prefer an installed Chromium or
// Firefox and only fall back to the shell's default association.
var browserCandidates = []struct {
	rel  string
	name string
}{
	{`Google\Chrome\Application\chrome.exe`, "Google Chrome"},
	{`Microsoft\Edge\Application\msedge.exe`, "Microsoft Edge"},
	{`Mozilla Firefox\firefox.exe`, "Mozilla Firefox"},
	{`Google\Chrome SxS\Application\chrome.exe`, "Chrome Canary"},
	{`BraveSoftware\Brave-Browser\Application\brave.exe`, "Brave"},
	{`Vivaldi\Application\vivaldi.exe`, "Vivaldi"},
	{`Opera Software\Opera Stable\opera.exe`, "Opera"},
	{`360chrome\chrome\Application\360chrome.exe`, "360 浏览器"},
	{`Tencent\QQBrowser\QQBrowser.exe`, "QQ 浏览器"},
	{`Sogou\SogouExplorer\SogouExplorer.exe`, "搜狗浏览器"},
}

func detectBrowser() (string, string) {
	roots := []string{}
	for _, key := range []string{"LOCALAPPDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "ProgramW6432"} {
		if v := os.Getenv(key); v != "" {
			roots = append(roots, v)
		}
	}
	for _, root := range roots {
		for _, c := range browserCandidates {
			p := filepath.Join(root, c.rel)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, c.name
			}
		}
	}
	for _, exe := range []string{"msedge.exe", "chrome.exe", "firefox.exe"} {
		if p, err := exec.LookPath(exe); err == nil {
			return p, exe
		}
	}
	return "", ""
}

// openBrowser returns a human-readable status line for the launcher window.
func openBrowser(url string) string {
	if bin, name := detectBrowser(); bin != "" {
		c := exec.Command(bin, url)
		c.Stdout = nil
		c.Stderr = nil
		if err := c.Start(); err == nil {
			return "已在 " + name + " 中打开管理界面。"
		}
	}
	if shellExecute(url) {
		return "已请求系统默认浏览器打开界面；若页面显示异常，请安装 Chrome / Edge / Firefox 后重试。"
	}
	return "未能自动打开浏览器，请复制上方地址到现代浏览器中打开。"
}

func shellExecute(url string) bool {
	u, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return false
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	r, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(u)),
		0, 0, 1, // SW_SHOWNORMAL
	)
	return r > 32
}
