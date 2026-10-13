package ballchasing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_GetUploadedReplays_PaginationAndRateLimit(t *testing.T) {
	reqCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		auth := r.Header.Get("Authorization")
		if auth != "test-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if reqCount == 2 {
			// Trigger a 429 once to test rate limit handling
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}

		resp := ReplayListResponse{
			Count: 1,
			List: []ReplayItem{
				{
					ID:         "test-replay-1",
					MatchGUID:  "guid-bc-1",
					MapName:    "DFH Stadium",
					PlaylistID: "ranked-doubles",
					Duration:   300,
					Blue: ReplayTeam{
						Goals: 3,
						Players: []ReplayPlayer{
							{
								Name:  "TestPlayer",
								Score: 400,
								ID:    PlayerIdentity{Platform: "epic", ID: "epic123"},
							},
						},
					},
					Orange: ReplayTeam{
						Goals: 1,
					},
				},
			},
			Next: "",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		APIKey:      "test-api-key",
		MaxRetries:  3,
		BaseBackoff: 10 * time.Millisecond,
		MaxBackoff:  50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// First page
	res1, err := client.GetUploadedReplays(context.Background(), "")
	if err != nil {
		t.Fatalf("GetUploadedReplays failed: %v", err)
	}
	if res1.Count != 1 || len(res1.List) != 1 {
		t.Errorf("unexpected list response: %+v", res1)
	}
	if res1.List[0].ID != "test-replay-1" {
		t.Errorf("expected test-replay-1, got %s", res1.List[0].ID)
	}

	// Second request tests the 429 retry
	res2, err := client.GetUploadedReplays(context.Background(), server.URL+"/replays?uploader=me&after=page2")
	if err != nil {
		t.Fatalf("GetUploadedReplays with 429 retry failed: %v", err)
	}
	if res2.Count != 1 {
		t.Errorf("expected count 1 on retry, got %d", res2.Count)
	}
}

func TestMapPlaylistSlugToID(t *testing.T) {
	cases := []struct {
		slug     string
		expected int
	}{
		{"ranked-doubles", 11},
		{"ranked-standard", 13},
		{"ranked-duels", 10},
		{"ranked-hoops", 27},
		{"tournament", 34},
		{"casual-doubles", 2},
		{"unknown-mode", 0},
	}
	for _, c := range cases {
		got := MapPlaylistSlugToID(c.slug)
		if got != c.expected {
			t.Errorf("MapPlaylistSlugToID(%q) = %d, expected %d", c.slug, got, c.expected)
		}
	}
}
