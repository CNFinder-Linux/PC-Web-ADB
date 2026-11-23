//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// bundledAdbRevision is the last Android SDK Platform Tools release that still
// supports Windows 7. It is part of the extraction directory name so an upgrade
// never mixes binaries from different revisions (a mismatch between adb.exe and
// AdbWinApi.dll is a classic source of "adb server didn't ACK" failures).
const bundledAdbRevision = "34.0.4"

// The three files are embedded into the x64 executable so a single
// PCWebADB-x64.exe copied anywhere can be double-clicked and just work.
// They are separate Apache-2.0 / Android SDK licensed programs distributed as
// an aggregate; see LICENSE and ADB-NOTICE.txt.
//
//go:embed vendor/platform-tools-win/adb.exe
var bundledAdbExe []byte

//go:embed vendor/platform-tools-win/AdbWinApi.dll
var bundledAdbWinApi []byte

//go:embed vendor/platform-tools-win/AdbWinUsbApi.dll
var bundledAdbWinUsbApi []byte

var adbBundleError string

func bundledAdbFiles() map[string][]byte {
	return map[string][]byte{
		"adb.exe":          bundledAdbExe,
		"AdbWinApi.dll":    bundledAdbWinApi,
		"AdbWinUsbApi.dll": bundledAdbWinUsbApi,
	}
}

// bundledAdbDir picks a per-user, writable directory. Writing into
// %ProgramFiles% would require elevation and fails for standard users, which is
// exactly why a plain copy of the exe must not depend on its own folder.
func bundledAdbDir() (string, error) {
	bases := []string{}
	for _, key := range []string{"LOCALAPPDATA", "APPDATA"} {
		if v := os.Getenv(key); v != "" {
			bases = append(bases, v)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		bases = append(bases, filepath.Join(home, "AppData", "Local"))
	}
	bases = append(bases, os.TempDir())

	var last error
	for _, base := range bases {
		dir := filepath.Join(base, "PC Web ADB", "adb", bundledAdbRevision)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			last = err
			continue
		}
		// MkdirAll can succeed on a read-only medium; probe with a real file.
		probe := filepath.Join(dir, ".write-test")
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
			last = err
			continue
		}
		_ = os.Remove(probe)
		return dir, nil
	}
	return "", fmt.Errorf("没有可写的用户目录用于释放内置 adb：%v", last)
}

// bundledAdbCandidates is called only when no external adb was found, so the
// installed layout (adb.exe next to SkyADBPC.exe) never pays for extraction.
func bundledAdbCandidates() []string {
	adbBundleError = ""
	dir, err := bundledAdbDir()
	if err != nil {
		adbBundleError = err.Error()
		return nil
	}
	exe, err := writeBundle(dir, bundledAdbFiles())
	if err != nil {
		adbBundleError = err.Error()
		return nil
	}
	return []string{exe}
}

func adbBundleFailure() string { return adbBundleError }

// platformStartupWarnings surfaces Win7 prerequisites that would otherwise
// look like "ADB is broken" with no explanation.
func platformStartupWarnings() []string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	// The bundled platform-tools 34.0.4 adb.exe is a 32-bit PE, so on Win7 x64
	// it needs the 32-bit UCRT from SysWOW64. Also accept System32 for users
	// who point --adb at a 64-bit build to avoid a false warning.
	found := false
	for _, dir := range []string{"SysWOW64", "System32"} {
		if _, err := os.Stat(filepath.Join(root, dir, "ucrtbase.dll")); err == nil {
			found = true
			break
		}
	}
	if !found {
		return []string{
			"未检测到 ucrtbase.dll（通用 C 运行库）。Windows 7 SP1 需先安装 32 位与 64 位 " +
				"VC++ 2015-2019 运行库（或 KB2999226），否则内置的 32 位 adb.exe 无法启动；程序界面仍可使用。",
		}
	}
	return nil
}

func adbNotFoundHelp() string {
	msg := "未能找到可用的 adb 组件。\n\n" +
		"本程序已内置 Android SDK Platform Tools，正常情况下会自动释放，无需另外安装。\n" +
		"若仍然失败，请任选一种方式：\n" +
		"  1. 重新运行完整安装包（会安装 adb.exe 与两个 AdbWin*.dll）；\n" +
		"  2. 把 adb.exe、AdbWinApi.dll、AdbWinUsbApi.dll 放到本程序同一目录；\n" +
		"  3. 确认当前用户目录可写，且安全软件没有拦截程序释放文件。\n\n" +
		"另请确认 Windows 7 SP1 已安装 KB2999226 / VC++ 2015-2019 运行库。"
	if e := adbBundleFailure(); e != "" {
		msg += "\n\n内置组件释放失败原因：" + e
	}
	return msg
}
