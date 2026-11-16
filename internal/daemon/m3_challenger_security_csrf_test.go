package daemon_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// TestM3_UnrestrictedLANAndProxyOrigins verifies that mutating daemon endpoints never impose
// unrequested 403 Forbidden Origin/authz checks across LAN IPs, custom domains, reverse proxies,
// loopback ports, or native CLI clients omitting Origin.
func TestM3_UnrestrictedLANAndProxyOrigins(t *testing.T) {
	h := setupM3DaemonTest(t)

	failedGUID := "lan-failed-guid-001"
	playerID := "Steam|76561198000000001|0"
	_ = h.store.UpsertPlayer(h.ctx, &storage.PlayerRecord{
		PlayerID:    playerID,
		PlayerName:  "LANPlayer",
		Platform:    "steam",
		FirstSeenAt: time.Now().UTC(),
		LastSeenAt:  time.Now().UTC(),
	})

	origins := []string{
		"",
		"http://localhost:5173",
		"http://127.0.0.1:49125",
		"http://[::1]:5173",
		"http://192.168.1.50:49125",
		"http://10.0.0.25:3000",
		"http://rl-desktop:49125",
		"http://rl-desktop.local:49125",
		"https://rl.strmshdw.io",
	}

	t.Run("GET /sync still enforces POST method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sync", nil)
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed for GET /sync, got %d", rec.Code)
		}
	})

	t.Run("POST /sync accepts all LAN, proxy, loopback, and empty origins", func(t *testing.T) {
		for _, origin := range origins {
			req := httptest.NewRequest(http.MethodPost, "/sync", nil)
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			rec := httptest.NewRecorder()
			h.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 OK for Origin %q, got %d (body: %s)", origin, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("POST /api/matches/{guid}/retry accepts all LAN, proxy, loopback, and empty origins", func(t *testing.T) {
		retryURL := fmt.Sprintf("/api/matches/%s/retry", failedGUID)
		for _, origin := range origins {
			_ = h.store.UpsertDiscoveredMatches(h.ctx, []*storage.MatchRecord{
				{
					MatchGUID:       failedGUID,
					DownloadStatus:  storage.DownloadDownloaded,
					UploadStatus:    storage.UploadFailed,
					StatsSyncStatus: "FAILED",
					RetryCount:      2,
				},
			})
			req := httptest.NewRequest(http.MethodPost, retryURL, nil)
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			rec := httptest.NewRecorder()
			h.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 OK for Origin %q, got %d", origin, rec.Code)
			}
		}
	})

	t.Run("PATCH /api/players/{id} accepts all LAN, proxy, loopback, and empty origins", func(t *testing.T) {
		patchURL := fmt.Sprintf("/api/players/%s", playerID)
		patchBody := []byte(`{"notes":"lan note","tags":["lan"]}`)
		for _, origin := range origins {
			req := httptest.NewRequest(http.MethodPatch, patchURL, bytes.NewReader(patchBody))
			req.Header.Set("Content-Type", "application/json")
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			rec := httptest.NewRecorder()
			h.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 OK for Origin %q on PATCH, got %d", origin, rec.Code)
			}
		}
	})
}
