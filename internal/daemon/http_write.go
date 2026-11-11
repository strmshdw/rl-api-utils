package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"
)

const (
	// Windows Winsock error codes surfaced via os.SyscallError("wsasend", syscall.Errno(...))
	// when a browser or local client aborts an in-flight HTTP connection.
	winsockWSAECONNABORTED = syscall.Errno(10053)
	winsockWSAECONNRESET   = syscall.Errno(10054)
)

// isClientDisconnect reports whether err or ctx indicates that the HTTP client
// closed or aborted the connection before the response finished transmitting.
func isClientDisconnect(ctx context.Context, err error) bool {
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

// writeJSONResponse marshals payload prior to committing HTTP headers so genuine serialization
// failures return HTTP 500 and log a warning, while client-initiated socket disconnects during
// response transmission are logged at Debug rather than Warn.
func (d *Daemon) writeJSONResponse(w http.ResponseWriter, r *http.Request, statusCode int, payload any, encodeWarnMsg string) {
	var reqCtx context.Context
	if r != nil {
		reqCtx = r.Context()
	}
	if isClientDisconnect(reqCtx, nil) {
		d.logger.Debug("skipping response write; client disconnected", slog.String("context", encodeWarnMsg))
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		d.logger.Warn(encodeWarnMsg, slog.Any("error", err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{\"error\":\"internal encoding error\"}\n"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		if isClientDisconnect(reqCtx, err) {
			d.logger.Debug("client disconnected while writing response",
				slog.String("context", encodeWarnMsg),
				slog.Any("error", err),
			)
			return
		}
		d.logger.Warn(encodeWarnMsg, slog.Any("error", err))
	}
}
