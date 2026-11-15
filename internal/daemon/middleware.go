package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
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

func isLoopbackHost(host string) bool {
	h := strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func isLocalInterfaceIP(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var ifaceIP net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ifaceIP = v.IP
		case *net.IPAddr:
			ifaceIP = v.IP
		}
		if ifaceIP != nil && ifaceIP.Equal(ip) {
			return true
		}
	}
	return false
}

func isTrustedNonLoopbackHostname(originHostname, configuredHost string) bool {
	if net.ParseIP(originHostname) != nil {
		return true
	}
	if configuredHost != "" && configuredHost != "0.0.0.0" && configuredHost != "::" && strings.EqualFold(originHostname, configuredHost) {
		return true
	}
	// Single-label intranet/NetBIOS hostnames cannot be registered on public DNS (immune to DNS rebinding).
	if !strings.Contains(originHostname, ".") {
		return true
	}
	for _, suffix := range []string{".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain", ".ts.net"} {
		if strings.HasSuffix(originHostname, suffix) {
			return true
		}
	}
	if machineHost, err := os.Hostname(); err == nil && machineHost != "" {
		mh := strings.ToLower(strings.TrimSpace(machineHost))
		if originHostname == mh {
			return true
		}
	}
	return false
}

func isLoopbackOrLocalPeer(remoteAddr string) bool {
	raw := strings.TrimSpace(remoteAddr)
	if raw == "" {
		return false
	}
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	return isLoopbackHost(host) || isLocalInterfaceIP(host)
}

func normalizeOriginHostPort(scheme, rawHost string) string {
	h := strings.ToLower(strings.TrimSpace(rawHost))
	if h == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(h)
	if err != nil {
		return h
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return h
}

func isAllowedMutatingOriginForHost(r *http.Request, configuredHost string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true // non-browser clients (curl, Go HTTP clients)
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	originHostname := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if originHostname == "" {
		return false
	}
	if isLoopbackHost(originHostname) {
		return true
	}

	cfgHost := strings.Trim(strings.ToLower(strings.TrimSpace(configuredHost)), "[]")
	origHost := normalizeOriginHostPort(scheme, u.Host)

	if isLoopbackOrLocalPeer(r.RemoteAddr) {
		if xfh := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); xfh != "" {
			if normalizeOriginHostPort(scheme, xfh) == origHost && isTrustedNonLoopbackHostname(originHostname, cfgHost) {
				return true
			}
		}
		if isLocalInterfaceIP(originHostname) {
			return true
		}
	}

	if cfgHost == "" || isLoopbackHost(cfgHost) {
		return false
	}

	// When web.host is non-loopback (e.g. "0.0.0.0"), allow same-origin requests over IP literals
	// or trusted local/tailnet hostnames, plus dev-proxy origins on this machine's own NIC IPs.
	reqHost := normalizeOriginHostPort(scheme, r.Host)
	if reqHost != "" && origHost == reqHost && isTrustedNonLoopbackHostname(originHostname, cfgHost) {
		return true
	}
	return isLocalInterfaceIP(originHostname)
}

func isAllowedMutatingOrigin(r *http.Request) bool {
	return isAllowedMutatingOriginForHost(r, "127.0.0.1")
}

func (d *Daemon) isAllowedMutatingOrigin(r *http.Request) bool {
	if d == nil || d.cfg == nil {
		return isAllowedMutatingOrigin(r)
	}
	return isAllowedMutatingOriginForHost(r, d.httpHost())
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
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
	reqURI := r.RequestURI
	if strings.Contains(reqURI, "..") || strings.HasPrefix(reqURI, "//") {
		return true
	}
	lowerURI := strings.ToLower(reqURI)
	if strings.Contains(lowerURI, "%2e") || strings.Contains(lowerURI, "%5c") || strings.Contains(lowerURI, "%00") {
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

// corsMiddleware enforces path traversal rejection, standard CORS headers, preflight
// handling, and Origin CSRF / DNS-rebinding checks on mutating HTTP methods.
func corsMiddleware(next http.Handler, allowOriginFn ...func(*http.Request) bool) http.Handler {
	originCheck := isAllowedMutatingOrigin
	if len(allowOriginFn) > 0 && allowOriginFn[0] != nil {
		originCheck = allowOriginFn[0]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject path traversal attempts immediately before ServeMux canonicalization redirects
		if isPathTraversal(r) {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Origin, Accept")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if isMutatingMethod(r.Method) && !originCheck(r) {
			writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
			return
		}

		next.ServeHTTP(w, r)
	})
}
