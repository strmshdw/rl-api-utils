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

	// Resolve absolute path to steam-auth.js
	scriptPath := filepath.Join(scriptDir, "steam-auth.js")
	if _, err := os.Stat(scriptPath); err != nil {
		// Fallback check in parent or working directory
		altPath := filepath.Join("steam", "steam-auth.js")
		if _, altErr := os.Stat(altPath); altErr == nil {
			scriptPath = altPath
		} else {
			return nil, fmt.Errorf("steam-auth.js not found at %s: %w", scriptPath, err)
		}
	}

	args := []string{
		scriptPath,
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
	cmd.Dir = filepath.Dir(scriptPath)

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
