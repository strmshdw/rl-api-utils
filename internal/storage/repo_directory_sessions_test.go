package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
)

func TestRepoDirectory_SearchWildcardsBulkParityAndSorting(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	// Seed 260 players (> bulkPlayerIDFilterMax = 200) + special wildcard/unicode/platform names.
	specialPlayers := []*PlayerRecord{
		{PlayerID: "PS4|p_pct|0", Platform: "PS4", PlayerName: "100%_Win\\Guy", RanksJSON: `{"11":{"tier":18,"division":2,"mmr":1510}}`, FirstSeenAt: now, LastSeenAt: now.Add(-time.Minute)},
		{PlayerID: "Epic|p_duo|0", Platform: "Epic", PlayerName: "SynergyPartner", RanksJSON: `{"11":{"tier":20,"division":1,"mmr":1720}}`, FirstSeenAt: now, LastSeenAt: now},
		{PlayerID: "Steam|p_uni|0", Platform: "Steam", PlayerName: "Café☕Player", RanksJSON: `{"11":{"tier":12,"division":3,"mmr":950}}`, FirstSeenAt: now, LastSeenAt: now.Add(-2 * time.Minute)},
	}
	for _, p := range specialPlayers {
		if err := store.UpsertPlayer(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 260; i++ {
		pid := fmt.Sprintf("Steam|bulk_%03d|0", i)
		_ = store.UpsertPlayer(ctx, &PlayerRecord{
			PlayerID: pid, Platform: "Steam", PlayerName: fmt.Sprintf("BulkPlayer_%03d", i),
			FirstSeenAt: now, LastSeenAt: now.Add(-time.Duration(i+10) * time.Minute),
		})
		_ = store.RecordMatchResults(ctx, fmt.Sprintf("bulk-g-%03d", i), 11, []PlayerOutcome{
			{PlayerID: pid, Platform: "Steam", PlayerName: fmt.Sprintf("BulkPlayer_%03d", i), IsTeammate: i%2 == 0, Won: i%3 != 0},
		}, now.Add(-time.Duration(i+10)*time.Minute))
	}

	// PlayStation alias match + SQL wildcard escaping (must match literally, not arbitrary chars).
	res, total, err := store.SearchPlayerSummaries(ctx, "100%_", "playstation", 20, 0)
	if err != nil || total != 1 || len(res) != 1 || res[0].PlayerID != "PS4|p_pct|0" {
		t.Fatalf("wildcard search failed: (%d, %+v, %v)", total, res, err)
	}
	if res, total, err = store.SearchPlayerSummaries(ctx, `win\guy`, "psynet", 20, 0); err != nil || total != 1 {
		t.Fatalf("backslash escape search failed: (%d, %v)", total, err)
	}
	// SQLi string must not inject or error.
	if res, total, err = store.SearchPlayerSummaries(ctx, "'; DROP TABLE players; --", "all", 20, 0); err != nil || total != 0 || len(res) != 0 {
		t.Fatalf("SQLi search failed: (%d, %v, %v)", total, res, err)
	}
	// NUL byte safety (literal INSTR fallback).
	if res, total, err = store.SearchPlayerSummaries(ctx, "a\x00b", "all", 20, 0); err != nil || total != 0 {
		t.Fatalf("NUL byte search failed: (%d, %v)", total, err)
	}
	// Context cancellation propagation.
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.FilterPlayerSummaries(cancelCtx, PlayerDirectoryFilter{Query: "Bulk"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Telemetry match enrichment + bulk parity vs single-player ComputePlayerSplitStats.
	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "syn-1", RecordStartTimestamp: now.Unix(), Playlist: 11, BlueScore: 3, OrangeScore: 1}})
	statsJSON, _ := json.Marshal(ReplayPlayerStats{
		Name: "SynergyPartner", Platform: "Epic", OnlineID: "p_duo", Team: 0, Score: 600, Goals: 2,
		Boost:    &domain.BoostStats{BPM: 420, BCPM: 440, AvgAmount: 48, AmountStolen: 300, CountCollectedBig: 6, CountCollectedSmall: 18},
		Movement: &domain.MovementStats{PercentSupersonic: 18, PercentBoostSpeed: 40, PercentSlowSpeed: 42, PercentGround: 60, PercentLowAir: 32, PercentHighAir: 8, CountPowerslide: 20, TimePowerslide: 14},
	})
	_ = store.SaveMatchPlayers(ctx, "syn-1", []*MatchPlayerRecord{
		{MatchGUID: "syn-1", PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true, Score: 500},
		{MatchGUID: "syn-1", PlayerID: "Epic|p_duo|0", TeamNum: 0, IsLocal: false, Score: 600, Goals: 2, StatsJSON: string(statsJSON)},
	})
	_ = store.RecordMatchResults(ctx, "syn-1", 11, []PlayerOutcome{{PlayerID: "Epic|p_duo|0", Platform: "Epic", PlayerName: "SynergyPartner", IsTeammate: true, Won: true}}, now)

	// Bulk FilterPlayerSummaries over > 250 non-local players + sort by MMR descending.
	ranked, rankedTotal, err := store.FilterPlayerSummaries(ctx, PlayerDirectoryFilter{Sort: "mmr", Order: "desc", Limit: 5})
	if err != nil || rankedTotal < 260 || len(ranked) != 5 {
		t.Fatalf("FilterPlayerSummaries bulk failed: len=%d total=%d err=%v", len(ranked), rankedTotal, err)
	}
	if ranked[0].PlayerID != "Epic|p_duo|0" {
		t.Fatalf("top MMR player = %s, want Epic|p_duo|0", ranked[0].PlayerID)
	}

	singleWith, singleAgainst := store.ComputePlayerSplitStats(ctx, "Epic|p_duo|0", "SynergyPartner", nil, 0, 0, 0, 0, 0, 0)
	if ranked[0].WithYouStats.Games != singleWith.Games || ranked[0].WithYouStats.AvgBPM != singleWith.AvgBPM || ranked[0].AgainstYouStats.Games != singleAgainst.Games {
		t.Fatalf("bulk vs single split stats mismatch: bulk=(%+v, %+v) single=(%+v, %+v)", ranked[0].WithYouStats, ranked[0].AgainstYouStats, singleWith, singleAgainst)
	}

	// Spotlights determinism check.
	spots, err := store.GetPlayerSpotlights(ctx)
	if err != nil {
		t.Fatalf("GetPlayerSpotlights failed: %v", err)
	}
	if spots == nil {
		t.Fatal("expected non-nil GlobalSpotlights")
	}
}

func TestRepoSessions_AndTimestampHelpers(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	if _, err := store.GetSession(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session = %v, want ErrSessionNotFound", err)
	}

	start := time.Date(2026, 5, 1, 14, 0, 0, 123456789, time.UTC)
	lastMatch := start.Add(45 * time.Minute)
	sess := &SessionRecord{
		SessionID:      "sess-1",
		StartedAt:      start,
		LastMatchAt:    &lastMatch,
		UpdatedAt:      lastMatch,
		TotalMatches:   6,
		TotalWins:      4,
		TotalLosses:    2,
		WinRate:        66.7,
		PlaylistsJSON:  `{"11":{"wins":4}}`,
		MatchesJSON:    `["m1","m2"]`,
		LocalPlayerIDs: []string{"Steam|me|0"},
		Dismissed:      true,
	}
	if err := store.SaveSession(ctx, sess); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	got, err := store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if !got.StartedAt.Equal(start) || got.EndedAt != nil || !got.Dismissed || got.TotalMatches != 6 {
		t.Fatalf("GetSession mismatch: %+v", got)
	}

	// Verify decodeSessionTime handles seconds, milliseconds, and nanoseconds seamlessly.
	secT := decodeSessionTime(1700000000)
	msT := decodeSessionTime(1700000000123)
	nsT := decodeSessionTime(1700000000123456789)
	if secT.Unix() != 1700000000 || msT.UnixMilli() != 1700000000123 || nsT.UnixNano() != 1700000000123456789 {
		t.Fatalf("decodeSessionTime mismatch: sec=%v ms=%v ns=%v", secT, msT, nsT)
	}

	list, total, err := store.ListSessions(ctx, 10, 0)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListSessions = (%v, %d, %v)", list, total, err)
	}
	if err := store.DeleteSession(ctx, "sess-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSession(ctx, "sess-1"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("deleted session still exists: %v", err)
	}

	// Pure helper coverage: ParseReplayDate, NormalizeBallchasingURL, ComputeRelationshipTag.
	if parsedTime, ok := ParseReplayDate("2026-01-02T03:04:05Z"); !ok || parsedTime.Unix() != 1767323045 {
		t.Fatalf("ParseReplayDate RFC3339 = (%v, %v)", parsedTime, ok)
	}
	if u := NormalizeBallchasingURL("abc-123", "https://ballchasing.com/api/replays/abc-123"); u != "https://ballchasing.com/replay/abc-123" {
		t.Fatalf("NormalizeBallchasingURL = %q", u)
	}
	if tag := ComputeRelationshipTag(4, 2, 0, 1); tag != "PRIME DUO" {
		t.Fatalf("ComputeRelationshipTag = %q, want PRIME DUO", tag)
	}
}
