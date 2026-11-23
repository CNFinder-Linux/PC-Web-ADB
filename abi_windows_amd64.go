//go:build windows && amd64

package main

import "unsafe"

// Compile-time ABI assertions. If the hand-written Win32 structures drift from
// the real Windows layouts, the array length underflows and the build fails.
// Reference sizes (amd64): WNDCLASSEXW 80, MSG 48, POINT 8, RECT 16,
// NOTIFYICONDATAW_V2_SIZE 952 (measured against mingw-w64 shellapi.h).
var (
	_ [80 - unsafe.Sizeof(wndClassExW{})]byte
	_ [unsafe.Sizeof(wndClassExW{}) - 80]byte
	_ [48 - unsafe.Sizeof(msgW{})]byte
	_ [unsafe.Sizeof(msgW{}) - 48]byte
	_ [8 - unsafe.Sizeof(pointW{})]byte
	_ [unsafe.Sizeof(pointW{}) - 8]byte
	_ [952 - unsafe.Sizeof(notifyIconDataW{})]byte
	_ [unsafe.Sizeof(notifyIconDataW{}) - 952]byte
)
