package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestWithQuery(query string) *http.Request {
	return httptest.NewRequest("GET", "/api/logcat?"+query, nil)
}

func TestValidateMutablePathRejectsSystemSubdirectories(t *testing.T) {
	for _, p := range []string{"/system", "/system/bin", "/data/data", "/proc/self", "/dev/block", "/"} {
		if err := validateMutablePath(p); err == nil {
			t.Errorf("validateMutablePath(%q) should reject system paths", p)
		}
	}
	for _, p := range []string{"/sdcard", "/sdcard/Download", "/storage/emulated/0/Movies"} {
		if err := validateMutablePath(p); err != nil {
			t.Errorf("validateMutablePath(%q) should allow user paths: %v", p, err)
		}
	}
}

func TestLogcatParams(t *testing.T) {
	// Invalid/negative line counts fall back to a safe default.
	for _, lines := range []string{"", "abc", "-1", "0", "200001"} {
		r := requestWithQuery("lines=" + lines)
		if got := logcatLinesParam(r); got != "200" {
			t.Errorf("lines=%q => %q, want 200", lines, got)
		}
	}
	r := requestWithQuery("lines=500")
	if got := logcatLinesParam(r); got != "500" {
		t.Fatalf("valid line count = %q, want 500", got)
	}
	if f, err := logcatFilterParam(requestWithQuery("filter=a%3Bb")); err == nil || f != "" {
		t.Fatalf("illegal filter should be rejected, got %q err=%v", f, err)
	}
	if f, err := logcatFilterParam(requestWithQuery("filter=ActivityManager:I")); err != nil || f != "ActivityManager:I" {
		t.Fatalf("valid filter = %q err=%v", f, err)
	}
}
