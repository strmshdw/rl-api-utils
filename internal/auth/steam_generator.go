package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SteamTicketOptions defines parameters for generating a Rocket League Steam session ticket.
type SteamTicketOptions struct {
	Username     string
	Password     string
	LoginKey     string
	AccountLabel string
	ScriptDir    string
}

// SteamTicketResult captures the output from the Steam ticket generator.
type SteamTicketResult struct {
	Success       bool   `json:"success"`
	SessionTicket string `json:"sessionTicket"`
	SteamID64     string `json:"steamID64"`
	LoginKey      string `json:"loginKey"`
	AccountName   string `json:"accountName"`
	Error         string `json:"error,omitempty"`
}

// SteamTicketGenerator abstracts generating a Steam session ticket.
// Allows mock implementations in unit tests.
type SteamTicketGenerator interface {
	GenerateTicket(ctx context.Context, opts SteamTicketOptions) (*SteamTicketResult, error)
}

// NodeSteamTicketGenerator executes steam/steam-auth.js via Node.js.
type NodeSteamTicketGenerator struct {
	NodePath  string
	ScriptDir string
}

// NewNodeSteamTicketGenerator creates a NodeSteamTicketGenerator.
func NewNodeSteamTicketGenerator(scriptDir string) *NodeSteamTicketGenerator {
	if scriptDir == "" {
		scriptDir = "./steam"
	}
	return &NodeSteamTicketGenerator{
		NodePath:  "node",
		ScriptDir: scriptDir,
	}
}

// GenerateTicket invokes node steam-auth.js, attaching stdin/stderr for interactive Steam Guard prompts.
func (g *NodeSteamTicketGenerator) GenerateTicket(ctx context.Context, opts SteamTicketOptions) (*SteamTicketResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scriptDir := opts.ScriptDir
	if scriptDir == "" {
		scriptDir = g.ScriptDir
	}

	absScriptPath, err := findSteamAuthScript(scriptDir)
	if err != nil {
		return nil, fmt.Errorf("locating steam-auth.js: %w", err)
	}

	args := []string{
		absScriptPath,
		"--username", opts.Username,
		"--account-type", opts.AccountLabel,
	}
	if opts.LoginKey != "" {
		args = append(args, "--login-key", opts.LoginKey)
	}
	if opts.Password != "" {
		args = append(args, "--password", opts.Password)
	}

	nodeBin := g.NodePath
	if nodeBin == "" {
		nodeBin = "node"
	}

	cmd := exec.CommandContext(ctx, nodeBin, args...)
	cmd.Dir = filepath.Dir(absScriptPath)

	// Attach stdin and stderr for interactive Steam Guard prompts
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf

	runErr := cmd.Run()

	stdoutStr := strings.TrimSpace(stdoutBuf.String())
	if stdoutStr == "" {
		if runErr != nil {
			return nil, fmt.Errorf("steam authenticator failed with error: %w", runErr)
		}
		return nil, errors.New("steam authenticator produced empty output")
	}

	// The last line of stdout is our JSON output
	lines := strings.Split(stdoutStr, "\n")
	lastLine := strings.TrimSpace(lines[len(lines)-1])

	var res SteamTicketResult
	if err := json.Unmarshal([]byte(lastLine), &res); err != nil {
		return nil, fmt.Errorf("parsing steam authenticator output %q: %w (full output: %s)", lastLine, err, stdoutStr)
	}

	if !res.Success {
		if res.Error != "" {
			return nil, fmt.Errorf("steam authentication failed: %s", res.Error)
		}
		return nil, errors.New("steam authentication failed")
	}

	if res.SessionTicket == "" {
		return nil, errors.New("steam authenticator returned empty session ticket")
	}

	return &res, nil
}

// findSteamAuthScript searches for steam-auth.js across configured directory,
// working directory ancestry, and executable directory ancestry.
func findSteamAuthScript(scriptDir string) (string, error) {
	// 1. Check explicitly specified scriptDir
	if scriptDir != "" {
		candidates := []string{
			filepath.Join(scriptDir, "steam-auth.js"),
			scriptDir,
		}
		for _, cand := range candidates {
			if abs, err := filepath.Abs(cand); err == nil {
				if stat, err := os.Stat(abs); err == nil && !stat.IsDir() {
					return abs, nil
				}
			}
		}
	}

	// 2. Search upward from current working directory
	if cwd, err := os.Getwd(); err == nil {
		curr := cwd
		for {
			target := filepath.Join(curr, "steam", "steam-auth.js")
			if stat, err := os.Stat(target); err == nil && !stat.IsDir() {
				return target, nil
			}
			targetDirect := filepath.Join(curr, "steam-auth.js")
			if stat, err := os.Stat(targetDirect); err == nil && !stat.IsDir() {
				return targetDirect, nil
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
	}

	// 3. Search upward from executable directory
	if exe, err := os.Executable(); err == nil {
		curr := filepath.Dir(exe)
		for {
			target := filepath.Join(curr, "steam", "steam-auth.js")
			if stat, err := os.Stat(target); err == nil && !stat.IsDir() {
				return target, nil
			}
			targetDirect := filepath.Join(curr, "steam-auth.js")
			if stat, err := os.Stat(targetDirect); err == nil && !stat.IsDir() {
				return targetDirect, nil
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
	}

	return "", errors.New("steam-auth.js not found in working directory, executable directory, or parents")
}

