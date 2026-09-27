package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/web"
)

// TestM4_Adversarial_DistHandler_ExactAPIRoot verifies that GET /api returns 404
// and does NOT fall back to index.html with 200 OK.
func TestM4_Adversarial_DistHandler_ExactAPIRoot(t *testing.T) {
	handler := web.DistHandler()

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	bodyStr := string(body)

	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 for /api on DistHandler, got %d", res.StatusCode)
	}
	if strings.Contains(bodyStr, "<div id=\"root\"></div>") {
		t.Errorf("route /api fell back to SPA index.html with body: %s", bodyStr)
	}
}

// TestM4_Adversarial_DistHandler_PathTraversalRejection verifies that path traversal attempts
// directly against DistHandler return 400 or 404 and do NOT serve index.html with 200 OK.
func TestM4_Adversarial_DistHandler_PathTraversalRejection(t *testing.T) {
	handler := web.DistHandler()

	traversalCases := []string{
		"/../../etc/passwd",
		"//dist/..",
		"/assets/../../secret.txt",
		"/assets/../index.html",
	}

	for _, p := range traversalCases {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			body, _ := io.ReadAll(res.Body)
			bodyStr := string(body)

			if res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusNotFound {
				t.Errorf("path traversal %s returned HTTP %d on DistHandler, expected 400 or 404", p, res.StatusCode)
			}
			if strings.Contains(bodyStr, "<div id=\"root\"></div>") {
				t.Errorf("path traversal %s erroneously fell back to index.html with 200 OK", p)
			}
		})
	}
}
