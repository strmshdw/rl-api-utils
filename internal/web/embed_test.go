package web_test

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

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

func TestDistHandler_MissingAssetsReturn404(t *testing.T) {
	handler := web.DistHandler()

	missingPaths := []string{
		"/assets/nonexistent-hash.js",
		"/assets/nonexistent-hash.css",
		"/assets/missing.woff2",
		"/assets/",
		"/missing-script.js",
		"/missing-style.css",
		"/missing-icon.svg",
	}

	for _, p := range missingPaths {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusNotFound {
				t.Errorf("expected status 404 for missing asset %s, got %d", p, res.StatusCode)
			}
			body, _ := io.ReadAll(res.Body)
			if strings.Contains(string(body), "<div id=\"root\"></div>") {
				t.Errorf("missing asset %s erroneously fell back to index.html", p)
			}
		})
	}
}

func TestViewerSDK_MIMETypesAndStaticServing(t *testing.T) {
	expectedMIMEs := map[string]string{
		".wasm":     "application/wasm",
		".glb":      "model/gltf-binary",
		".gltf":     "model/gltf+json",
		".bin":      "application/octet-stream",
		".webp":     "image/webp",
		".avif":     "image/avif",
		".ktx2":     "image/ktx2",
		".dds":      "application/octet-stream",
		".env":      "application/octet-stream",
		".hdr":      "application/octet-stream",
		".replay":   "application/octet-stream",
		".rlbundle": "application/octet-stream",
		".map":      "application/json",
	}
	for ext, wantPrefix := range expectedMIMEs {
		got := mime.TypeByExtension(ext)
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("mime.TypeByExtension(%q) = %q, want prefix %q", ext, got, wantPrefix)
		}
	}

	mockDist := fstest.MapFS{
		"index.html":                             &fstest.MapFile{Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/app-abc123.js":                   &fstest.MapFile{Data: []byte(`console.log("ok");`)},
		"viewer-sdk/wasm/rl_boxcars_v4.wasm":     &fstest.MapFile{Data: []byte("\x00asm\x01\x00\x00\x00")},
		"viewer-sdk/assets/models/octane.glb":    &fstest.MapFile{Data: []byte("glTF\x02\x00\x00\x00")},
		"viewer-sdk/assets/draco/draco_dec.wasm": &fstest.MapFile{Data: []byte("\x00asm\x01\x00\x00\x00")},
	}
	handler := web.NewStaticHandler(mockDist)

	cases := []struct {
		path          string
		wantStatus    int
		wantCTPrefix  string
		wantCC        string
		disallowImmut bool
	}{
		{
			path:          "/viewer-sdk/wasm/rl_boxcars_v4.wasm",
			wantStatus:    http.StatusOK,
			wantCTPrefix:  "application/wasm",
			wantCC:        "public, max-age=3600",
			disallowImmut: true,
		},
		{
			path:          "/viewer-sdk/assets/models/octane.glb",
			wantStatus:    http.StatusOK,
			wantCTPrefix:  "model/gltf-binary",
			wantCC:        "public, max-age=3600",
			disallowImmut: true,
		},
		{
			path:          "/viewer-sdk/assets/draco/draco_dec.wasm",
			wantStatus:    http.StatusOK,
			wantCTPrefix:  "application/wasm",
			wantCC:        "public, max-age=3600",
			disallowImmut: true,
		},
		{
			path:         "/assets/app-abc123.js",
			wantStatus:   http.StatusOK,
			wantCTPrefix: "application/javascript",
			wantCC:       "public, max-age=31536000, immutable",
		},
		{
			path:       "/viewer-sdk/wasm/missing.wasm",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tc.wantStatus {
				t.Fatalf("GET %s status = %d, want %d", tc.path, res.StatusCode, tc.wantStatus)
			}
			if res.Header.Get("Cross-Origin-Opener-Policy") != "" || res.Header.Get("Cross-Origin-Embedder-Policy") != "" {
				t.Errorf("unexpected COOP/COEP headers on %s", tc.path)
			}
			if tc.wantStatus != http.StatusOK {
				body, _ := io.ReadAll(res.Body)
				if strings.Contains(string(body), `<div id="root"></div>`) {
					t.Errorf("missing SDK asset %s fell back to index.html", tc.path)
				}
				return
			}
			if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, tc.wantCTPrefix) {
				t.Errorf("Content-Type for %s = %q, want prefix %q", tc.path, ct, tc.wantCTPrefix)
			}
			if cc := res.Header.Get("Cache-Control"); cc != tc.wantCC {
				t.Errorf("Cache-Control for %s = %q, want %q", tc.path, cc, tc.wantCC)
			}
			if tc.disallowImmut && strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
				t.Errorf("unhashed SDK asset %s should not be marked immutable", tc.path)
			}
		})
	}
}


