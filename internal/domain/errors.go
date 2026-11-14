package domain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
)

const (
	winsockWSAECONNABORTED = syscall.Errno(10053)
	winsockWSAECONNRESET   = syscall.Errno(10054)
)

// IsDisconnectError reports whether ctx or err reflects an expected client disconnect or context cancellation.
func IsDisconnectError(ctx context.Context, err error) bool {
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, winsockWSAECONNABORTED) ||
		errors.Is(err, winsockWSAECONNRESET) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "an established connection was aborted by the software in your host machine") ||
		strings.Contains(msg, "an existing connection was forcibly closed by the remote host") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "client disconnected") ||
		strings.Contains(msg, "http2: stream closed")
}

// SanitizePathSegment rejects empty segments, path separators, traversal markers, and control bytes.
func SanitizePathSegment(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("identifier cannot be empty")
	}
	if trimmed == "." || trimmed == ".." || strings.Contains(trimmed, "..") {
		return "", fmt.Errorf("invalid identifier %q: path traversal forbidden", raw)
	}
	if strings.ContainsAny(trimmed, "/\\\x00") {
		return "", fmt.Errorf("invalid identifier %q: illegal path character", raw)
	}
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("invalid identifier %q: control character forbidden", raw)
		}
	}
	return trimmed, nil
}
