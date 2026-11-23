//go:build !windows

package main

import (
	"path/filepath"
	"testing"
)

func TestExtraAdbCandidatesUseInstalledLayout(t *testing.T) {
	got := extraAdbCandidates("/usr/bin")
	want := "/usr/lib/pcwebadb/adb"
	found := false
	for _, p := range got {
		if p == want {
			found = true
		}
		if filepath.Base(filepath.Dir(p)) == "skyadb-pc" {
			t.Fatalf("stale pre-rename candidate: %s", p)
		}
	}
	if !found {
		t.Fatalf("deb-installed adb path %s missing from candidates: %v", want, got)
	}
}
