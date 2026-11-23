//go:build windows

package main

// Native Win32 launcher window for the GUI-subsystem build.
//
// The Go binary is linked with -H windowsgui, so there is no console to show
// startup information or to receive Ctrl+C. This file creates a small Win32
// window with the service URL, the adb path, a status line and three buttons,
// using only user32 / kernel32 / shell32 / gdi32 APIs that ship with Win7.
// No cgo, no manifest, no third-party module, no Win10-only API set.
//
// Closing the window hides it and leaves the service running in the
// notification area (see tray_windows.go). Only the tray's "退出" item, the
// "退出" button or a system shutdown actually stops the process.

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modGdi32    = syscall.NewLazyDLL("gdi32.dll")
	modNtdll    = syscall.NewLazyDLL("ntdll.dll")

	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
	procDestroyWindow    = modUser32.NewProc("DestroyWindow")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procUpdateWindow     = modUser32.NewProc("UpdateWindow")
	procSendMessageW     = modUser32.NewProc("SendMessageW")
	procLoadIconW        = modUser32.NewProc("LoadIconW")
	procLoadCursorW      = modUser32.NewProc("LoadCursorW")
	procMessageBoxW      = modUser32.NewProc("MessageBoxW")
	procGetSystemMetrics = modUser32.NewProc("GetSystemMetrics")
	procOpenClipboard    = modUser32.NewProc("OpenClipboard")
	procEmptyClipboard   = modUser32.NewProc("EmptyClipboard")
	procSetClipboardData = modUser32.NewProc("SetClipboardData")
	procCloseClipboard   = modUser32.NewProc("CloseClipboard")

	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc      = modKernel32.NewProc("GlobalAlloc")
	procGlobalLock       = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock     = modKernel32.NewProc("GlobalUnlock")
	procGlobalFree       = modKernel32.NewProc("GlobalFree")

	procShellExecuteW  = modShell32.NewProc("ShellExecuteW")
	procGetStockObject = modGdi32.NewProc("GetStockObject")
	// ntdll.dll is always present on Win7; RtlMoveMemory is a stable export.
	procRtlMoveMemory = modNtdll.NewProc("RtlMoveMemory")
)

// Hand-written Win32 structures. Sizes are verified by the compile-time
// assertions in abi_windows_amd64.go (this product is x64 only).
type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  uintptr
	lpszClassName uintptr
	hIconSm       uintptr
}

type pointW struct {
	x int32
	y int32
}

// msgW stores the UINT message field in a pointer-sized slot, which on amd64
// fills the natural alignment padding between hwnd and wParam.
type msgW struct {
	hwnd     uintptr
	message  uintptr
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       pointW
	lPrivate uint32
}

const (
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsBorder      = 0x00800000
	wsTabStop     = 0x00010000
	esReadOnly    = 0x00000800
	esAutoHScroll = 0x00000080
	csHRedraw     = 0x00000002
	csVRedraw     = 0x00000001

	wmDestroy         = 0x0002
	wmSetText         = 0x000C
	wmClose           = 0x0010
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wmCommand         = 0x0111
	wmSetFont         = 0x0030

	swShow         = 5
	idiApplication = 32532
	idcArrow       = 32512
	colorBtnFace   = 15
	defaultGuiFont = 17
	gmMoveable     = 0x0002
	cfUnicodeText  = 13
	mbIconError    = 0x00000010
	mbIconInfo     = 0x00000040
	mbOk           = 0
	winStyleMain   = 0x00CA0000 // CAPTION | SYSMENU | MINIMIZEBOX, fixed size
)

const (
	idOpen   = 1001
	idCopy   = 1002
	idExit   = 1003
	idStatus = 1004
)

var (
	guiURL    string
	guiHwnd   uintptr
	guiStatus uintptr
	// UTF-16 buffers stay reachable for the whole process lifetime so no GC can
	// reclaim them while Win32 holds pointers to the strings.
	keepUTF16 []*uint16
)

func u16(s string) uintptr {
	p, _ := syscall.UTF16PtrFromString(s)
	keepUTF16 = append(keepUTF16, p)
	return uintptr(unsafe.Pointer(p))
}

func messageBox(title, text string, flags uintptr) {
	t, _ := syscall.UTF16PtrFromString(title)
	x, _ := syscall.UTF16PtrFromString(text)
	// MessageBoxW(hWnd, lpText, lpCaption, uType)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(x)), uintptr(unsafe.Pointer(t)), flags)
	runtime.KeepAlive(t)
	runtime.KeepAlive(x)
}

func setStatus(text string) {
	if guiStatus == 0 {
		return
	}
	p, _ := syscall.UTF16PtrFromString(text)
	procSendMessageW.Call(guiStatus, wmSetText, 0, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}

func setClipboardText(s string) bool {
	buf, err := syscall.UTF16FromString(s) // includes the terminating NUL
	if err != nil {
		return false
	}
	h, _, _ := procGlobalAlloc.Call(gmMoveable, uintptr(len(buf)*2))
	if h == 0 {
		return false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return false
	}
	// Copy the UTF-16 buffer into the locked global memory via the OS memmove
	// export. Passing both addresses as uintptr avoids a uintptr->unsafe.Pointer
	// conversion (which go vet's unsafeptr check correctly rejects).
	src := uintptr(unsafe.Pointer(&buf[0]))
	procRtlMoveMemory.Call(p, src, uintptr(len(buf)*2))
	runtime.KeepAlive(buf)
	procGlobalUnlock.Call(h)

	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		procGlobalFree.Call(h)
		return false
	}
	procEmptyClipboard.Call(0)
	r, _, _ := procSetClipboardData.Call(cfUnicodeText, h)
	procCloseClipboard.Call(0)
	if r == 0 {
		procGlobalFree.Call(h)
		return false
	}
	// The clipboard now owns h; it must not be freed here.
	return true
}

// wndProc is installed through syscall.NewCallback. A panic inside a Win32
// callback would terminate the process, hence the recover guard.
func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	defer func() { _ = recover() }()
	// Explorer restarted and rebuilt the notification area: re-register the
	// icon, otherwise the hidden window has no way back.
	if taskbarCreated != 0 && msg == uintptr(taskbarCreated) {
		trayReAdd()
		return 0
	}
	switch msg {
	case wmCommand:
		switch uint32(wparam) & 0xFFFF {
		case idOpen:
			setStatus(openBrowser(guiURL))
		case idCopy:
			if setClipboardText(guiURL) {
				setStatus("地址已复制到剪贴板，可粘贴到浏览器中。")
			} else {
				setStatus("复制失败，请手动选中上方地址框后按 Ctrl+C。")
			}
		case idExit:
			quitApp()
		}
		return 0
	case wmTrayIcon:
		switch uint32(lparam) & 0xFFFF {
		case wmLButtonUp, wmLButtonDblClk:
			showMainWindow()
		case wmRButtonUp:
			showTrayMenu()
		}
		return 0
	case wmAppRaise:
		// Sent by a second launch, or by GET /api/raise from the Web UI.
		showMainWindow()
		return 0
	case wmClose:
		hideToTray()
		return 0
	case wmQueryEndSession:
		// Only vote "yes" here. The session can still be cancelled by another
		// application; cleaning up now would kill the service needlessly.
		return 1
	case wmEndSession:
		// wParam != 0 means the session is really ending (logoff/shutdown).
		if wparam != 0 {
			quitApp()
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func createWin(class, title uintptr, style uintptr, x, y, w, h int32, parent, menu, inst uintptr) uintptr {
	r, _, _ := procCreateWindowExW.Call(
		0, class, title, style,
		uintptr(uint32(x)), uintptr(uint32(y)),
		uintptr(uint32(w)), uintptr(uint32(h)),
		parent, menu, inst, 0,
	)
	return r
}

func runGUI(url, bin string, shutdown func()) {
	// Win32 windows must be created, messaged and destroyed on ONE thread.
	// Without LockOSThread the Go scheduler may resume this goroutine on a
	// different OS thread (e.g. after trayAdd's retry sleep or any blocking
	// call); GetMessageW would then pump a queue that no longer owns the
	// window and the UI freezes with no way to close it. The GTK shell locks
	// its thread too - this was simply missing here.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer shutdown()
	defer trayShutdown()
	guiURL = url

	// RegisterWindowMessage("TaskbarCreated"): Explorer broadcasts it whenever
	// the shell restarts. We must re-add the tray icon or a hidden window
	// becomes unreachable until the process is killed.
	taskbarCreated = registerWindowMessage("TaskbarCreated")

	inst, _, _ := procGetModuleHandleW.Call(0)
	// The embedded ICO becomes both the window icon and the tray icon. Fall
	// back to the stock application icon if the resource cannot be decoded.
	trayIcon = loadTrayIcon()
	hIcon := trayIcon
	if hIcon == 0 {
		hIcon, _, _ = procLoadIconW.Call(0, idiApplication)
	}
	hCursor, _, _ := procLoadCursorW.Call(0, idcArrow)

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     inst,
		hIcon:         hIcon,
		hCursor:       hCursor,
		hbrBackground: uintptr(colorBtnFace + 1),
		lpszClassName: u16("PCWebADBMainWindow"),
		hIconSm:       hIcon,
	}
	r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	runtime.KeepAlive(wc)
	if r == 0 {
		messageBox(appName, "无法注册窗口类，程序将退出。", mbIconError)
		return
	}

	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	const winW, winH = int32(580), int32(360)
	x, y := (int32(sw)-winW)/2, (int32(sh)-winH)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	guiHwnd = createWin(u16("PCWebADBMainWindow"), u16(appName+" "+version+" · ADB 设备管理器"),
		winStyleMain, x, y, winW, winH, 0, 0, inst)
	if guiHwnd == 0 {
		messageBox(appName, "无法创建主窗口，程序将退出。", mbIconError)
		return
	}
	// Capture the handle locally: /api/raise runs on an HTTP goroutine, so it
	// must not race with the GUI thread's globals. PostMessage is thread-safe.
	hwnd := guiHwnd
	setRaiseHandler(func() {
		if hwnd != 0 {
			procPostMessageW.Call(hwnd, wmAppRaise, 0, 0)
		}
	})

	font, _, _ := procGetStockObject.Call(defaultGuiFont)
	add := func(kind, text string, style uintptr, cx, cy, cw, ch int32, id uintptr) uintptr {
		h := createWin(u16(kind), u16(text), wsChild|wsVisible|style, cx, cy, cw, ch, guiHwnd, id, inst)
		if h != 0 && font != 0 {
			procSendMessageW.Call(h, wmSetFont, font, 1)
		}
		return h
	}

	add("STATIC", appName+" "+version+" — PC 端 ADB 设备管理器（功能参考 sky22333/skyadb）", 0, 20, 16, 540, 24, 0)
	add("STATIC", "管理界面地址（浏览器打开）：", 0, 20, 52, 540, 20, 0)
	add("EDIT", url, wsBorder|esReadOnly|esAutoHScroll|wsTabStop, 20, 74, 540, 26, 0)
	add("STATIC", "ADB 程序：", 0, 20, 110, 540, 20, 0)
	add("EDIT", bin, wsBorder|esReadOnly|esAutoHScroll|wsTabStop, 20, 132, 540, 26, 0)
	guiStatus = add("STATIC", "服务已启动。点击「打开界面」进入管理界面。", 0, 20, 170, 540, 68, idStatus)
	add("BUTTON", "打开界面(&O)", wsTabStop, 20, 248, 165, 34, idOpen)
	add("BUTTON", "复制地址(&C)", wsTabStop, 200, 248, 165, 34, idCopy)
	add("BUTTON", "退出(&X)", wsTabStop, 395, 248, 165, 34, idExit)
	add("STATIC", "关闭窗口 → 最小化到通知区域，服务继续运行；右键托盘图标可退出。", 0, 20, 298, 540, 20, 0)

	if !trayAdd(guiHwnd, hIcon) {
		messageBox(appName, "无法创建任务栏通知区域图标。\n\n"+
			"本次运行将保持旧行为：关闭窗口即停止服务。\n"+
			"请检查 Explorer / 通知区域设置后重新启动。", mbIconInfo)
	}

	procShowWindow.Call(guiHwnd, swShow)
	procUpdateWindow.Call(guiHwnd)

	if !*flagNoBrowser {
		setStatus(openBrowser(url))
	} else {
		setStatus("已按 --no-browser 启动，服务地址见上方地址框。")
	}

	var m msgW
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 || int32(uint32(ret)) == -1 { // WM_QUIT or error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	runtime.KeepAlive(&m)
	runtime.KeepAlive(keepUTF16)
}
