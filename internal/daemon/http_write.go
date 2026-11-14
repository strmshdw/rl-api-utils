package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dank/rl-api-utils/internal/domain"
)

// isClientDisconnect reports whether err or ctx indicates that the HTTP client
// closed or aborted the connection before the response finished transmitting.
func isClientDisconnect(ctx context.Context, err error) bool {
	return domain.IsDisconnectError(ctx, err)
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
