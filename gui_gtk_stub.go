//go:build !linux || !cgo

package main

// runNativeWindow is unavailable on non-Linux systems and on CGO-disabled
// Linux builds. Callers treat a non-zero return as "use the fallback".
func runNativeWindow(_ string) int { return 1 }

func requestNativeStop() {}

func requestNativeShow() {}
