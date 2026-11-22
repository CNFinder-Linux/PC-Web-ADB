package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestServeHTTPRejectsCrossOriginStateChange(t *testing.T) {
	srv := NewServer(nil, nil, nil, http.Dir("."))
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:1234/api/shell", nil)
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403", w.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:1234/", nil)
	r.Host = "[::1]:1234"
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code == http.StatusForbidden {
		t.Fatal("bracketed IPv6 loopback host should be accepted")
	}
}

func TestMethodNotAllowedReturns405(t *testing.T) {
	srv := NewServer(nil, nil, nil, http.Dir("."))
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:1234/api/state", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/state status = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow header = %q, want GET", got)
	}
}

func TestDownloadAPKServerErrorIncludesDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "<html><body>400 bad request: download token expired</body></html>")
	}))
	defer srv.Close()

	_, _, err := downloadAPK(srv.URL + "/app.apk")
	if err == nil {
		t.Fatal("expected error for HTTP 400 response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 400") || !strings.Contains(msg, "token expired") {
		t.Fatalf("error should explain the 400 response, got: %s", msg)
	}
	if strings.Contains(msg, "<html>") {
		t.Fatalf("HTML tags should be stripped, got: %s", msg)
	}
}

func TestDownloadAPKRejectsHTMLPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>登录</title><p>please login first</p>")
	}))
	defer srv.Close()

	local, _, err := downloadAPK(srv.URL + "/page")
	if err == nil {
		os.Remove(local)
		t.Fatal("expected error for an HTML landing page")
	}
	if !strings.Contains(err.Error(), "不是 APK") || !strings.Contains(err.Error(), "please login") {
		t.Fatalf("error should explain that the link is not a direct APK, got: %s", err)
	}
}

func TestDownloadAPKFollowsRedirectWithCookie(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "dl", Value: "ok", Path: "/"})
		http.Redirect(w, r, "/MiTV.apk", http.StatusFound)
	})
	mux.HandleFunc("/MiTV.apk", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("dl")
		if err != nil || c.Value != "ok" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="MiTV-release.apk"`)
		_, _ = w.Write([]byte("PK\x03\x04fake-apk-payload"))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	local, name, err := downloadAPK(srv.URL + "/start")
	if err != nil {
		t.Fatalf("redirect with cookie should succeed: %v", err)
	}
	defer os.Remove(local)
	if name != "MiTV-release.apk" {
		t.Fatalf("filename from Content-Disposition = %q, want MiTV-release.apk", name)
	}
	b, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("downloaded file should keep APK bytes, got %q", b[:minInt(len(b), 8)])
	}
}

func TestDownloadAPKRejectsNonHTTPScheme(t *testing.T) {
	_, _, err := downloadAPK("ftp://example.com/app.apk")
	if err == nil || !strings.Contains(err.Error(), "http/https") {
		t.Fatalf("expected http/https scheme error, got: %v", err)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
