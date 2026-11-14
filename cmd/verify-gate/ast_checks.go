package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const modulePrefix = "github.com/dank/rl-api-utils/"

var goLayer = map[string]int{
	"internal/contracts": 0, "internal/version": 0,
	"internal/domain":    1,
	"internal/config":    2, "internal/replay": 2, "internal/storage": 2,
	"internal/auth":      3, "internal/psynet": 3, "internal/ballchasing": 3, "internal/statsapi": 3,
	"internal/syncer":    4, "internal/playertrack": 4, "internal/session": 4,
	"internal/web":       5, "internal/daemon": 5,
}

func checkGoDAG(root string) error {
	fset := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		srcPkg := filepath.ToSlash(rel)
		srcRank, knownSrc := goLayer[srcPkg]

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, modulePrefix) {
				continue
			}
			dstPkg := strings.TrimPrefix(p, modulePrefix)
			if dstPkg == "internal/testutil" && srcPkg != "internal/testutil" {
				violations = append(violations, fmt.Sprintf("%s imports testutil", srcPkg))
				continue
			}
			dstRank, knownDst := goLayer[dstPkg]
			if knownSrc && knownDst && dstRank >= srcRank && !(srcRank == 2 && dstRank == 2 && srcPkg != "internal/config" && dstPkg == "internal/config") {
				if !(srcPkg == "internal/psynet" && (dstPkg == "internal/replay" || dstPkg == "internal/storage")) &&
					!(srcPkg == "internal/auth" && (dstPkg == "internal/config" || dstPkg == "internal/storage")) &&
					!(srcPkg == "internal/statsapi" && dstPkg == "internal/storage") {
					if dstRank > srcRank || (dstRank == srcRank && srcPkg != dstPkg) {
						violations = append(violations, fmt.Sprintf("%s (L%d) -> %s (L%d)", srcPkg, srcRank, dstPkg, dstRank))
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("DAG layer violations:\n  - %s", strings.Join(violations, "\n  - "))
	}
	return nil
}

var tsImportRe = regexp.MustCompile(`(?:from|import)\s+['"](\.[^'"]+)['"]`)

func checkTSCycles(root string) error {
	srcRoot := filepath.Join(root, "web", "src")
	if _, err := os.Stat(srcRoot); err != nil {
		return nil
	}
	adj := make(map[string][]string)
	err := filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		if (!strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx")) || strings.Contains(path, ".test.") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		key := normTS(path)
		for _, m := range tsImportRe.FindAllStringSubmatch(string(raw), -1) {
			resolved := resolveTS(filepath.Dir(path), m[1])
			if resolved != "" {
				adj[key] = append(adj[key], resolved)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	color := make(map[string]int) // 0=unvisited, 1=visiting, 2=done
	var dfs func(u string) error
	dfs = func(u string) error {
		color[u] = 1
		for _, v := range adj[u] {
			if color[v] == 1 {
				return fmt.Errorf("circular TS import detected: %s <-> %s", filepath.Base(u), filepath.Base(v))
			}
			if color[v] == 0 {
				if err := dfs(v); err != nil {
					return err
				}
			}
		}
		color[u] = 2
		return nil
	}
	for node := range adj {
		if color[node] == 0 {
			if err := dfs(node); err != nil {
				return err
			}
		}
	}
	return nil
}

func normTS(p string) string {
	clean := filepath.Clean(p)
	return strings.TrimSuffix(strings.TrimSuffix(clean, ".tsx"), ".ts")
}

func resolveTS(dir, rel string) string {
	target := filepath.Join(dir, rel)
	for _, cand := range []string{target + ".ts", target + ".tsx", filepath.Join(target, "index.ts"), filepath.Join(target, "index.tsx")} {
		if _, err := os.Stat(cand); err == nil {
			return normTS(cand)
		}
	}
	return ""
}

var perfMarkerRe = regexp.MustCompile(`@perf-isolation\(parity_test="([^"]+)"\)`)

func checkSliceAndPerfIsolation(root string) error {
	fset := token.NewFileSet()
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
		slashed := filepath.ToSlash(rel)
		if !strings.HasSuffix(slashed, ".go") || strings.HasSuffix(slashed, "_test.go") ||
			strings.HasPrefix(slashed, "schema/") || strings.HasPrefix(slashed, "internal/contracts/generated_") ||
			strings.HasPrefix(slashed, "cmd/verify-gate/") || strings.HasPrefix(slashed, "internal/testutil/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		if m := perfMarkerRe.FindStringSubmatch(text); m != nil {
			for _, req := range []string{"What:", "Why:", "How:"} {
				if !strings.Contains(text, req) {
					violations = append(violations, fmt.Sprintf("%s has @perf-isolation missing %s", slashed, req))
				}
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(m[1]))); err != nil {
				violations = append(violations, fmt.Sprintf("%s parity test missing: %s", slashed, m[1]))
			}
			return nil
		}
		fileAST, err := parser.ParseFile(fset, path, raw, 0)
		if err != nil {
			return err
		}
		ast.Inspect(fileAST, func(n ast.Node) bool {
			checkLit := func(expr ast.Expr) {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					return
				}
				if v, err := strconv.Atoi(lit.Value); err == nil && v >= 16 {
					pos := fset.Position(lit.Pos())
					violations = append(violations, fmt.Sprintf("%s:%d raw slice/index >= 16 (%d) without @perf-isolation", slashed, pos.Line, v))
				}
			}
			switch node := n.(type) {
			case *ast.SliceExpr:
				checkLit(node.Low)
				checkLit(node.High)
				checkLit(node.Max)
			case *ast.IndexExpr:
				checkLit(node.Index)
			}
			return true
		})
		return nil
	})
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("SOLID-SLICE-01 / SOLID-PERF-01 violations:\n  - %s", strings.Join(violations, "\n  - "))
	}
	return nil
}
