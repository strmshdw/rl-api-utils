package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckFileLOCAndPerfIsolation(t *testing.T) {
	tmp := t.TempDir()
	goodFile := filepath.Join(tmp, "internal", "domain", "ok.go")
	if err := os.MkdirAll(filepath.Dir(goodFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goodFile, []byte("package domain\nfunc f(b []byte) byte { return b[1] }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkSliceAndPerfIsolation(tmp); err != nil {
		t.Fatalf("unexpected slice violation: %v", err)
	}

	badFile := filepath.Join(tmp, "internal", "domain", "bad.go")
	if err := os.WriteFile(badFile, []byte("package domain\nfunc g(b []byte) []byte { return b[:20] }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkSliceAndPerfIsolation(tmp); err == nil {
		t.Fatal("expected SOLID-SLICE-01 violation for unannotated b[:20]")
	}
}

func TestRunPillar3_RequireRaceFailsWithoutGCC(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CI", "")
	usedRace, err := runPillar3(t.TempDir(), true)
	if err == nil {
		t.Fatal("expected runPillar3(requireRace=true) to fail when gcc is missing from PATH")
	}
	if usedRace {
		t.Fatal("expected usedRace=false when gcc is missing")
	}
}
