package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestStress_GetPlayer_PercentEncodedAndRawPipes(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			ctx := context.Background()
			now := time.Now().UTC()

			// Seed test players
			steamID := "Steam|76561198000000001|0"
			epicID := "Epic|000102030405060708090a0b0c0d0e0f|0"
			spaceID := "Steam|7656 1198 0002|0"

			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID: steamID, Platform: "Steam", PlayerName: "SteamPlayer1", FirstSeenAt: now, LastSeenAt: now,
			})
			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID: epicID, Platform: "Epic", PlayerName: "EpicPlayer1", FirstSeenAt: now, LastSeenAt: now,
			})
			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID: spaceID, Platform: "Steam", PlayerName: "SpacedPlayer", FirstSeenAt: now, LastSeenAt: now,
			})

			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			cases := []struct {
				name             string
				reqPath          string
				expectedPlayerID string
				expectedName     string
			}{
				{
					name:             "Steam percent encoded uppercase %7C",
					reqPath:          "/players/Steam%7C76561198000000001%7C0",
					expectedPlayerID: steamID,
					expectedName:     "SteamPlayer1",
				},
				{
					name:             "Steam percent encoded lowercase %7c",
					reqPath:          "/players/Steam%7c76561198000000001%7c0",
					expectedPlayerID: steamID,
					expectedName:     "SteamPlayer1",
				},
				{
					name:             "Steam raw unencoded pipe",
					reqPath:          "/players/Steam|76561198000000001|0",
					expectedPlayerID: steamID,
					expectedName:     "SteamPlayer1",
				},
				{
					name:             "Epic percent encoded %7C",
					reqPath:          "/players/Epic%7C000102030405060708090a0b0c0d0e0f%7C0",
					expectedPlayerID: epicID,
					expectedName:     "EpicPlayer1",
				},
				{
					name:             "Epic raw unencoded pipe",
					reqPath:          "/players/Epic|000102030405060708090a0b0c0d0e0f|0",
					expectedPlayerID: epicID,
					expectedName:     "EpicPlayer1",
				},
				{
					name:             "Player ID with encoded space %20",
					reqPath:          "/players/Steam%7C7656%201198%200002%7C0",
					expectedPlayerID: spaceID,
					expectedName:     "SpacedPlayer",
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					rec := executeTestRequest(d, http.MethodGet, tc.reqPath, nil)
					if rec.Code != http.StatusOK {
						t.Fatalf("expected 200 OK for %s, got %d: %s", tc.reqPath, rec.Code, rec.Body.String())
					}

					var resp daemon.PlayerDetailResponse
					if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
						t.Fatalf("failed to decode JSON: %v", err)
					}

					if resp.PlayerID != tc.expectedPlayerID {
						t.Errorf("expected player ID %q, got %q", tc.expectedPlayerID, resp.PlayerID)
					}
					if resp.PlayerName != tc.expectedName {
						t.Errorf("expected player name %q, got %q", tc.expectedName, resp.PlayerName)
					}
					if resp.Matchups == nil {
						t.Errorf("expected non-nil matchups slice")
					}
				})
			}
		})
	}
}

// TestStress_GetPlayer_MalformedPercentEscapes_Returns400 verifies that malformed percent
// escapes (e.g., %, %2, %ZZ, %G1, trailing %) return HTTP 400 Bad Request.
// Tested both via in-memory handler execution and over actual TCP wire connection.
func TestStress_GetPlayer_MalformedPercentEscapes_Returns400(t *testing.T) {
	store := newTestStore(t, backendSQLite)
	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	malformedIDs := []string{
		"%",
		"%2",
		"%ZZ",
		"%G1",
		"Steam%",
		"Steam%7",
		"Steam%2Z",
		"Steam%ZZ%7C0",
	}

	handler := d.Handler(context.Background())

	for _, malformed := range malformedIDs {
		t.Run("Handler_"+malformed, func(t *testing.T) {
			// Construct request directly with malformed path without triggering url.Parse errors
			req, err := http.NewRequest(http.MethodGet, "http://localhost", nil)
			if err != nil {
				t.Fatalf("failed to construct request: %v", err)
			}
			req.URL.Path = "/players/" + malformed

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request for %q, got %d: %s", malformed, rec.Code, rec.Body.String())
			}

			var errResp map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("failed to decode error JSON: %v", err)
			}
			if errResp["error"] == "" {
				t.Errorf("expected non-empty error message in response")
			}
		})
	}

	// Live TCP Wire Verification: spin up httptest.Server and send raw HTTP bytes over net.Dial
	srv := httptest.NewServer(handler)
	defer srv.Close()

	srvURL, _ := url.Parse(srv.URL)
	hostPort := srvURL.Host

	for _, malformed := range malformedIDs {
		t.Run("WireTCP_"+malformed, func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", hostPort, 1*time.Second)
			if err != nil {
				t.Fatalf("failed to connect to server: %v", err)
			}
			defer conn.Close()

			rawRequest := fmt.Sprintf("GET /players/%s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", malformed, hostPort)
			if _, err := io.WriteString(conn, rawRequest); err != nil {
				t.Fatalf("failed to write raw request: %v", err)
			}

			respReader := bufio.NewReader(conn)
			resp, err := http.ReadResponse(respReader, nil)
			if err != nil {
				t.Fatalf("failed to read response: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				bodyBytes, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 400 Bad Request on wire for %q, got %d: %s", malformed, resp.StatusCode, string(bodyBytes))
			}
		})
	}
}

// TestStress_GetPlayer_NotFound_And_EmptyPath asserts 404 Not Found for nonexistent IDs,
// empty path, and whitespace-only IDs.
func TestStress_GetPlayer_NotFound_And_EmptyPath(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			cases := []struct {
				name    string
				path    string
				expCode int
			}{
				{
					name:    "Nonexistent Steam ID",
					path:    "/players/Steam%7C99999999999999999%7C0",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Nonexistent Epic ID",
					path:    "/players/Epic%7Cnonexistent_acc_guid%7C0",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Empty ID after slash",
					path:    "/players/",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Multiple slashes cleaned by ServeMux (301 Redirect)",
					path:    "/players///",
					expCode: http.StatusMovedPermanently,
				},
				{
					name:    "Encoded slash (%2F) unescapes to slash then trimmed to empty",
					path:    "/players/%2F",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Whitespace only encoded (%20)",
					path:    "/players/%20",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Tab character encoded (%09)",
					path:    "/players/%09",
					expCode: http.StatusNotFound,
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					rec := executeTestRequest(d, http.MethodGet, tc.path, nil)
					if tc.path == "/players///" {
						if rec.Code != http.StatusMovedPermanently && rec.Code != http.StatusTemporaryRedirect {
							t.Fatalf("expected status 301 or 307 for %s, got %d: %s", tc.path, rec.Code, rec.Body.String())
						}
					} else if rec.Code != tc.expCode {
						t.Fatalf("expected status %d for %s, got %d: %s", tc.expCode, tc.path, rec.Code, rec.Body.String())
					}
					if tc.expCode == http.StatusNotFound {
						var errResp map[string]string
						_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
						if errResp["error"] != "player not found" {
							t.Errorf("expected error 'player not found', got %q", errResp["error"])
						}
					} else if tc.expCode == http.StatusMovedPermanently || rec.Code == http.StatusTemporaryRedirect {
						loc := rec.Header().Get("Location")
						if loc != "/players/" {
							t.Errorf("expected redirect location '/players/', got %q", loc)
						}
					}
				})
			}
		})
	}
}

// TestStress_GetPlayer_DualBackend_FullParity verifies that detailed player records
// with multi-playlist matchup histories are returned identically across SQLite and JSONStore.
func TestStress_GetPlayer_DualBackend_FullParity(t *testing.T) {
	ctx := context.Background()
	sqliteStore := newTestStore(t, backendSQLite)
	jsonStore := newTestStore(t, backendJSON)

	now := time.Now().UTC().Truncate(time.Second)
	targetID := "Steam|76561198099887766|0"

	record := &storage.PlayerRecord{
		PlayerID:    targetID,
		Platform:    "Steam",
		PlayerName:  "ParityMaster",
		RanksJSON:   `{"10":{"tier":12,"division":1},"11":{"tier":16,"division":2},"13":{"tier":17,"division":0}}`,
		FirstSeenAt: now.Add(-10 * time.Hour),
		LastSeenAt:  now,
	}

	_ = sqliteStore.UpsertPlayer(ctx, record)
	_ = jsonStore.UpsertPlayer(ctx, record)

	// Seed matchups across 3 playlists: 10 (1v1), 11 (2v2), 13 (3v3)
	matchupsToSeed := []struct {
		playlist   int
		isTeammate bool
		won        bool
		count      int
	}{
		{playlist: 10, isTeammate: false, won: true, count: 4},
		{playlist: 10, isTeammate: false, won: false, count: 2},
		{playlist: 11, isTeammate: true, won: true, count: 8},
		{playlist: 11, isTeammate: true, won: false, count: 3},
		{playlist: 11, isTeammate: false, won: true, count: 2},
		{playlist: 13, isTeammate: true, won: false, count: 5},
	}

	for i, m := range matchupsToSeed {
		for c := 0; c < m.count; c++ {
			guid := fmt.Sprintf("guid-m-%d-%d", i, c)
			outcomes := []storage.PlayerOutcome{
				{PlayerID: targetID, Platform: "Steam", PlayerName: "ParityMaster", IsTeammate: m.isTeammate, Won: m.won},
			}
			_ = sqliteStore.RecordMatchResults(ctx, guid, m.playlist, outcomes)
			_ = jsonStore.RecordMatchResults(ctx, guid, m.playlist, outcomes)
		}
	}

	cfg := makeTestConfig(5*time.Minute, false)
	dSQL, _ := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(sqliteStore))
	dJSON, _ := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(jsonStore))

	reqPath := "/players/Steam%7C76561198099887766%7C0"
	recSQL := executeTestRequest(dSQL, http.MethodGet, reqPath, nil)
	recJSON := executeTestRequest(dJSON, http.MethodGet, reqPath, nil)

	if recSQL.Code != http.StatusOK || recJSON.Code != http.StatusOK {
		t.Fatalf("expected 200 OK: SQLite=%d, JSON=%d", recSQL.Code, recJSON.Code)
	}

	var respSQL daemon.PlayerDetailResponse
	var respJSON daemon.PlayerDetailResponse

	if err := json.Unmarshal(recSQL.Body.Bytes(), &respSQL); err != nil {
		t.Fatalf("failed to decode SQL: %v", err)
	}
	if err := json.Unmarshal(recJSON.Body.Bytes(), &respJSON); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	// Compare player record fields
	if respSQL.PlayerID != respJSON.PlayerID ||
		respSQL.Platform != respJSON.Platform ||
		respSQL.PlayerName != respJSON.PlayerName ||
		respSQL.RanksJSON != respJSON.RanksJSON {
		t.Fatalf("player record mismatch:\nSQL:  %+v\nJSON: %+v", respSQL.PlayerRecord, respJSON.PlayerRecord)
	}

	// Compare matchups
	if len(respSQL.Matchups) != len(respJSON.Matchups) {
		t.Fatalf("matchup count mismatch: SQL=%d, JSON=%d", len(respSQL.Matchups), len(respJSON.Matchups))
	}
	if len(respSQL.Matchups) != 3 {
		t.Fatalf("expected 3 playlist matchups, got %d", len(respSQL.Matchups))
	}

	for i := 0; i < len(respSQL.Matchups); i++ {
		sm := respSQL.Matchups[i]
		jm := respJSON.Matchups[i]

		if sm.PlaylistID != jm.PlaylistID ||
			sm.WinsAsTeammate != jm.WinsAsTeammate ||
			sm.LossesAsTeammate != jm.LossesAsTeammate ||
			sm.WinsAsOpponent != jm.WinsAsOpponent ||
			sm.LossesAsOpponent != jm.LossesAsOpponent ||
			sm.TotalMatches != jm.TotalMatches {
			t.Fatalf("matchup mismatch at index %d:\nSQL:  %+v\nJSON: %+v", i, sm, jm)
		}
	}
}

// -----------------------------------------------------------------------------
// 4. LIVE HTTP SERVER FULL SYSTEM CONCURRENT STRESS TEST
// -----------------------------------------------------------------------------

// TestStress_FullHTTPAPI_HighConcurrency exercises all query endpoints concurrently
// under load across 50 worker goroutines over live HTTP connections.
