package playertrack

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

type spyMatchListener struct {
	mu      sync.Mutex
	updates []*CurrentMatchResponse
}

func (s *spyMatchListener) OnMatchStarted(*CurrentMatchResponse) {}
func (s *spyMatchListener) OnActiveMatchUpdated(m *CurrentMatchResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, m)
}
func (s *spyMatchListener) OnMatchConcluded(*CurrentMatchResponse) {}

func TestFromSkillAndLiveScoutingHydration(t *testing.T) {
	skill := rlapi.Skill{
		Playlist:               11,
		Mu:                     69.25,
		Sigma:                  3.55,
		Tier:                   16,
		Division:               2,
		MMR:                    69.25,
		WinStreak:              6,
		MatchesPlayed:          22,
		PlacementMatchesPlayed: 10,
	}
	rank := FromSkill(skill)
	if rank.Sigma != 3.55 || rank.WinStreak != 6 || rank.PlacementMatchesPlayed != 10 || rank.MatchesPlayed != 22 {
		t.Fatalf("FromSkill failed to preserve PsyNet telemetry: %+v", rank)
	}

	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "scouting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	oppID := "Steam|76561198000000999|0"
	localID := "Epic|local_acc_001|0"
	now := time.Now().UTC()
	if err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    oppID,
		Platform:    "Steam",
		PlayerName:  "KnownOpponent",
		RanksJSON:   "{}",
		Notes:       "Fast kickoff challenge",
		Tags:        []string{"verified", "kickoff-pro"},
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatal(err)
	}

	mockSkills := NewMockSkillFetcher()
	mockSkills.SkillsMap[rlapi.PlayerID(oppID)] = []rlapi.Skill{skill}

	tracker, err := NewTracker(store, mockSkills, config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  localID,
		AutoFetchRanks: true,
	}, config.AuthConfig{Provider: "epic"})
	if err != nil {
		t.Fatal(err)
	}
	defer tracker.Close()

	spy := &spyMatchListener{}
	tracker.SetMatchStateListener(spy)

	if err := tracker.OnUpdateState(ctx, "match-scouting-1", 11, []statsapi.StatsPlayer{
		{PrimaryId: localID, Name: "Me", TeamNum: 0, Score: 300},
		{PrimaryId: oppID, Name: "KnownOpponent", TeamNum: 1, Score: 410},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait briefly for async rank fetch & scouting hydration
	deadline := time.Now().Add(2 * time.Second)
	var hydrated LobbyPlayer
	for time.Now().Before(deadline) {
		snap := tracker.GetCurrentMatch()
		if len(snap.Opponents) == 1 {
			hydrated = snap.Opponents[0]
			if hydrated.Notes == "Fast kickoff challenge" && hydrated.CurrentRank != nil && hydrated.CurrentRank.Sigma == 3.55 {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}

	if hydrated.Notes != "Fast kickoff challenge" {
		t.Fatalf("expected hydrated Notes='Fast kickoff challenge', got %q", hydrated.Notes)
	}
	if len(hydrated.Tags) != 2 || hydrated.Tags[0] != "verified" {
		t.Fatalf("expected hydrated Tags=[verified kickoff-pro], got %v", hydrated.Tags)
	}
	if hydrated.CurrentRank == nil || hydrated.CurrentRank.Sigma != 3.55 || hydrated.CurrentRank.WinStreak != 6 {
		t.Fatalf("expected hydrated CurrentRank with Sigma=3.55 WinStreak=6, got %+v", hydrated.CurrentRank)
	}

	// Live scouting update via UpdatePlayerScouting immediately updates currentMatch and notifies listener
	spy.mu.Lock()
	beforeCount := len(spy.updates)
	spy.mu.Unlock()

	tracker.UpdatePlayerScouting(oppID, "Confirmed alt account", []string{"smurf"})

	snapAfter := tracker.GetCurrentMatch()
	if len(snapAfter.Opponents) != 1 || snapAfter.Opponents[0].Notes != "Confirmed alt account" ||
		len(snapAfter.Opponents[0].Tags) != 1 || snapAfter.Opponents[0].Tags[0] != "smurf" {
		t.Fatalf("UpdatePlayerScouting did not update currentMatch: %+v", snapAfter.Opponents)
	}

	spy.mu.Lock()
	afterCount := len(spy.updates)
	spy.mu.Unlock()
	if afterCount <= beforeCount {
		t.Fatalf("expected UpdatePlayerScouting to emit OnActiveMatchUpdated (before=%d, after=%d)", beforeCount, afterCount)
	}
}
