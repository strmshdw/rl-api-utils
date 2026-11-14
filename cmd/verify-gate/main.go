package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	root := flag.String("root", ".", "repository root")
	skipBuild := flag.Bool("skip-build", false, "skip Pillar 2 build commands")
	skipTests := flag.Bool("skip-tests", false, "skip Pillar 3 test commands")
	allowLegacyLOC := flag.Bool("allow-legacy-loc", false, "only warn on >800 LOC (never allowed in release gate)")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify-gate: resolve root: %v\n", err)
		os.Exit(1)
	}

	if err := checkTrunkLock(absRoot); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(42)
	}
	fmt.Println("[Pillar 4] Trunk lock OK (not on main/master)")

	if err := runPillar1(absRoot, *allowLegacyLOC); err != nil {
		fmt.Fprintf(os.Stderr, "[Pillar 1] FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[Pillar 1] Static, schema, LOC, DAG, slice/perf-isolation & type checks PASSED")

	if !*skipBuild {
		if err := runPillar2(absRoot); err != nil {
			fmt.Fprintf(os.Stderr, "[Pillar 2] FAILED: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[Pillar 2] Frontend production bundle & Go binary build PASSED")
	}

	if !*skipTests {
		if err := runPillar3(absRoot); err != nil {
			fmt.Fprintf(os.Stderr, "[Pillar 3] FAILED: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[Pillar 3] Go (-race) & Vitest suites PASSED")
	}

	fmt.Println("4-PILLAR PRE-MERGE GATE PASSED (Exit Code 0)")
}

func checkTrunkLock(root string) error {
	out, err := runCmdCapture(root, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("[Pillar 4] git branch check failed: %w", err)
	}
	branch := strings.TrimSpace(out)
	if branch == "main" || branch == "master" {
		return fmt.Errorf("[Pillar 4] BLOCKED (Exit 42): direct verification/edits on protected trunk branch %q", branch)
	}
	return nil
}

func runPillar1(root string, allowLegacyLOC bool) error {
	if err := checkSpecsSize(root); err != nil {
		return err
	}
	if err := runCmdStream(root, "go", "run", "./cmd/schemagen", "-check", "-root", root); err != nil {
		return fmt.Errorf("schemagen -check: %w", err)
	}
	if err := checkFileLOC(root, 800, allowLegacyLOC); err != nil {
		return err
	}
	if err := checkGoDAG(root); err != nil {
		return err
	}
	if err := checkTSCycles(root); err != nil {
		return err
	}
	if err := checkSliceAndPerfIsolation(root); err != nil {
		return err
	}
	if err := runCmdStream(root, "go", "vet", "./..."); err != nil {
		return fmt.Errorf("go vet: %w", err)
	}
	if err := runNpm(root, "exec", "tsc", "--", "-b"); err != nil {
		return fmt.Errorf("tsc -b: %w", err)
	}
	return nil
}

func runPillar2(root string) error {
	if err := runNpm(root, "run", "build"); err != nil {
		return fmt.Errorf("web production build: %w", err)
	}
	if err := checkProdBundleNoPreview(root); err != nil {
		return err
	}
	tmpOut := filepath.Join(os.TempDir(), "rl-sync-verify-build.exe")
	defer os.Remove(tmpOut)
	if err := runCmdStream(root, "go", "build", "-o", tmpOut, "./cmd/rl-sync"); err != nil {
		return fmt.Errorf("go build ./cmd/rl-sync: %w", err)
	}
	return nil
}

func runPillar3(root string) error {
	if _, err := exec.LookPath("gcc"); err == nil {
		if err := runCmdStream(root, "go", "test", "-race", "-count=1", "./..."); err != nil {
			return fmt.Errorf("go test -race: %w", err)
		}
	} else {
		fmt.Println("[Pillar 3] gcc not found on PATH; running go test -count=1 ./... (non-cgo mode)")
		if err := runCmdStream(root, "go", "test", "-count=1", "./..."); err != nil {
			return fmt.Errorf("go test: %w", err)
		}
	}
	if err := runNpm(root, "test", "--", "--run"); err != nil {
		return fmt.Errorf("vitest: %w", err)
	}
	return nil
}

func runNpm(root string, args ...string) error {
	npmExe := "npm"
	if _, err := exec.LookPath("npm.cmd"); err == nil {
		npmExe = "npm.cmd"
	}
	fullArgs := append([]string{"--prefix", "web"}, args...)
	return runCmdStream(root, npmExe, fullArgs...)
}

func runCmdCapture(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func runCmdStream(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
