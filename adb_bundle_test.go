package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func bundleFixture() map[string][]byte {
	return map[string][]byte{
		"adb.exe":          []byte("ADB-EXECUTABLE-34.0.4"),
		"AdbWinApi.dll":    []byte("ADBWINAPI"),
		"AdbWinUsbApi.dll": []byte("ADBWINUSBAPI"),
	}
}

func TestWriteBundleCreatesAllFiles(t *testing.T) {
	// A path with a space exercises the same layout as "%LOCALAPPDATA%\SkyADB PC".
	dir := filepath.Join(t.TempDir(), "PC Web ADB", "adb", "34.0.4")
	files := bundleFixture()

	exe, err := writeBundle(dir, files)
	if err != nil {
		t.Fatalf("writeBundle: %v", err)
	}
	if want := filepath.Join(dir, "adb.exe"); exe != want {
		t.Fatalf("adb path = %q, want %q", exe, want)
	}
	for name, data := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("%s content mismatch", name)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != len(files) {
		t.Fatalf("unexpected extra files in bundle dir: %v", entries)
	}
}

func TestWriteBundleIsIdempotentButRepairsCorruption(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bundle")
	files := bundleFixture()
	if _, err := writeBundle(dir, files); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "adb.exe")

	// Same size, different bytes: a size-only check would miss this. It must be
	// detected and repaired.
	corrupt := bytes.Repeat([]byte("X"), len(files["adb.exe"]))
	if err := os.WriteFile(exe, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeBundle(dir, files); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if !bytes.Equal(got, files["adb.exe"]) {
		t.Fatal("corrupted adb.exe was not repaired")
	}

	// A second clean call must leave the file untouched.
	before, _ := os.Stat(exe)
	if _, err := writeBundle(dir, files); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(exe)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("clean second call rewrote adb.exe")
	}
}

func TestWriteBundleRecoversFromPartialExtraction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bundle")
	files := bundleFixture()
	if _, err := writeBundle(dir, files); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "AdbWinUsbApi.dll")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeBundle(dir, files); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("%s not restored after partial extraction", name)
		}
	}
}

func TestWriteBundleReportsUnwritableDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := writeBundle(filepath.Join(blocker, "bundle"), bundleFixture())
	if err == nil {
		t.Fatal("expected an error when the bundle directory cannot be created")
	}
}
