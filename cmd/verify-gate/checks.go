package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const maxSpecBytes = 46080

func shouldSkipDir(rel string) bool {
	base := filepath.Base(rel)
	switch base {
	case ".git", "node_modules", ".agents", "dist", "steam":
		return true
	}
	slashed := filepath.ToSlash(rel)
	return strings.HasPrefix(slashed, "internal/web/dist")
}

func checkSpecsSize(root string) error {
	specDir := filepath.Join(root, "docs", "specs")
	indexPath := filepath.Join(specDir, "MASTER_SPEC_INDEX.md")
	if _, err := os.Stat(indexPath); err != nil {
		return fmt.Errorf("missing docs/specs/MASTER_SPEC_INDEX.md: %w", err)
	}
	entries, err := os.ReadDir(specDir)
	if err != nil {
		return fmt.Errorf("read docs/specs: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.Size() >= maxSpecBytes {
			return fmt.Errorf("spec %s is %d bytes (limit < %d B)", e.Name(), info.Size(), maxSpecBytes)
		}
	}
	return nil
}

func checkFileLOC(root string, maxLOC int, warnOnly bool) error {
	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".ts" && ext != ".tsx" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := countLines(data)
		if lines >= maxLOC {
			violations = append(violations, fmt.Sprintf("%s (%d lines >= %d)", filepath.ToSlash(rel), lines, maxLOC))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		msg := fmt.Sprintf("SOLID-LOC-01 violations:\n  - %s", strings.Join(violations, "\n  - "))
		if warnOnly {
			fmt.Println("WARN:", msg)
			return nil
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := 0
	for _, ch := range b {
		if ch == '\n' {
			n++
		}
	}
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

func checkProdBundleNoPreview(root string) error {
	assetsDir := filepath.Join(root, "internal", "web", "dist", "assets")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		return fmt.Errorf("read dist/assets: %w", err)
	}
	jsCount := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		jsCount++
		raw, err := os.ReadFile(filepath.Join(assetsDir, e.Name()))
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "__RL_SYNC_DEV_PREVIEW_FIXTURE__") {
			return fmt.Errorf("production bundle %s still contains dev preview fixtures", e.Name())
		}
	}
	if jsCount == 0 {
		return fmt.Errorf("production build produced 0 JS bundles in %s", assetsDir)
	}
	return nil
}
