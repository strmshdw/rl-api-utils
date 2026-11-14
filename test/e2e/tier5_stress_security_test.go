package e2e

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/web"
)

// ============================================================================
// Suite 5: Raw Socket Adversarial Security & Static Boundary Penetration
// ============================================================================

// TestTier5_Adversarial_RawSocketPathTraversalAndBoundary bypasses client-side URL
// normalization by transmitting un-sanitized raw HTTP/1.1 bytes across direct TCP
// sockets to probe daemon static handlers and path traversal guards.
func TestTier5_Adversarial_RawSocketPathTraversalAndBoundary(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state_raw_sec.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer store.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Web: config.WebConfig{Enabled: true, Host: "127.0.0.1", Port: 0},
		PlayerTracking: config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001|0",
		},
	}

	d, err := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithStateStore(store),
		daemon.WithWebHandler(web.DistHandler()),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	serverAddr := server.Listener.Addr().String()

	// Helper to send raw HTTP/1.1 request bytes over raw TCP connection
	sendRawRequest := func(method, rawPath string) (*http.Response, string, error) {
		conn, dialErr := net.DialTimeout("tcp", serverAddr, 2*time.Second)
		if dialErr != nil {
			return nil, "", fmt.Errorf("tcp dial failed: %w", dialErr)
		}
		defer conn.Close()

		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

		rawReq := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", method, rawPath, serverAddr)
		if _, writeErr := conn.Write([]byte(rawReq)); writeErr != nil {
			return nil, "", fmt.Errorf("conn write failed: %w", writeErr)
		}

		reader := bufio.NewReader(conn)
		resp, readErr := http.ReadResponse(reader, nil)
		if readErr != nil {
			return nil, "", fmt.Errorf("read response failed: %w", readErr)
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)
		return resp, string(bodyBytes), nil
	}

	// 1. Mandatory 14 Path Traversal Variants via Raw Sockets
	traversalVectors := []string{
		"//dist/..",
		"/..",
		"/../../etc/passwd",
		"/..%2f..%2fetc/passwd",
		"/%2e%2e/%2e%2e/windows/win.ini",
		"/%2e%2e",
		"/assets/../index.html",
		"/assets/%2e%2e/dist/index.html",
		"/assets/..%2findex.html",
		"/api/../index.html",
		"/api/%2e%2e/index.html",
		"/\\..\\windows\\win.ini",
		"/..\\..\\windows\\system.ini",
		"/..%5c..%5cwindows%5cwin.ini",
	}

	for _, vec := range traversalVectors {
		t.Run("RawSocket_Traversal_"+vec, func(t *testing.T) {
			resp, body, err := sendRawRequest(http.MethodGet, vec)
			if err != nil {
				t.Fatalf("raw request failed for %q: %v", vec, err)
			}

			// Must be rejected with 400 or 404
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				t.Errorf("vector %q returned status %d, expected 400 or 404", vec, resp.StatusCode)
			}

			// Traversal must NEVER return SPA index.html or leak system files
			if strings.Contains(body, "<div id=\"root\">") || strings.Contains(body, "rl-sync-web") {
				t.Errorf("vector %q leaked SPA index.html via raw TCP socket!", vec)
			}
			if strings.Contains(body, "[extensions]") || strings.Contains(body, "root:") || strings.Contains(body, "[boot loader]") {
				t.Errorf("vector %q leaked host filesystem contents via raw TCP socket!", vec)
			}
		})
	}

	// 1b. Extended Path Traversal Vectors
	extendedVectors := []string{
		"//..//windows//win.ini",
		"/./../../etc/shadow",
		"/%2e%2e%2f",
		"/assets/..",
		"/..;",
	}

	for _, vec := range extendedVectors {
		t.Run("RawSocket_ExtendedTraversal_"+vec, func(t *testing.T) {
			resp, body, err := sendRawRequest(http.MethodGet, vec)
			if err != nil {
				t.Fatalf("raw request failed for %q: %v", vec, err)
			}

			// Extended traversals must be rejected with 400 or 404
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				t.Errorf("vector %q returned status %d, expected 400 or 404", vec, resp.StatusCode)
			}

			if strings.Contains(body, "<div id=\"root\">") || strings.Contains(body, "rl-sync-web") {
				t.Errorf("vector %q leaked SPA index.html!", vec)
			}
			if strings.Contains(body, "[extensions]") || strings.Contains(body, "root:") {
				t.Errorf("vector %q leaked host filesystem contents!", vec)
			}
		})
	}

	// 1c. Double-Encoding Exploratory Probe: /%252e%252e/
	// Demonstrates that even though double-URL-encoded %252e decodes to %2e, it never traverses
	// out of the sandbox or leaks host filesystem files.
	t.Run("RawSocket_DoubleEncoding_Probe", func(t *testing.T) {
		_, body, err := sendRawRequest(http.MethodGet, "/%252e%252e/")
		if err != nil {
			t.Fatalf("raw request failed: %v", err)
		}
		if strings.Contains(body, "[extensions]") || strings.Contains(body, "root:") || strings.Contains(body, "[boot loader]") {
			t.Errorf("double-encoded traversal leaked host files!")
		}
	})

	// 2. Strict /api Guard: Raw requests to /api and /api/* must NEVER fall back to index.html
	apiVectors := []string{
		"/api",
		"/api/",
		"/api/unknown",
		"/api/unknown/nested",
		"/api/v1/invalid",
		"/api/session/extra",
	}

	for _, vec := range apiVectors {
		t.Run("RawSocket_APIGuard_"+vec, func(t *testing.T) {
			resp, body, err := sendRawRequest(http.MethodGet, vec)
			if err != nil {
				t.Fatalf("raw request failed for %q: %v", vec, err)
			}

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("API guard vector %q returned status %d, expected 404", vec, resp.StatusCode)
			}
			if strings.Contains(body, "<div id=\"root\">") || strings.Contains(body, "<html") {
				t.Errorf("API guard vector %q erroneously fell back to index.html!", vec)
			}
		})
	}

	// 3. Static HTTP Method Boundary Enforcement
	methodVectors := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/index.html"},
		{http.MethodPut, "/index.html"},
		{http.MethodDelete, "/session"},
		{http.MethodPatch, "/"},
	}

	for _, mv := range methodVectors {
		t.Run("RawSocket_Method_"+mv.method+"_"+mv.path, func(t *testing.T) {
			resp, _, err := sendRawRequest(mv.method, mv.path)
			if err != nil {
				t.Fatalf("raw request failed for %s %s: %v", mv.method, mv.path, err)
			}

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s returned status %d, expected 405 Method Not Allowed", mv.method, mv.path, resp.StatusCode)
			}
		})
	}

	// 4. Valid SPA Route Fallbacks over Raw Socket
	validSPAVectors := []string{
		"/",
		"/session",
		"/dashboard",
		"/overlay",
	}

	for _, vec := range validSPAVectors {
		t.Run("RawSocket_ValidSPA_"+vec, func(t *testing.T) {
			resp, body, err := sendRawRequest(http.MethodGet, vec)
			if err != nil {
				t.Fatalf("raw request failed for %q: %v", vec, err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf("valid SPA vector %q returned status %d, expected 200 OK", vec, resp.StatusCode)
			}
			if !strings.Contains(body, "<html") && !strings.Contains(body, "<!doctype html>") {
				t.Errorf("valid SPA vector %q did not return HTML content", vec)
			}
			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "text/html") {
				t.Errorf("valid SPA vector %q had Content-Type %q, expected text/html", vec, ct)
			}
		})
	}
}
