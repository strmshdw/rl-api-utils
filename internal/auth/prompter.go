package auth

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// CodePrompter provides an interface for prompting the user for an authorization code.
// Abstracted to allow automated unit testing without waiting for interactive input.
type CodePrompter interface {
	PromptForCode(ctx context.Context, accountLabel, authURL string) (string, error)
}

// TerminalPrompter prompts the user via an io.Reader and io.Writer.
type TerminalPrompter struct {
	In  io.Reader
	Out io.Writer
}

// NewTerminalPrompter creates a TerminalPrompter reading from in and writing to out.
// If in is nil, os.Stdin is used. If out is nil, os.Stdout is used.
func NewTerminalPrompter(in io.Reader, out io.Writer) *TerminalPrompter {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &TerminalPrompter{
		In:  in,
		Out: out,
	}
}

// PromptForCode displays a structured authentication banner and waits for the user's input.
func (p *TerminalPrompter) PromptForCode(ctx context.Context, accountLabel, authURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	banner := fmt.Sprintf(`
================================================================================
[%s] Epic Games Authentication Required
Please visit the following URL in your browser to log in to Epic Games:

%s

After logging in, copy the authorization code from the redirected page.
================================================================================
[%s] Enter authorization code: `, accountLabel, authURL, accountLabel)

	_, _ = fmt.Fprint(p.Out, banner)

	type scanResult struct {
		text string
		err  error
	}

	resCh := make(chan scanResult, 1)

	go func() {
		scanner := bufio.NewScanner(p.In)
		if scanner.Scan() {
			resCh <- scanResult{text: strings.TrimSpace(scanner.Text()), err: nil}
		} else {
			if err := scanner.Err(); err != nil {
				resCh <- scanResult{err: err}
			} else {
				resCh <- scanResult{err: io.EOF}
			}
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return "", fmt.Errorf("reading authorization code: %w", res.err)
		}
		if res.text == "" {
			return "", fmt.Errorf("authorization code cannot be empty")
		}
		return res.text, nil
	}
}
