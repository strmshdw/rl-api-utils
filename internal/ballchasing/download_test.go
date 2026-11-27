package ballchasing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newFastTestClient(t *testing.T, baseURL string, maxRetries int) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{
		BaseURL:     baseURL,
		APIKey:      "test-secret-key",
		MaxRetries:  maxRetries,
		Timeout:     5 * time.Second,
		BaseBackoff: 5 * time.Millisecond,
		MaxBackoff:  20 * time.Millisecond,
		RateLimiter: NewRateLimiter(time.Millisecond),
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return c
}

func TestDownloadReplayFile_SuccessAndAtomicCleanup(t *testing.T) {
	payload := append([]byte{0x10, 0x04, 0x00, 0x00}, bytes.Repeat([]byte("RLREPLAY"), 256)...)
	var gotAuth string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := newFastTestClient(t, srv.URL, 1)
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "nested", "bc-replay-123.replay")

	n, err := c.DownloadReplayFile(context.Background(), "bc-replay-123", destPath)
	if err != nil {
		t.Fatalf("DownloadReplayFile failed: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("written bytes = %d, want %d", n, len(payload))
	}
	if gotAuth != "test-secret-key" {
		t.Errorf("Authorization header = %q, want raw key without Bearer", gotAuth)
	}
	if gotPath != "/replays/bc-replay-123/file" {
		t.Errorf("request path = %q, want /replays/bc-replay-123/file", gotPath)
	}

	gotBytes, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(gotBytes, payload) {
		t.Fatalf("downloaded bytes mismatch")
	}

	entries, err := os.ReadDir(filepath.Dir(destPath))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "bc-replay-123.replay" {
		t.Errorf("expected only final file in cache dir, got %d entries", len(entries))
	}
}

func TestDownloadReplayFile_StatusErrorsAndRateLimit(t *testing.T) {
	t.Run("401 unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		_, err := c.DownloadReplayFile(context.Background(), "rep-401", filepath.Join(t.TempDir(), "a.replay"))
		if !errors.Is(err, ErrInvalidAPIKey) {
			t.Fatalf("got err %v, want ErrInvalidAPIKey", err)
		}
	})

	t.Run("403 forbidden", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		_, err := c.DownloadReplayFile(context.Background(), "rep-403", filepath.Join(t.TempDir(), "a.replay"))
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("got err %v, want ErrForbidden", err)
		}
	})

	t.Run("404 not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		_, err := c.DownloadReplayFile(context.Background(), "rep-404", filepath.Join(t.TempDir(), "a.replay"))
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("got err %v, want ErrNotFound", err)
		}
	})

	t.Run("429 exhausted preserves RetryAfter", func(t *testing.T) {
		var attempts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.Header().Set("Retry-After", "12")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
		}))
		defer srv.Close()

		c, err := NewClient(ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      "test-key",
			MaxRetries:  1,
			BaseBackoff: 5 * time.Millisecond,
			MaxBackoff:  15 * time.Second,
			RateLimiter: NewRateLimiter(time.Millisecond),
		})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		// Override maxRetries=0 so test runs immediately without 12s sleep while still parsing Retry-After
		c.maxRetries = 0

		_, err = c.DownloadReplayFile(context.Background(), "rep-429", filepath.Join(t.TempDir(), "a.replay"))
		if !errors.Is(err, ErrRateLimitExhausted) {
			t.Fatalf("got err %v, want ErrRateLimitExhausted", err)
		}
		var rlErr *RateLimitError
		if !errors.As(err, &rlErr) {
			t.Fatalf("expected *RateLimitError, got %T", err)
		}
		if rlErr.RetryAfter != 12*time.Second {
			t.Errorf("RetryAfter = %v, want 12s", rlErr.RetryAfter)
		}
		if attempts.Load() != 1 {
			t.Errorf("attempts = %d, want 1", attempts.Load())
		}
	})

	t.Run("429 retries then succeeds", func(t *testing.T) {
		var attempts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if attempts.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("VALID_REPLAY_BYTES"))
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 2)
		dest := filepath.Join(t.TempDir(), "retry.replay")
		n, err := c.DownloadReplayFile(context.Background(), "rep-retry", dest)
		if err != nil {
			t.Fatalf("expected retry success, got %v", err)
		}
		if n != int64(len("VALID_REPLAY_BYTES")) {
			t.Errorf("written = %d, want %d", n, len("VALID_REPLAY_BYTES"))
		}
	})

	t.Run("500 exhausted returns ErrServerError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 1)
		_, err := c.DownloadReplayFile(context.Background(), "rep-500", filepath.Join(t.TempDir(), "a.replay"))
		if !errors.Is(err, ErrServerError) {
			t.Fatalf("got err %v, want ErrServerError", err)
		}
	})
}

func TestDownloadReplayFile_RejectsInvalidAndEmptyPayloads(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        []byte
		wantErr     error
	}{
		{
			name:        "HTML content-type with 200",
			contentType: "text/html; charset=utf-8",
			body:        []byte("<html><body>502 Bad Gateway</body></html>"),
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "HTML body with octet-stream content-type",
			contentType: "application/octet-stream",
			body:        []byte("  <!DOCTYPE html><html><body>Cloudflare error</body></html>"),
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "JSON error body with 200",
			contentType: "application/octet-stream",
			body:        []byte(`{"error":"replay expired"}`),
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "application/json content-type with 200",
			contentType: "application/json; charset=utf-8",
			body:        []byte(`{"message":"maintenance"}`),
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "text/plain content-type with 200",
			contentType: "text/plain; charset=utf-8",
			body:        []byte("upstream temporarily unavailable"),
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "JSON message body with octet-stream content-type",
			contentType: "application/octet-stream",
			body:        []byte(`  {"message":"not ready yet"}`),
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "JSON array body with octet-stream content-type",
			contentType: "application/octet-stream",
			body:        []byte(`["err"]`),
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "XML/tag error body with octet-stream content-type",
			contentType: "application/octet-stream",
			body:        []byte(`<Error><Code>AccessDenied</Code></Error>`),
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "Empty 0-byte payload",
			contentType: "application/octet-stream",
			body:        nil,
			wantErr:     ErrEmptyFile,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()

			c := newFastTestClient(t, srv.URL, 1)
			dir := t.TempDir()
			dest := filepath.Join(dir, "bad.replay")
			_, err := c.DownloadReplayFile(context.Background(), "rep-bad", dest)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got err %v, want %v", err, tc.wantErr)
			}
			if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
				t.Errorf("dest file should not exist on invalid payload")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Errorf("temp files leaked in dir: %d entries remain", len(entries))
			}
		})
	}

	t.Run("accepts binary replay whose uint32 header size low byte equals '{'", func(t *testing.T) {
		// header_size = 0x00001A7B -> leading byte 0x7B ('{') followed by binary NULs in bytes 2..3
		binaryReplay := append([]byte{0x7B, 0x1A, 0x00, 0x00, 0xDE, 0xAD, 0xBE, 0xEF}, bytes.Repeat([]byte{0x01, 0x00}, 64)...)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(binaryReplay)
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		dest := filepath.Join(t.TempDir(), "header7b.replay")
		n, err := c.DownloadReplayFile(context.Background(), "rep-7b", dest)
		if err != nil {
			t.Fatalf("binary replay starting with 0x7B should succeed, got %v", err)
		}
		if n != int64(len(binaryReplay)) {
			t.Fatalf("written = %d, want %d", n, len(binaryReplay))
		}
	})
}

func TestFindReplayByMetadata(t *testing.T) {
	baseTime := time.Date(2026, 11, 20, 18, 30, 0, 0, time.UTC)

	t.Run("exact MatchGUID match in list", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/replays") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ReplayListResponse{
				Count: 2,
				List: []ReplayItem{
					{ID: "bc-other", MatchGUID: "OTHER-GUID"},
					{ID: "bc-exact", MatchGUID: "AABB-CCDD-1122", Link: "https://ballchasing.com/replay/bc-exact"},
				},
			})
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		found, err := c.FindReplayByMetadata(context.Background(), ReplaySearchQuery{
			MatchGUID: "aabbccdd1122",
			Timestamp: baseTime,
		})
		if err != nil {
			t.Fatalf("FindReplayByMetadata failed: %v", err)
		}
		if found.ID != "bc-exact" {
			t.Errorf("got ID %q, want bc-exact", found.ID)
		}
	})

	t.Run("metadata scoring with GetReplay GUID confirmation and disqualification", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/replays":
				_ = json.NewEncoder(w).Encode(ReplayListResponse{
					Count: 2,
					List: []ReplayItem{
						{
							ID:         "bc-wrong-guid",
							Date:       baseTime.Add(30 * time.Second).Format(time.RFC3339),
							PlaylistID: "ranked-doubles",
							Blue:       ReplayTeam{Goals: 3, Players: []ReplayPlayer{{Name: "Alice"}}},
							Orange:     ReplayTeam{Goals: 2},
						},
						{
							ID:         "bc-right-guid",
							Date:       baseTime.Add(90 * time.Second).Format(time.RFC3339),
							PlaylistID: "ranked-doubles",
							Blue:       ReplayTeam{Goals: 3, Players: []ReplayPlayer{{Name: "Alice"}}},
							Orange:     ReplayTeam{Goals: 2},
						},
					},
				})
			case "/replays/bc-wrong-guid":
				_ = json.NewEncoder(w).Encode(ReplayItem{ID: "bc-wrong-guid", MatchGUID: "DIFFERENT-MATCH"})
			case "/replays/bc-right-guid":
				_ = json.NewEncoder(w).Encode(ReplayItem{ID: "bc-right-guid", MatchGUID: "TARGET-MATCH-99"})
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		found, err := c.FindReplayByMetadata(context.Background(), ReplaySearchQuery{
			MatchGUID:   "target-match-99",
			Timestamp:   baseTime,
			PlaylistID:  11,
			BlueScore:   3,
			OrangeScore: 2,
			PlayerNames: []string{"Alice"},
		})
		if err != nil {
			t.Fatalf("FindReplayByMetadata failed: %v", err)
		}
		if found.ID != "bc-right-guid" {
			t.Errorf("got ID %q, want bc-right-guid", found.ID)
		}
	})

	t.Run("not found when empty list", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ReplayListResponse{Count: 0, List: []ReplayItem{}})
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		_, err := c.FindReplayByMetadata(context.Background(), ReplaySearchQuery{MatchGUID: "missing"})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("got err %v, want ErrNotFound", err)
		}
	})

	t.Run("matches normalized RocketLeagueID in list and detail", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ReplayListResponse{
				Count: 2,
				List: []ReplayItem{
					{ID: "bc-other", RocketLeagueID: "OTHER-RL-ID"},
					{ID: "bc-rl-hit", RocketLeagueID: "DEAD-BEEF-4242", Link: "https://ballchasing.com/replay/bc-rl-hit"},
				},
			})
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		found, err := c.FindReplayByMetadata(context.Background(), ReplaySearchQuery{
			MatchGUID: "deadbeef4242",
			Timestamp: baseTime,
		})
		if err != nil {
			t.Fatalf("FindReplayByMetadata failed: %v", err)
		}
		if found.ID != "bc-rl-hit" {
			t.Errorf("got ID %q, want bc-rl-hit", found.ID)
		}
	})

	t.Run("matches RocketLeagueID during GetReplay detail verification", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/replays":
				_ = json.NewEncoder(w).Encode(ReplayListResponse{
					Count: 1,
					List: []ReplayItem{
						{
							ID:         "bc-detail-rlid",
							Date:       baseTime.Add(30 * time.Second).Format(time.RFC3339),
							PlaylistID: "ranked-doubles",
							Blue:       ReplayTeam{Goals: 2},
							Orange:     ReplayTeam{Goals: 1},
						},
					},
				})
			case "/replays/bc-detail-rlid":
				_ = json.NewEncoder(w).Encode(ReplayItem{
					ID:             "bc-detail-rlid",
					RocketLeagueID: "RLID-DETAIL-99",
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		found, err := c.FindReplayByMetadata(context.Background(), ReplaySearchQuery{
			MatchGUID:   "rliddetail99",
			Timestamp:   baseTime,
			PlaylistID:  11,
			BlueScore:   2,
			OrangeScore: 1,
		})
		if err != nil {
			t.Fatalf("FindReplayByMetadata failed: %v", err)
		}
		if found.ID != "bc-detail-rlid" {
			t.Errorf("got ID %q, want bc-detail-rlid", found.ID)
		}
	})

	t.Run("surfaces 403 ErrForbidden and 429 RateLimitError with RetryAfter", func(t *testing.T) {
		srv403 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"forbidden"}`))
		}))
		defer srv403.Close()

		c403 := newFastTestClient(t, srv403.URL, 0)
		_, err := c403.FindReplayByMetadata(context.Background(), ReplaySearchQuery{MatchGUID: "g1"})
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("403 search err = %v, want ErrForbidden", err)
		}

		srv429 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "14")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
		}))
		defer srv429.Close()

		c429, _ := NewClient(ClientConfig{
			BaseURL:     srv429.URL,
			APIKey:      "key",
			MaxRetries:  0,
			BaseBackoff: 5 * time.Millisecond,
			MaxBackoff:  20 * time.Second,
			RateLimiter: NewRateLimiter(time.Millisecond),
		})
		_, err = c429.FindReplayByMetadata(context.Background(), ReplaySearchQuery{MatchGUID: "g2"})
		if !errors.Is(err, ErrRateLimitExhausted) {
			t.Fatalf("429 search err = %v, want ErrRateLimitExhausted", err)
		}
		var rlErr *RateLimitError
		if !errors.As(err, &rlErr) || rlErr.RetryAfter != 14*time.Second {
			t.Fatalf("expected *RateLimitError with RetryAfter=14s, got %#v", err)
		}
	})

	t.Run("secondary player-name fallback finds teammate-uploaded replay", func(t *testing.T) {
		var sawUploaderMe bool
		var sawPlayerSearch bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/replays":
				q := r.URL.Query()
				if q.Get("uploader") == "me" {
					sawUploaderMe = true
					_ = json.NewEncoder(w).Encode(ReplayListResponse{Count: 0, List: []ReplayItem{}})
					return
				}
				if q.Get("player-name") == "TeammatePro" && q.Get("uploader") == "" {
					sawPlayerSearch = true
					_ = json.NewEncoder(w).Encode(ReplayListResponse{
						Count: 1,
						List: []ReplayItem{
							{
								ID:         "bc-teammate-upload",
								Date:       baseTime.Add(20 * time.Second).Format(time.RFC3339),
								PlaylistID: "ranked-doubles",
								Blue:       ReplayTeam{Goals: 3, Players: []ReplayPlayer{{Name: "TeammatePro"}}},
								Orange:     ReplayTeam{Goals: 1},
							},
						},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(ReplayListResponse{Count: 0, List: []ReplayItem{}})
			case "/replays/bc-teammate-upload":
				_ = json.NewEncoder(w).Encode(ReplayItem{
					ID:        "bc-teammate-upload",
					MatchGUID: "TEAMMATE-MATCH-GUID",
					Link:      "https://ballchasing.com/replay/bc-teammate-upload",
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		c := newFastTestClient(t, srv.URL, 0)
		found, err := c.FindReplayByMetadata(context.Background(), ReplaySearchQuery{
			MatchGUID:   "teammate-match-guid",
			Timestamp:   baseTime,
			PlaylistID:  11,
			BlueScore:   3,
			OrangeScore: 1,
			PlayerNames: []string{"TeammatePro"},
		})
		if err != nil {
			t.Fatalf("secondary teammate lookup failed: %v", err)
		}
		if !sawUploaderMe || !sawPlayerSearch {
			t.Errorf("expected uploader=me (%v) followed by player-name search (%v)", sawUploaderMe, sawPlayerSearch)
		}
		if found.ID != "bc-teammate-upload" {
			t.Errorf("got ID %q, want bc-teammate-upload", found.ID)
		}
	})
}
