package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
)

func TestM4_Adversarial_ConcurrentRESTHammerWhileSSEActive(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Maintain 10 background SSE streams
	const sseWorkers = 10
	var sseWg sync.WaitGroup
	sseWg.Add(sseWorkers)

	for i := 0; i < sseWorkers; i++ {
		go func() {
			defer sseWg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				return
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()
	}

	// Hammer GET /api/session and POST /api/session/reset concurrently
	const readerCount = 15
	const writerCount = 5
	var opWg sync.WaitGroup
	opWg.Add(readerCount + writerCount)

	var totalReads int64
	var totalResets int64

	// Readers hammering GET /api/session
	for r := 0; r < readerCount; r++ {
		go func() {
			defer opWg.Done()
			client := &http.Client{Timeout: 1 * time.Second}
			for ctx.Err() == nil {
				resp, err := client.Get(server.URL + "/api/session")
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					t.Errorf("GET /api/session failed: %v", err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("GET /api/session unexpected status: %d", resp.StatusCode)
				}
				var summary session.SessionResponse
				if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
					t.Errorf("failed to decode session summary: %v", err)
				}
				_ = resp.Body.Close()
				atomic.AddInt64(&totalReads, 1)
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Writers hammering POST /api/session/reset
	for w := 0; w < writerCount; w++ {
		go func() {
			defer opWg.Done()
			client := &http.Client{Timeout: 1 * time.Second}
			for ctx.Err() == nil {
				resp, err := client.Post(server.URL+"/api/session/reset", "application/json", nil)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					t.Errorf("POST /api/session/reset failed: %v", err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("POST /api/session/reset unexpected status: %d", resp.StatusCode)
				}
				var res map[string]string
				if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
					t.Errorf("failed to decode reset response: %v", err)
				} else if res["status"] != "ok" {
					t.Errorf("expected status ok, got %v", res)
				}
				_ = resp.Body.Close()
				atomic.AddInt64(&totalResets, 1)
				time.Sleep(15 * time.Millisecond)
			}
		}()
	}

	// Concurrently simulate live match concluded events
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		mIndex := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				mIndex++
				w := mIndex % 2
				st.ConcludeMatch(&playertrack.CurrentMatchResponse{
					MatchGUID:  fmt.Sprintf("guid-%d", mIndex),
					PlaylistID: 13,
					WinnerTeam: &w,
					LocalTeam:  &w,
					Result:     "victory",
				})
			}
		}
	}()

	<-ctx.Done()
	opWg.Wait()
	sseWg.Wait()

	reads := atomic.LoadInt64(&totalReads)
	resets := atomic.LoadInt64(&totalResets)
	if reads < 30 {
		t.Errorf("expected at least 30 reads during stress test, got %d", reads)
	}
	if resets < 5 {
		t.Errorf("expected at least 5 resets during stress test, got %d", resets)
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 4: Boundary & Method Check on /api/session/reset
// -----------------------------------------------------------------------------

func TestM4_Adversarial_Boundary_SessionResetMethods(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	// 1. POST succeeds
	resp, err := client.Post(server.URL+"/api/session/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/session/reset failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for POST /api/session/reset, got %d", resp.StatusCode)
	}

	// 2. Disallowed methods must return 405 Method Not Allowed
	disallowed := []string{
		http.MethodGet,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodHead,
	}

	for _, m := range disallowed {
		t.Run("Method_"+m, func(t *testing.T) {
			req, err := http.NewRequest(m, server.URL+"/api/session/reset", nil)
			if err != nil {
				t.Fatalf("request creation failed: %v", err)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s /api/session/reset failed: %v", m, err)
			}
			defer res.Body.Close()

			if res.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 Method Not Allowed for %s, got %d", m, res.StatusCode)
			}

			allow := res.Header.Get("Allow")
			if !strings.Contains(allow, "POST") {
				t.Errorf("expected Allow header to contain POST, got %q", allow)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 5: API Invalid Routes 404 Guard
// -----------------------------------------------------------------------------

func TestM4_Adversarial_Boundary_InvalidAPIRoutes(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	routes := []struct {
		name string
		path string
	}{
		{name: "invalid route under /api/", path: "/api/invalid_route"},
		{name: "nonexistent endpoint", path: "/api/does_not_exist"},
		{name: "invalid session subpath", path: "/api/session/unknown"},
		{name: "invalid players subpath", path: "/api/players/../invalid"},
		{name: "invalid events subpath", path: "/api/events/sub"},
		{name: "exact api root", path: "/api"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tc.path, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected 404 Not Found for %s, got HTTP %d", tc.path, resp.StatusCode)
			}
			if strings.Contains(bodyStr, "<div id=\"root\"></div>") {
				t.Errorf("SECURITY/ROUTING BUG: route %s fell back to SPA index.html instead of returning 404!", tc.path)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 6: Static Assets Path Traversal Boundary & Security
// -----------------------------------------------------------------------------

func TestM4_Adversarial_Security_PathTraversalStaticAssets(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	traversalPaths := []struct {
		name string
		path string
	}{
		{name: "parent traversal to etc passwd", path: "/../../etc/passwd"},
		{name: "deep parent traversal to win.ini", path: "/../../../windows/win.ini"},
		{name: "double slash dist parent", path: "//dist/.."},
		{name: "assets parent traversal", path: "/assets/../../secret.txt"},
		{name: "assets relative to index", path: "/assets/../index.html"},
		{name: "url encoded slash traversal", path: "/..%2f..%2fetc/passwd"},
		{name: "url encoded dot traversal", path: "/%2e%2e/%2e%2e/etc/passwd"},
	}

	for _, tc := range traversalPaths {
		t.Run(tc.name, func(t *testing.T) {
			// Test 1: Using client without auto-redirect to inspect raw immediate response
			tr := &http.Transport{
				DialContext: (&net.Dialer{}).DialContext,
			}
			noRedirectClient := &http.Client{
				Transport: tr,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			req, err := http.NewRequest(http.MethodGet, server.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("request creation failed: %v", err)
			}
			rawResp, err := noRedirectClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tc.path, err)
			}
			defer rawResp.Body.Close()
			rawBody, _ := io.ReadAll(rawResp.Body)
			rawBodyStr := string(rawBody)

			// Security Assertion 1: Must never escape embedded directory
			if strings.Contains(rawBodyStr, "root:x:") || strings.Contains(rawBodyStr, "[fonts]") || strings.Contains(rawBodyStr, "[extensions]") {
				t.Fatalf("CRITICAL SECURITY VULNERABILITY: %s leaked host filesystem file!", tc.path)
			}

			// Test 2: Standard client following redirects
			stdClient := server.Client()
			stdResp, err := stdClient.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("standard GET %s failed: %v", tc.path, err)
			}
			defer stdResp.Body.Close()
			stdBody, _ := io.ReadAll(stdResp.Body)
			stdBodyStr := string(stdBody)

			if strings.Contains(stdBodyStr, "root:x:") || strings.Contains(stdBodyStr, "[fonts]") {
				t.Fatalf("CRITICAL SECURITY VULNERABILITY: followed %s leaked host filesystem file!", tc.path)
			}

			// Boundary Check: Requirement states:
			// "Verify path traversal attempts on static assets (e.g. /../../etc/passwd or //dist/..) return 400 or 404 and cannot escape the embedded directory."
			// If raw response is not 400 or 404, or if followed response returns 200 index.html, flag as finding.
			if rawResp.StatusCode != http.StatusBadRequest && rawResp.StatusCode != http.StatusNotFound {
				t.Errorf("Path traversal %s: raw response was HTTP %d (Location: %q), expected 400 or 404",
					tc.path, rawResp.StatusCode, rawResp.Header.Get("Location"))
			}
			if stdResp.StatusCode == http.StatusOK && strings.Contains(stdBodyStr, "<div id=\"root\"></div>") {
				t.Errorf("Path traversal %s: standard client received HTTP 200 with index.html SPA fallback, expected 400 or 404 rejection", tc.path)
			}
		})
	}
}
