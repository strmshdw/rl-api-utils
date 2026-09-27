package web_test

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/web"
)

func TestDistHandler_Root(t *testing.T) {
	handler := web.DistHandler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	ct := res.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type containing 'text/html', got %q", ct)
	}

	cc := res.Header.Get("Cache-Control")
	if !strings.Contains(cc, "no-cache") {
		t.Errorf("expected Cache-Control containing 'no-cache', got %q", cc)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, "<div id=\"root\"></div>") {
		t.Errorf("expected body to contain '<div id=\"root\"></div>', got: %s", bodyStr)
	}
}

func TestDistHandler_IndexHTML(t *testing.T) {
	handler := web.DistHandler()

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	ct := res.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type containing 'text/html', got %q", ct)
	}

	cc := res.Header.Get("Cache-Control")
	if !strings.Contains(cc, "no-cache") {
		t.Errorf("expected Cache-Control containing 'no-cache', got %q", cc)
	}
}

func TestDistHandler_SPAFallbackRoutes(t *testing.T) {
	handler := web.DistHandler()

	routes := []struct {
		name string
		path string
	}{
		{name: "session route", path: "/session"},
		{name: "session with trailing slash", path: "/session/"},
		{name: "player drilldown route", path: "/players/123"},
		{name: "player with platform id", path: "/players/Epic%7Caccount_123%7C0"},
		{name: "overlay mode query parameter", path: "/?mode=overlay"},
		{name: "session with overlay parameter", path: "/session?mode=overlay"},
		{name: "deep nested route", path: "/settings/preferences/custom"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				t.Fatalf("expected status 200 for %s, got %d", tc.path, res.StatusCode)
			}

			ct := res.Header.Get("Content-Type")
			if !strings.Contains(ct, "text/html") {
				t.Errorf("expected Content-Type text/html for %s, got %q", tc.path, ct)
			}

			cc := res.Header.Get("Cache-Control")
			if !strings.Contains(cc, "no-cache") {
				t.Errorf("expected Cache-Control no-cache for %s, got %q", tc.path, cc)
			}

			body, _ := io.ReadAll(res.Body)
			if !strings.Contains(string(body), "<div id=\"root\"></div>") {
				t.Errorf("expected HTML index body for %s", tc.path)
			}
		})
	}
}

func TestDistHandler_AssetFiles(t *testing.T) {
	handler := web.DistHandler()

	// Locate asset files dynamically from the embedded filesystem
	subFS, err := web.DistFS()
	if err != nil {
		t.Fatalf("failed to get DistFS: %v", err)
	}

	entries, err := fs.ReadDir(subFS, "assets")
	if err != nil {
		t.Fatalf("failed to read assets dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no asset files found in embedded assets directory")
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		assetPath := "/assets/" + entry.Name()

		t.Run(entry.Name(), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, assetPath, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				t.Fatalf("expected status 200 for asset %s, got %d", assetPath, res.StatusCode)
			}

			// Validate Cache-Control for assets
			cc := res.Header.Get("Cache-Control")
			if !strings.Contains(cc, "max-age=") {
				t.Errorf("expected Cache-Control containing 'max-age=' for asset %s, got %q", assetPath, cc)
			}

			// Validate MIME type based on file extension
			ct := res.Header.Get("Content-Type")
			if strings.HasSuffix(entry.Name(), ".js") {
				if !strings.Contains(ct, "javascript") {
					t.Errorf("expected javascript MIME type for %s, got %q", entry.Name(), ct)
				}
			} else if strings.HasSuffix(entry.Name(), ".css") {
				if !strings.Contains(ct, "text/css") {
					t.Errorf("expected text/css MIME type for %s, got %q", entry.Name(), ct)
				}
			}

			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("failed to read asset body: %v", err)
			}
			if len(body) == 0 {
				t.Errorf("expected non-empty body for asset %s", assetPath)
			}
		})
	}
}

func TestDistHandler_APIRoutesExcluded(t *testing.T) {
	handler := web.DistHandler()

	apiPaths := []string{
		"/api",
		"/api/session",
		"/api/players",
		"/api/current-match",
		"/api/events",
		"/api/nonexistent",
	}

	for _, p := range apiPaths {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusNotFound {
				t.Errorf("expected status 404 for API path %s on DistHandler, got %d", p, res.StatusCode)
			}
		})
	}
}

func TestDistHandler_MethodNotAllowed(t *testing.T) {
	handler := web.DistHandler()

	disallowedMethods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, method := range disallowedMethods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/session", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("expected status 405 Method Not Allowed for %s, got %d", method, res.StatusCode)
			}

			allow := res.Header.Get("Allow")
			if !strings.Contains(allow, "GET") || !strings.Contains(allow, "HEAD") {
				t.Errorf("expected Allow header with GET, HEAD, got %q", allow)
			}
		})
	}
}

func TestDistHandler_HEAD(t *testing.T) {
	handler := web.DistHandler()

	t.Run("HEAD /", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.StatusCode)
		}

		body, _ := io.ReadAll(res.Body)
		if len(body) != 0 {
			t.Errorf("expected empty body for HEAD request, got %d bytes", len(body))
		}
	})

	t.Run("HEAD /session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/session", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.StatusCode)
		}

		body, _ := io.ReadAll(res.Body)
		if len(body) != 0 {
			t.Errorf("expected empty body for HEAD request, got %d bytes", len(body))
		}
	})
}
