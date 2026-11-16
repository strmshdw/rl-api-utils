package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dank/rl-api-utils/internal/domain"
)

// MaxJSONBodyBytes bounds JSON request payloads across mutating daemon endpoints (1 MiB).
const MaxJSONBodyBytes int64 = 1 << 20

// decodeJSONBody enforces MaxJSONBodyBytes via http.MaxBytesReader and decodes a JSON request body.
// When allowEmpty is true, an empty body (io.EOF) succeeds without populating dst.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) bool {
	if r.Body == nil {
		if allowEmpty {
			return true
		}
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return false
	}

	body := http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	defer body.Close()

	if err := json.NewDecoder(body).Decode(dst); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return true
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	payload, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		payload = []byte(`{"error":"internal server error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(payload, '\n'))
}

func isPathTraversal(r *http.Request) bool {
	p := r.URL.Path
	if strings.Contains(p, "..") || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return true
	}
	raw := r.URL.RawPath
	if raw != "" {
		if strings.Contains(raw, "..") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
			return true
		}
		lowerRaw := strings.ToLower(raw)
		if strings.Contains(lowerRaw, "%2e") || strings.Contains(lowerRaw, "%5c") || strings.Contains(lowerRaw, "%00") {
			return true
		}
	}
	reqPath, _, _ := strings.Cut(r.RequestURI, "?")
	if strings.Contains(reqPath, "..") || strings.HasPrefix(reqPath, "//") {
		return true
	}
	lowerPath := strings.ToLower(reqPath)
	if strings.Contains(lowerPath, "%2e") || strings.Contains(lowerPath, "%5c") || strings.Contains(lowerPath, "%00") {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if strings.TrimSpace(seg) == "" {
			continue
		}
		if _, err := domain.SanitizePathSegment(seg); err != nil {
			return true
		}
	}
	return false
}

// corsMiddleware enforces path traversal rejection, standard CORS headers, and preflight
// handling without imposing Origin/authz barriers on LAN, proxy, or local network clients.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject path traversal attempts immediately before ServeMux canonicalization redirects
		if isPathTraversal(r) {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Origin, Accept, Authorization, X-Requested-With")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
