package main

import (
	"path/filepath"
	"testing"
	"time"
)

// TestAddRecentDoesNotDeadlock guards against the regression that made the
// whole app unresponsive: AddRecent held s.mu and then called s.Recent(), which
// locks the same non-reentrant mutex. Every /api/state call afterwards blocked
// forever, so the UI hung the first time a device was connected.
func TestAddRecentDoesNotDeadlock(t *testing.T) {
	st := &Store{path: filepath.Join(t.TempDir(), "config.json")}
	st.data.Settings = Settings{DefaultPort: 5555}

	done := make(chan []RecentDevice, 1)
	go func() {
		done <- st.AddRecent(RecentDevice{Host: "192.168.2.192", Port: 5555, Serial: "192.168.2.192:5555"})
	}()

	select {
	case got := <-done:
		if len(got) != 1 || got[0].Host != "192.168.2.192" {
			t.Fatalf("unexpected recent list: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("AddRecent deadlocked")
	}

	// The store must stay usable afterwards (this is what /api/state does).
	if got := st.Recent(); len(got) != 1 {
		t.Fatalf("Recent() returned %d entries, want 1", len(got))
	}
	if got := st.Get(); got.DefaultPort != 5555 {
		t.Fatalf("Get() returned %+v", got)
	}
}

// TestAddRecentDeduplicates keeps the list bounded and moves a known device to
// the front instead of appending duplicates.
func TestNormalizeSettingsClampsLoadedValues(t *testing.T) {
	v := normalizeSettings(Settings{
		DefaultPort:      -1,
		ConnectTimeout:   1,
		CommandTimeout:   1000000,
		ScanConcurrency:  9999,
		MirrorQuality:    "",
		Theme:            "",
		DefaultFilesPath: "",
	})
	if v.DefaultPort != 5555 || v.ConnectTimeout != 400 || v.CommandTimeout != 20 ||
		v.ScanConcurrency != 64 || v.MirrorQuality != "balanced" ||
		v.Theme != "system" || v.DefaultFilesPath != "/sdcard" {
		t.Fatalf("normalizeSettings did not clamp out-of-range values: %+v", v)
	}
}

func TestAddRecentDeduplicates(t *testing.T) {
	st := &Store{path: filepath.Join(t.TempDir(), "config.json")}
	for i := 0; i < 30; i++ {
		st.AddRecent(RecentDevice{Host: "10.0.0.1", Port: 5555})
		st.AddRecent(RecentDevice{Host: "10.0.0.2", Port: 5555})
	}
	got := st.Recent()
	if len(got) != 2 {
		t.Fatalf("want 2 unique entries, got %d: %+v", len(got), got)
	}
	if got[0].Host != "10.0.0.2" {
		t.Fatalf("most recent entry should be first, got %+v", got)
	}
}
