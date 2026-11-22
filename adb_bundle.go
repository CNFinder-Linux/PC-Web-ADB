package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// writeBundle materialises the bundled ADB platform-tools files into dir and
// returns the path of adb.exe.
//
// The Windows executables embed adb.exe plus the two AdbWin*.dll helpers (see
// adb_bundle_windows.go). A GUI program started from a shortcut has no
// guaranteed working directory, and a standalone copy of SkyADBPC.exe has no
// sibling files at all, so the first run extracts them into a per-user
// writable directory. Windows resolves DLL imports relative to the *adb.exe*
// file, therefore all three files must live side by side.
//
// Extraction is idempotent: existing files are verified byte-for-byte and left
// untouched. Each file is written to a temporary name and renamed into place so
// a crash or a second instance can never leave a half-written adb.exe.
func writeBundle(dir string, files map[string][]byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建 ADB 组件目录 %s：%w", dir, err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		data := files[name]
		dst := filepath.Join(dir, name)
		if st, err := os.Stat(dst); err == nil && !st.IsDir() {
			if st.Size() == int64(len(data)) {
				if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, data) {
					continue
				}
			}
		}
		tmp := fmt.Sprintf("%s.%d.tmp", dst, os.Getpid())
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("无法写入 %s：%w", dst, err)
		}
		// Remove first: Windows refuses to rename over a live target.
		_ = os.Remove(dst)
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("无法安装 %s：%w", dst, err)
		}
	}
	return filepath.Join(dir, "adb.exe"), nil
}
