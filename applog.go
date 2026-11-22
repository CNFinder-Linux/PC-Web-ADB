package main

// Persistent run log for the GUI builds.
//
// A GUI-subsystem process has no console, so a startup failure, a panic or a
// recoverable adb problem used to leave nothing behind - the user only saw
// "nothing happens". This file appends a small plain-text log next to the
// configuration (configDir/logs/pcwebadb.log) that both the platform shells
// and Diagnostics tee into. It is intentionally dependency-free and
// best-effort: log failures must never break startup.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	logMaxBytes = 2 << 20 // rotate at 2 MiB
	logKeep     = ".1"    // single previous generation
)

var (
	logMu       sync.Mutex
	logFile     *os.File
	logInited   bool
	logDisabled bool
)

func logPath() string {
	return filepath.Join(configDir(), "logs", "pcwebadb.log")
}

// initAppLog opens the run log. Called once from main before any subsystem
// starts; safe to call again (subsequent calls are no-ops).
//
// NOTE: appLogf takes logMu, so this function must NOT call it while holding
// the lock (a non-reentrant self-deadlock would otherwise freeze every cgo
// build at startup, where the deadlock detector cannot fire).
func initAppLog() {
	logMu.Lock()
	p := openLogFileLocked()
	logMu.Unlock()
	if p != "" {
		appLogf("info", "=== %s %s 启动，日志文件：%s", appName, version, p)
	}
}

// openLogFileLocked does the actual open; caller must hold logMu. Returns the
// log path on success so initAppLog can announce it without the lock held.
func openLogFileLocked() string {
	if logInited {
		return ""
	}
	logInited = true
	p := logPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		logDisabled = true
		return ""
	}
	if st, err := os.Stat(p); err == nil && st.Size() > logMaxBytes {
		_ = os.Remove(p + logKeep)
		_ = os.Rename(p, p+logKeep)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logDisabled = true
		return ""
	}
	logFile = f
	return p
}

// appLogf appends one timestamped line. Never panics, never blocks long.
func appLogf(level, format string, args ...interface{}) {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		return
	}
	line := fmt.Sprintf("%s [%s] %s\n",
		time.Now().Format("2006-01-02 15:04:05.000"), level, fmt.Sprintf(format, args...))
	_, _ = logFile.WriteString(line)
	if st, err := logFile.Stat(); err == nil && st.Size() > logMaxBytes {
		rotateLogLocked()
	}
}

// rotateLogLocked moves the current log to .1 and reopens a fresh file.
// The caller already holds logMu; never call appLogf from here.
func rotateLogLocked() {
	if logFile == nil {
		return
	}
	_ = logFile.Close()
	logFile = nil
	p := logPath()
	_ = os.Remove(p + logKeep)
	_ = os.Rename(p, p+logKeep)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logDisabled = true
		return
	}
	logFile = f
}

func closeAppLog() {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
}

// panicLog is installed as a deferred recover in main: a GUI process that dies
// from a panic shows nothing, so the stack trace goes to the log file and to a
// message box (Windows) / stderr (Unix) via platformFatalReport.
func panicLog() {
	if r := recover(); r != nil {
		appLogf("error", "程序异常退出: %v\n%s", r, stackTrace())
		platformFatalReport(fmt.Sprintf("程序内部错误，已写入日志后退出。\n\n%v\n\n日志：%s", r, logPath()))
	}
}

func stackTrace() string {
	buf := make([]byte, 16384)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}
