package daemon_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/session"
)

func TestWebIntegration_StaticAndSPAFallback(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	// 1. Root / serves index.html
	respRoot, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer respRoot.Body.Close()

	if respRoot.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /, got %d", respRoot.StatusCode)
	}
	if ct := respRoot.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %s", ct)
	}
	bodyRoot, _ := io.ReadAll(respRoot.Body)
	if !strings.Contains(string(bodyRoot), `<div id="root"></div>`) {
		t.Errorf("expected index.html body to contain root div, got: %s", string(bodyRoot))
	}

	// 2. /index.html serves index.html with Cache-Control: no-cache
	respIndex, err := client.Get(server.URL + "/index.html")
	if err != nil {
		t.Fatalf("GET /index.html failed: %v", err)
	}
	defer respIndex.Body.Close()
	if respIndex.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /index.html, got %d", respIndex.StatusCode)
	}
	if cc := respIndex.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Errorf("expected no-cache Cache-Control for index.html, got %s", cc)
	}

	// 3. Static JS/CSS asset serving (filename is content-hashed, so read it from index.html)
	m := regexp.MustCompile(`src="(/assets/[^"]+\.js)"`).FindStringSubmatch(string(bodyRoot))
	if m == nil {
		t.Fatalf("index.html does not reference a hashed /assets/*.js entry: %s", string(bodyRoot))
	}
	respJS, err := client.Get(server.URL + m[1])
	if err != nil {
		t.Fatalf("GET asset JS failed: %v", err)
	}
	defer respJS.Body.Close()
	if respJS.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for static JS asset, got %d", respJS.StatusCode)
	}
	if ct := respJS.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("expected javascript Content-Type, got %s", ct)
	}

	// 4. SPA Fallback: /session-history
	respHistory, err := client.Get(server.URL + "/session-history")
	if err != nil {
		t.Fatalf("GET /session-history failed: %v", err)
	}
	defer respHistory.Body.Close()
	if respHistory.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback /session-history, got %d", respHistory.StatusCode)
	}
	bodyHistory, _ := io.ReadAll(respHistory.Body)
	if !strings.Contains(string(bodyHistory), `<div id="root"></div>`) {
		t.Errorf("expected index.html fallback for /session-history, got: %s", string(bodyHistory))
	}

	// 5. SPA Fallback: /overlay & /?mode=overlay
	respOverlay, err := client.Get(server.URL + "/overlay")
	if err != nil {
		t.Fatalf("GET /overlay failed: %v", err)
	}
	defer respOverlay.Body.Close()
	if respOverlay.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /overlay, got %d", respOverlay.StatusCode)
	}

	respModeOverlay, err := client.Get(server.URL + "/?mode=overlay")
	if err != nil {
		t.Fatalf("GET /?mode=overlay failed: %v", err)
	}
	defer respModeOverlay.Body.Close()
	if respModeOverlay.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /?mode=overlay, got %d", respModeOverlay.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// 5. SSE Stream Delivery: /api/events (Snapshot, Push, Keepalive, Disconnect)
// -----------------------------------------------------------------------------

func TestWebIntegration_SSE_Stream(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("creating SSE request failed: %v", err)
	}

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/events failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for SSE, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	reader := bufio.NewReader(resp.Body)

	lineChan := make(chan string, 100)
	errChan := make(chan error, 1)
	go func() {
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				select {
				case errChan <- readErr:
				default:
				}
				return
			}
			lineChan <- line
		}
	}()

	readEventWithTimeout := func(desc string, timeout time.Duration) (string, string) {
		var evType, data string
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		for {
			select {
			case line := <-lineChan:
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "event:") {
					evType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				} else if strings.HasPrefix(line, "data:") {
					data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				} else if line == "" && evType != "" {
					return evType, data
				}
			case err := <-errChan:
				t.Fatalf("reading SSE stream for %s failed: %v", desc, err)
				return "", ""
			case <-timer.C:
				cancel()
				_ = resp.Body.Close()
				t.Fatalf("timed out after %v waiting for SSE %s", timeout, desc)
				return "", ""
			}
		}
	}

	// 1. Initial snapshot must be event: session_update
	initialEventType, initialData := readEventWithTimeout("initial snapshot (session_update)", 2*time.Second)

	if initialEventType != "session_update" {
		t.Errorf("expected initial event to be session_update, got %s", initialEventType)
	}
	if !strings.Contains(initialData, "session_id") {
		t.Errorf("expected initial data to contain session_id, got: %s", initialData)
	}

	// 2. Broadcast event distribution
	// Ensure the SSE subscriber is registered before broadcasting to prevent racing
	subDeadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(subDeadline) {
		if st.Broadcaster().SubscriberCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count := st.Broadcaster().SubscriberCount(); count == 0 {
		t.Fatalf("timed out waiting for SSE subscriber registration: subscriber count is 0")
	}

	matchEvent := session.SessionEvent{
		Event: session.EventMatchUpdate,
		Data: map[string]interface{}{
			"active_match": true,
			"playlist_id":  13,
		},
	}
	st.Broadcaster().Broadcast(matchEvent)

	broadcastType, broadcastData := readEventWithTimeout("broadcast event (match_update)", 2*time.Second)

	if broadcastType != "match_update" {
		t.Errorf("expected broadcast event to be match_update, got %s", broadcastType)
	}
	if !strings.Contains(broadcastData, `"active_match":true`) {
		t.Errorf("expected broadcast data to contain active_match, got %s", broadcastData)
	}

	// 3. Client disconnection cleanup
	cancel()
	_ = resp.Body.Close()

	// Wait for subscriber channel to be removed
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if st.Broadcaster().SubscriberCount() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if count := st.Broadcaster().SubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers after client disconnect, got %d", count)
	}
}

// -----------------------------------------------------------------------------
// 6. CORS Headers: Simple Requests & Preflight OPTIONS
// -----------------------------------------------------------------------------

func TestWebIntegration_CORS(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	endpoints := []string{
		"/api/session",
		"/api/current-match",
		"/api/players",
	}

	for _, ep := range endpoints {
		t.Run("GET_"+ep, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, server.URL+ep, nil)
			req.Header.Set("Origin", "http://localhost:5173")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "*" && acao != "http://localhost:5173" {
				t.Errorf("expected Access-Control-Allow-Origin, got %q", acao)
			}
		})
	}

	// OPTIONS Preflight
	t.Run("OPTIONS_Preflight_Reset", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodOptions, server.URL+"/api/session/reset", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "Content-Type")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("OPTIONS request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			t.Errorf("expected 204 or 200 for OPTIONS, got %d", resp.StatusCode)
		}
		if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao == "" {
			t.Error("missing Access-Control-Allow-Origin on preflight")
		}
		if acam := resp.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(acam, "POST") {
			t.Errorf("expected POST in Access-Control-Allow-Methods, got %s", acam)
		}
		if acah := resp.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(acah, "Content-Type") {
			t.Errorf("expected Content-Type in Access-Control-Allow-Headers, got %s", acah)
		}
	})
}

// -----------------------------------------------------------------------------
// 7. Network LAN Discovery & Banner Formatting Unit Tests
// -----------------------------------------------------------------------------

func TestNetwork_LAN_Discovery_Unit(t *testing.T) {
	// Test banner formatting with active LAN IP
	banner := daemon.FormatStartupBanner("0.0.0.0", 49125, "192.168.1.150")
	expected := "Web Dashboard:\n  Local:   http://localhost:49125\n  Network: http://192.168.1.150:49125\n  Overlay: http://localhost:49125/?mode=overlay"
	if banner != expected {
		t.Errorf("banner mismatch:\nexpected:\n%s\ngot:\n%s", expected, banner)
	}

	// Test banner with loopback bind
	bannerLoopback := daemon.FormatStartupBanner("127.0.0.1", 49125, "192.168.1.150")
	if !strings.Contains(bannerLoopback, "disabled") {
		t.Errorf("expected loopback banner to mention disabled, got:\n%s", bannerLoopback)
	}

	// Test banner with unavailable network
	bannerNoLAN := daemon.FormatStartupBanner("0.0.0.0", 49125, "")
	if !strings.Contains(bannerNoLAN, "unavailable") {
		t.Errorf("expected no-LAN banner to mention unavailable, got:\n%s", bannerNoLAN)
	}

	// Test real host discovery does not crash
	ip, err := daemon.DiscoverLANIPv4()
	if err == nil {
		if net.ParseIP(ip) == nil {
			t.Errorf("discovered invalid IP: %s", ip)
		}
	}
}

// -----------------------------------------------------------------------------
// 8. API Route 404 Guard & Security Verification
// -----------------------------------------------------------------------------

func TestWebIntegration_Api404Guard(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	guardedPaths := []string{
		"/api",
		"/api/",
		"/api/invalid",
		"/api/nonexistent",
		"/api/session/unknown",
		"/api/players/../invalid",
	}

	for _, p := range guardedPaths {
		t.Run(p, func(t *testing.T) {
			resp, err := client.Get(server.URL + p)
			if err != nil {
				t.Fatalf("GET %s failed: %v", p, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected 404 Not Found for %s, got HTTP %d", p, resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), "<div id=\"root\"></div>") {
				t.Errorf("route %s erroneously fell back to SPA index.html", p)
			}
		})
	}
}
