package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSchemagenCheckAndDetectDrift(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	if err := run(root, false); err != nil {
		t.Fatalf("generate contracts: %v", err)
	}
	if err := run(root, true); err != nil {
		t.Fatalf("check contracts immediately after generation: %v", err)
	}

	tmp := t.TempDir()
	// Copy schema/ to temp root and test drift detection.
	for _, dir := range []string{"schema", filepath.Join("internal", "contracts"), filepath.Join("web", "src", "contracts")} {
		if err := os.MkdirAll(filepath.Join(tmp, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"enums.yaml", "telemetry.yaml", "dto.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, "schema", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, "schema", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run(tmp, false); err != nil {
		t.Fatal(err)
	}
	enumOut := filepath.Join(tmp, "internal", "contracts", "generated_enums.go")
	if err := os.WriteFile(enumOut, []byte("// hand-edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(tmp, true); err == nil {
		t.Fatal("expected check mode to fail on hand-edited contract file")
	}
}
