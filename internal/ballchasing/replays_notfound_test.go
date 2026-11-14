package ballchasing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetReplay_WrapsErrNotFoundOn404(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "replay not found"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL,
		APIKey:      "token",
		MaxRetries:  3,
		RateLimiter: NewRateLimiter(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = client.GetReplay(context.Background(), "expired-replay-123")
	if err == nil {
		t.Fatal("expected error on HTTP 404, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected error to wrap ErrNotFound, got: %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected 404 to fail immediately without retrying, got %d attempts", got)
	}
}

func TestGetUploadedReplays_WrapsErrNotFoundOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "page cursor expired"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL,
		APIKey:      "token",
		RateLimiter: NewRateLimiter(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = client.GetUploadedReplays(context.Background(), "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected error to wrap ErrNotFound, got: %v", err)
	}
}

func TestClient_PingInfo_And_RateLimiterDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "good-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(PingResponse{
			Chaser:  true,
			Name:    "Tester",
			SteamID: "76561198000000001",
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL: srv.URL,
		APIKey:  "good-key",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.RateLimiter() == nil || client.RateLimiter().Interval() != DefaultRateInterval {
		t.Fatalf("expected default 500ms RateLimiter on client")
	}

	info, err := client.PingInfo(context.Background())
	if err != nil || info == nil || !info.Chaser || info.Name != "Tester" {
		t.Fatalf("unexpected PingInfo result: %+v, err=%v", info, err)
	}
}
