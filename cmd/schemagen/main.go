package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type outputTarget struct {
	relPath string
	content []byte
}

func loadYAML[T any](path string) (T, error) {
	var out T
	data, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

func normalizeCRLF(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

func run(root string, checkOnly bool) error {
	enums, err := loadYAML[EnumsSchema](filepath.Join(root, "schema", "enums.yaml"))
	if err != nil {
		return err
	}
	telemetry, err := loadYAML[TelemetrySchema](filepath.Join(root, "schema", "telemetry.yaml"))
	if err != nil {
		return err
	}
	dtos, err := loadYAML[DTOSchema](filepath.Join(root, "schema", "dto.yaml"))
	if err != nil {
		return err
	}

	goEnums, err := renderGoEnums(enums)
	if err != nil {
		return fmt.Errorf("render go enums: %w", err)
	}
	goRanks, err := renderGoRanks(enums)
	if err != nil {
		return fmt.Errorf("render go ranks: %w", err)
	}
	goDTOs, err := renderGoDTOs(dtos)
	if err != nil {
		return fmt.Errorf("render go dtos: %w", err)
	}

	targets := []outputTarget{
		{filepath.Join("internal", "contracts", "generated_enums.go"), goEnums},
		{filepath.Join("internal", "contracts", "generated_ranks.go"), goRanks},
		{filepath.Join("internal", "contracts", "generated_dto.go"), goDTOs},
		{filepath.Join("web", "src", "contracts", "generated_enums.ts"), renderTSEnums(enums)},
		{filepath.Join("web", "src", "contracts", "generated_columns.ts"), renderTSColumns(telemetry)},
		{filepath.Join("web", "src", "contracts", "generated_dto.ts"), renderTSDTOs(dtos)},
	}

	for _, t := range targets {
		full := filepath.Join(root, t.relPath)
		want := normalizeCRLF(t.content)
		if checkOnly {
			got, err := os.ReadFile(full)
			if err != nil {
				return fmt.Errorf("schemagen check: missing %s: %w", t.relPath, err)
			}
			if !bytes.Equal(normalizeCRLF(got), want) {
				return fmt.Errorf("schemagen check: %s is out of date; run `go run ./cmd/schemagen`", t.relPath)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, want, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", t.relPath, err)
		}
	}
	return nil
}

func main() {
	check := flag.Bool("check", false, "verify generated artifacts match schema without modifying disk")
	root := flag.String("root", ".", "repository root path")
	flag.Parse()

	if err := run(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
