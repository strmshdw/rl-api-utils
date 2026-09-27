package daemon_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// Challenger Empirical Stress Suite: Routing Boundaries & Traversal Security
// -----------------------------------------------------------------------------

func TestChallenger_Boundary_ExactAPIRoot(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	tests := []struct {
		name string
		path string
	}{
		{name: "exact /api", path: "/api"},
		{name: "exact /api/ with trailing slash", path: "/api/"},
		{name: "unhandled /api/does_not_exist", path: "/api/does_not_exist"},
		{name: "unhandled /api/session/extra", path: "/api/session/extra"},
		{name: "unhandled /api/events/child", path: "/api/events/child"},
		{name: "unhandled /api/current-match/child", path: "/api/current-match/child"},
		{name: "unhandled /api/players/sub/path/invalid", path: "/api/players/sub/path/invalid"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tc.path, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected status 404 for %s, got %d", tc.path, resp.StatusCode)
			}
			if strings.Contains(bodyStr, "<div id=\"root\"></div>") {
				t.Errorf("CRITICAL ROUTING VIOLATION: route %s erroneously fell back to index.html with body: %s", tc.path, bodyStr)
			}
		})
	}
}

func TestChallenger_Security_PathTraversalExhaustive(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	traversalPayloads := []struct {
		name string
		path string
	}{
		{name: "unix etc passwd double dot", path: "/../../etc/passwd"},
		{name: "windows win ini triple dot", path: "/../../../windows/win.ini"},
		{name: "double slash dist parent", path: "//dist/.."},
		{name: "assets escape secret text", path: "/assets/../../secret.txt"},
		{name: "assets escape to index directly", path: "/assets/../index.html"},
		{name: "url encoded slash traversal", path: "/..%2f..%2fetc/passwd"},
		{name: "url encoded dot traversal", path: "/%2e%2e/%2e%2e/etc/passwd"},
		{name: "url encoded dot and slash traversal", path: "/%2e%2e%2f%2e%2e%2fetc/passwd"},
		{name: "backslash windows traversal", path: "/..\\..\\windows\\win.ini"},
		{name: "encoded backslash traversal", path: "/..%5c..%5cwindows%5cwin.ini"},
		{name: "encoded dot and backslash traversal", path: "/%2e%2e%5c%2e%2e%5cwindows%5cwin.ini"},
		{name: "multiple double slashes with traversal", path: "//..//..//etc//passwd"},
		{name: "assets nested encoded dot traversal", path: "/assets/%2e%2e/%2e%2e/secret.txt"},
		{name: "assets nested encoded slash traversal", path: "/assets/..%2f..%2fsecret.txt"},
		{name: "double dot root", path: "/.."},
		{name: "double dot slash root", path: "/../"},
		{name: "encoded double dot root", path: "/%2e%2e"},
		{name: "encoded double dot slash root", path: "/%2e%2e/"},
		{name: "triple dot path", path: "/..."},
		{name: "dist triple dot", path: "//dist/../../"},
		{name: "deep nested traversal sequence", path: "/assets/css/../../../../../../windows/win.ini"},
	}

	for _, tc := range traversalPayloads {
		t.Run(tc.name, func(t *testing.T) {
			// Test 1: Check raw response without auto-following redirects
			tr := &http.Transport{
				DialContext: (&net.Dialer{}).DialContext,
			}
			rawClient := &http.Client{
				Transport: tr,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			req, err := http.NewRequest(http.MethodGet, server.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("request creation failed: %v", err)
			}
			rawResp, err := rawClient.Do(req)
			if err != nil {
				t.Fatalf("raw GET %s failed: %v", tc.path, err)
			}
			defer rawResp.Body.Close()

			rawBody, _ := io.ReadAll(rawResp.Body)
			rawBodyStr := string(rawBody)

			// Security Leak Check
			if strings.Contains(rawBodyStr, "root:x:") ||
				strings.Contains(rawBodyStr, "[fonts]") ||
				strings.Contains(rawBodyStr, "[extensions]") ||
				strings.Contains(rawBodyStr, "16-bit app support") {
				t.Fatalf("CRITICAL SECURITY VULNERABILITY: raw %s leaked host filesystem!", tc.path)
			}

			// Boundary Check: Must be 400 or 404 (NEVER 200 or 301/307 to a traversal target)
			if rawResp.StatusCode != http.StatusBadRequest && rawResp.StatusCode != http.StatusNotFound {
				t.Errorf("Path traversal %s: raw response was HTTP %d (Location: %q), expected 400 or 404",
					tc.path, rawResp.StatusCode, rawResp.Header.Get("Location"))
			}

			// Test 2: Standard client following redirects
			stdClient := server.Client()
			stdResp, err := stdClient.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("std GET %s failed: %v", tc.path, err)
			}
			defer stdResp.Body.Close()

			stdBody, _ := io.ReadAll(stdResp.Body)
			stdBodyStr := string(stdBody)

			// Security Leak Check
			if strings.Contains(stdBodyStr, "root:x:") ||
				strings.Contains(stdBodyStr, "[fonts]") ||
				strings.Contains(stdBodyStr, "[extensions]") ||
				strings.Contains(stdBodyStr, "16-bit app support") {
				t.Fatalf("CRITICAL SECURITY VULNERABILITY: followed %s leaked host filesystem!", tc.path)
			}

			// SPA Fallback Check: Must NEVER serve index.html for a traversal attack
			if stdResp.StatusCode == http.StatusOK && strings.Contains(stdBodyStr, "<div id=\"root\"></div>") {
				t.Errorf("Path traversal %s: standard client received HTTP 200 with index.html SPA fallback, expected 400 or 404 rejection", tc.path)
			}
		})
	}
}

func TestChallenger_MultiSlash_RedirectionPreserved(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	// Client without auto-redirects
	tr := &http.Transport{
		DialContext: (&net.Dialer{}).DialContext,
	}
	rawClient := &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	multiSlashCases := []struct {
		name             string
		path             string
		expectedLocation string
	}{
		{name: "players triple slash", path: "/players///", expectedLocation: "/players/"},
		{name: "players double slash", path: "/players//", expectedLocation: "/players/"},
		{name: "session double slash", path: "/session//", expectedLocation: "/session/"},
	}

	for _, tc := range multiSlashCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, server.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("request creation failed: %v", err)
			}
			resp, err := rawClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tc.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusMovedPermanently && resp.StatusCode != http.StatusTemporaryRedirect {
				t.Errorf("expected 301 or 307 redirect for %s, got HTTP %d", tc.path, resp.StatusCode)
			}

			loc := resp.Header.Get("Location")
			if loc != tc.expectedLocation {
				t.Errorf("expected redirect Location %q, got %q", tc.expectedLocation, loc)
			}
		})
	}
}

func TestChallenger_SPA_LegitimateRoutes(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	legitRoutes := []string{
		"/",
		"/index.html",
		"/session",
		"/session/",
		"/?mode=overlay",
		"/session?mode=overlay",
		"/settings",
		"/settings/preferences",
	}

	for _, p := range legitRoutes {
		t.Run(p, func(t *testing.T) {
			resp, err := client.Get(server.URL + p)
			if err != nil {
				t.Fatalf("GET %s failed: %v", p, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected HTTP 200 for %s, got %d", p, resp.StatusCode)
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "text/html") {
				t.Errorf("expected Content-Type containing text/html for %s, got %q", p, ct)
			}

			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "<div id=\"root\"></div>") {
				t.Errorf("expected SPA index.html containing root div for %s", p)
			}
		})
	}
}

func TestChallenger_QueryParameters_PlayerSearch(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	// Query strings with dots (like player name "Dr.Octopus" or single dot)
	validQueries := []string{
		"/api/players?query=Dr.Octopus",
		"/api/players?query=player.one",
		"/session?user=Dr.Octopus",
	}

	for _, p := range validQueries {
		t.Run(p, func(t *testing.T) {
			resp, err := client.Get(server.URL + p)
			if err != nil {
				t.Fatalf("GET %s failed: %v", p, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected HTTP 200 for valid query %s, got %d", p, resp.StatusCode)
			}
		})
	}
}

