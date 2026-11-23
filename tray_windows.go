//go:build windows

package main

// Windows notification-area (system tray) support.
//
// Shell_NotifyIconW + a small popup menu are enough to keep the service alive
// after the launcher window is closed. Everything used here ships with Win7:
// user32 / shell32 / kernel32 only, no COM, no manifest, no cgo.
//
// NOTIFYICONDATAW is declared with the V2 layout (no guidItem, no
// hBalloonIcon). cbSize = sizeof(the struct) then evaluates to the canonical
// NOTIFYICONDATAW_V2_SIZE: 952 on amd64. This was measured against mingw-w64's
// shellapi.h and is asserted at compile time in abi_windows_amd64.go.

import (
	_ "embed"
	"encoding/binary"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

//go:embed packaging/windows/pcwebadb.ico
var trayIconICO []byte

var (
	procShellNotifyIconW         = modShell32.NewProc("Shell_NotifyIconW")
	procCreatePopupMenu          = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW              = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu           = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu              = modUser32.NewProc("DestroyMenu")
	procGetCursorPos             = modUser32.NewProc("GetCursorPos")
	procSetForegroundWindow      = modUser32.NewProc("SetForegroundWindow")
	procPostMessageW             = modUser32.NewProc("PostMessageW")
	procDestroyIcon              = modUser32.NewProc("DestroyIcon")
	procCreateIconFromResourceEx = modUser32.NewProc("CreateIconFromResourceEx")
	procFindWindowW              = modUser32.NewProc("FindWindowW")
	procRegisterWindowMessageW   = modUser32.NewProc("RegisterWindowMessageW")
	procCreateMutexW             = modKernel32.NewProc("CreateMutexW")
	procCloseHandle              = modKernel32.NewProc("CloseHandle")
)

const (
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010
	niifInfo   = 0x00000001

	notifyIconVersion = 3 // NOTIFYICON_VERSION: stable Win7 behaviour

	// WM_APP-based messages are private to this application and never collide
	// with system messages. lParam of wmTrayIcon carries the mouse message.
	wmTrayIcon      = 0x8001
	wmAppRaise      = 0x8002
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmNull          = 0x0000

	swHide    = 0
	swRestore = 9

	mfString       = 0x0000
	mfSeparator    = 0x0800
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	idTrayOpenUI = 2101
	idTrayShow   = 2102
	idTrayQuit   = 2103

	errorAlreadyExists = 183 // ERROR_ALREADY_EXISTS
)

// notifyIconDataW matches NOTIFYICONDATAW up to dwInfoFlags. Go's natural
// struct alignment inserts exactly the padding the Microsoft compiler does,
// including the 8-byte alignment of hWnd / hIcon on amd64.
type notifyIconDataW struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32 // shares a union with uTimeout
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
}

var (
	trayIcon      uintptr
	trayWnd       uintptr
	trayActive    bool
	trayOK        bool
	balloonShown  bool
	instanceMutex uintptr
	// taskbarCreated holds RegisterWindowMessage("TaskbarCreated"); Explorer
	// broadcasts it after a shell restart so tray owners can re-add icons.
	taskbarCreated uint32
)

func registerWindowMessage(name string) uint32 {
	p, _ := syscall.UTF16PtrFromString(name)
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
	return uint32(r)
}

func fillUTF16(dst []uint16, s string) {
	for i := range dst {
		dst[i] = 0
	}
	v, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	if len(v) >= len(dst) {
		v = v[:len(dst)-1]
	}
	copy(dst, v)
}

func newNotifyIconData(hwnd uintptr) notifyIconDataW {
	n := notifyIconDataW{
		cbSize:           uint32(unsafe.Sizeof(notifyIconDataW{})),
		hWnd:             hwnd,
		uID:              1,
		uCallbackMessage: wmTrayIcon,
	}
	return n
}

func shellNotify(msg uintptr, n *notifyIconDataW) bool {
	r, _, _ := procShellNotifyIconW.Call(msg, uintptr(unsafe.Pointer(n)))
	runtime.KeepAlive(n)
	return r != 0
}

// loadTrayIcon converts the embedded ICO's largest RT_ICON image into an HICON.
// CreateIconFromResourceEx is available on Win7 and avoids shipping a sidecar
// file, keeping the "one exe, zero dependencies" promise.
func loadTrayIcon() uintptr {
	d := trayIconICO
	if len(d) < 22 {
		return 0
	}
	count := int(binary.LittleEndian.Uint16(d[4:6]))
	bestOff, bestSize, bestDim := 0, 0, 0
	for i := 0; i < count; i++ {
		off := 6 + i*16
		if off+16 > len(d) {
			break
		}
		dim := int(d[off])
		if dim == 0 {
			dim = 256
		}
		size := int(binary.LittleEndian.Uint32(d[off+8 : off+12]))
		start := int(binary.LittleEndian.Uint32(d[off+12 : off+16]))
		if size <= 0 || start < 0 || start+size > len(d) || dim < bestDim {
			continue
		}
		bestOff, bestSize, bestDim = start, size, dim
	}
	if bestSize == 0 {
		return 0
	}
	h, _, _ := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&d[bestOff])), uintptr(bestSize),
		1, 0x00030000, uintptr(bestDim), uintptr(bestDim), 0,
	)
	runtime.KeepAlive(d)
	return h
}

func trayAdd(hwnd, icon uintptr) bool {
	n := newNotifyIconData(hwnd)
	n.uFlags = nifMessage | nifIcon | nifTip
	n.hIcon = icon
	fillUTF16(n.szTip[:], appName+" · ADB 设备管理（后台运行中）")
	// Explorer may not have created its tray window yet during logon.
	for i := 0; i < 4; i++ {
		if shellNotify(nimAdd, &n) {
			trayWnd, trayActive, trayOK = hwnd, true, true
			// Pin the notification-icon behaviour instead of relying on the
			// uVersion/uTimeout union default, which differed across shells.
			vn := newNotifyIconData(hwnd)
			vn.uVersion = notifyIconVersion
			shellNotify(nimSetVersion, &vn)
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	trayOK = false
	return false
}

// trayReAdd rebuilds the notification-area icon after an Explorer restart.
// The old icon is deleted first: after a shell restart the original NIM_ADD
// registration is gone, so a second plain NIM_ADD for the same (hWnd, uID)
// would be treated as an update to a phantom entry.
func trayReAdd() {
	// TaskbarCreated must also recover from an initial add failure (slow
	// Explorer during logon), so trayOK must not gate the retry.
	if trayWnd == 0 {
		return
	}
	if trayActive {
		n := newNotifyIconData(trayWnd)
		shellNotify(nimDelete, &n)
		trayActive = false
	}
	icon := trayIcon
	if icon == 0 {
		icon = loadTrayIcon()
		trayIcon = icon
	}
	trayAdd(trayWnd, icon)
}

// trayBalloon explains the new close behaviour exactly once, on first hide.
func trayBalloon(title, text string) {
	if !trayActive || balloonShown {
		return
	}
	balloonShown = true
	n := newNotifyIconData(trayWnd)
	n.uFlags = nifInfo
	n.dwInfoFlags = niifInfo
	fillUTF16(n.szInfoTitle[:], title)
	fillUTF16(n.szInfo[:], text)
	shellNotify(nimModify, &n)
}

func trayShutdown() {
	if trayActive {
		n := newNotifyIconData(trayWnd)
		shellNotify(nimDelete, &n)
		trayActive = false
	}
	if trayIcon != 0 {
		procDestroyIcon.Call(trayIcon)
		trayIcon = 0
	}
}

// showTrayMenu runs synchronously on the GUI thread. TPM_RETURNCMD makes
// TrackPopupMenu return the chosen id instead of posting WM_COMMAND.
func showTrayMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	// Menu labels are copied into the menu by AppendMenuW; they do not need to
	// live in the process-wide keepUTF16 list (that list is for strings Win32
	// holds pointers to for the whole process lifetime).
	add := func(id uintptr, label string) {
		p, _ := syscall.UTF16PtrFromString(label)
		procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(p)))
		runtime.KeepAlive(p)
	}
	add(idTrayOpenUI, "打开管理界面(&O)")
	add(idTrayShow, "显示控制窗口(&S)")
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	add(idTrayQuit, "退出 "+appName+"(&X)")

	var pt pointW
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(guiHwnd)
	cmd, _, _ := procTrackPopupMenu.Call(
		menu, tpmRightButton|tpmReturnCmd,
		uintptr(uint32(pt.x)), uintptr(uint32(pt.y)),
		0, guiHwnd, 0,
	)
	// MSDN: a WM_NULL after the menu makes it dismiss correctly on click-away.
	procPostMessageW.Call(guiHwnd, wmNull, 0, 0)
	procDestroyMenu.Call(menu)
	runtime.KeepAlive(&pt)

	switch uint32(cmd) {
	case idTrayOpenUI:
		setStatus(openBrowser(guiURL))
	case idTrayShow:
		showMainWindow()
	case idTrayQuit:
		quitApp()
	}
}

func showMainWindow() {
	if guiHwnd == 0 {
		return
	}
	procShowWindow.Call(guiHwnd, swRestore)
	procSetForegroundWindow.Call(guiHwnd)
}

// hideToTray is the WM_CLOSE / close-button behaviour. The HTTP service and
// the already-open browser page keep running; the tray icon is the only way in.
func hideToTray() {
	if !trayOK {
		// Without a tray icon the user would have no way to restore or quit, so
		// fall back to the old behaviour instead of leaving a stranded process.
		quitApp()
		return
	}
	procShowWindow.Call(guiHwnd, swHide)
	setStatus("已最小化到任务栏通知区域，服务继续运行。右键托盘图标可退出。")
	trayBalloon(appName+" 已切换到后台",
		"服务继续运行，管理界面可继续使用。右键托盘图标可显示窗口或退出程序。")
}

func quitApp() {
	if guiHwnd != 0 {
		procDestroyWindow.Call(guiHwnd)
	}
}

// claimSingleInstance prevents a second launcher from starting a second adb
// service. It also raises the running window, which is the safety net for
// desktop environments where the tray icon is hidden by policy.
//
// GetLastError is only meaningful for ERROR_ALREADY_EXISTS (the call itself
// does not clear it on success), so a stale errno must never strand the
// launch: we only defer to the other instance when its window is actually
// there. Otherwise this process keeps the mutex and starts normally.
func claimSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString(`Local\PCWebADB_SingleInstance`)
	h, _, callErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	if h == 0 {
		return true // mutex unavailable: do not block startup
	}
	errno, _ := callErr.(syscall.Errno)
	if errno == errorAlreadyExists {
		cls, _ := syscall.UTF16PtrFromString("PCWebADBMainWindow")
		// The first instance creates this mutex long before its window exists.
		// Wait briefly before concluding the mutex is stale, otherwise two fast
		// launches can both start a service and a tray icon.
		var w uintptr
		for i := 0; i < 10; i++ {
			w, _, _ = procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
			if w != 0 {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		runtime.KeepAlive(cls)
		if w != 0 {
			procPostMessageW.Call(w, wmAppRaise, 0, 0)
			procCloseHandle.Call(h)
			return false
		}
		// Mutex left behind by a killed process: adopt the handle and start.
		appLogf("info", "单实例互斥体已存在但没有找到运行中的窗口，按首次启动继续")
	}
	instanceMutex = h // keep the handle for the whole process lifetime
	return true
}

func publishInstance(_ string) {}
