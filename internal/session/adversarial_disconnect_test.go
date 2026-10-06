package session

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
)

// ============================================================================
// Adversarial Disconnect Suite 1: Session Goal Summation & Box Scores
// ============================================================================

func TestAdversarialSession_MidGameDisconnect_GoalSummationAndScores(t *testing.T) {
	st := NewSessionTracker()

	localTeam := 0
	winnerTeam := 0

	// Match snapshot where disconnected teammate scored 2 goals, active local scored 1,
	// disconnected opponent scored 1, active opponent scored 1.
	match := &playertrack.CurrentMatchResponse{
		ActiveMatch:  false,
		MatchEnded:   true,
		MatchGUID:    "adv-sess-drop-1",
		PlaylistID:   11,
		PlaylistName: "Ranked Doubles",
		LocalTeam:    &localTeam,
		WinnerTeam:   &winnerTeam,
		Result:       "victory",
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID:       "Steam|local_hero|0",
			Name:           "LocalHero",
			TeamNum:        0,
			IsLocal:        true,
			IsDisconnected: false,
			Stats: playertrack.PlayerStatsSummary{
				Goals: 1,
				Score: 100,
			},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|dropped_tm|0",
				Name:           "DroppedTm",
				TeamNum:        0,
				IsLocal:        false,
				IsDisconnected: true, // Disconnected teammate!
				Stats: playertrack.PlayerStatsSummary{
					Goals: 2, // Scored 2 goals before dropping
					Score: 250,
				},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|dropped_opp|0",
				Name:           "DroppedOpp",
				TeamNum:        1,
				IsLocal:        false,
				IsDisconnected: true, // Disconnected opponent!
				Stats: playertrack.PlayerStatsSummary{
					Goals: 1, // Scored 1 goal before dropping
					Score: 120,
				},
			},
			{
				PlayerID:       "Steam|active_opp|0",
				Name:           "ActiveOpp",
				TeamNum:        1,
				IsLocal:        false,
				IsDisconnected: false,
				Stats: playertrack.PlayerStatsSummary{
					Goals: 1,
					Score: 110,
				},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}

	st.ConcludeMatch(match)

	summary := st.GetSessionSummary()
	if len(summary.Matches) != 1 {
		t.Fatalf("expected 1 match in history, got %d", len(summary.Matches))
	}
	m := summary.Matches[0]

	// Invariant: BlueScore must include dropped teammate's goals (1 + 2 = 3)
	if m.BlueScore != 3 {
		t.Errorf("expected BlueScore=3 (including dropped teammate), got %d", m.BlueScore)
	}
	// Invariant: OrangeScore must include dropped opponent's goals (1 + 1 = 2)
	if m.OrangeScore != 2 {
		t.Errorf("expected OrangeScore=2 (including dropped opponent), got %d", m.OrangeScore)
	}

	// Invariant: Players roster must have 4 players with accurate IsDisconnected and Won flags
	if len(m.Players) != 4 {
		t.Fatalf("expected 4 players in match roster, got %d", len(m.Players))
	}

	var foundDroppedTm, foundDroppedOpp bool
	for _, p := range m.Players {
		if p.PlayerID == "Steam|dropped_tm|0" {
			foundDroppedTm = true
			if !p.IsDisconnected {
				t.Errorf("dropped teammate should have IsDisconnected=true")
			}
			if p.Won == nil || *p.Won != true {
				t.Errorf("dropped teammate on blue team should have Won=true")
			}
			if p.Stats.Goals != 2 {
				t.Errorf("dropped teammate goals preserved as 2, got %d", p.Stats.Goals)
			}
		}
		if p.PlayerID == "Steam|dropped_opp|0" {
			foundDroppedOpp = true
			if !p.IsDisconnected {
				t.Errorf("dropped opponent should have IsDisconnected=true")
			}
			if p.Won == nil || *p.Won != false {
				t.Errorf("dropped opponent on orange team should have Won=false")
			}
		}
	}
	if !foundDroppedTm || !foundDroppedOpp {
		t.Fatalf("both dropped players must be present in concluded match history")
	}
}

// ============================================================================
// Adversarial Disconnect Suite 2: SSE Broadcast Emits is_disconnected: true
// ============================================================================

func TestAdversarialSession_SSEBroadcast_EmitsDisconnectedFlags(t *testing.T) {
	st := NewSessionTracker()

	ch, unsub := st.Subscribe()
	defer unsub()

	localTeam := 0
	match := &playertrack.CurrentMatchResponse{
		ActiveMatch:  true,
		MatchEnded:   false,
		MatchGUID:    "adv-sse-drop-1",
		PlaylistID:   11,
		PlaylistName: "Ranked Doubles",
		LocalTeam:    &localTeam,
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID:       "Steam|local_hero|0",
			Name:           "LocalHero",
			TeamNum:        0,
			IsLocal:        true,
			IsDisconnected: false,
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|leaver_tm|0",
				Name:           "LeaverTm",
				TeamNum:        0,
				IsLocal:        false,
				IsDisconnected: true, // Marked disconnected
			},
		},
		UpdatedAt: time.Now().UTC(),
	}

	st.RecordActiveMatch(match)

	select {
	case evt := <-ch:
		if evt.Event != EventMatchUpdate {
			t.Fatalf("expected EventMatchUpdate, got %q", evt.Event)
		}
		jsonBytes, err := json.Marshal(evt.Data)
		if err != nil {
			t.Fatalf("failed to marshal event data: %v", err)
		}
		jsonStr := string(jsonBytes)
		if !strings.Contains(jsonStr, `"is_disconnected":true`) {
			t.Fatalf("SSE broadcast JSON payload missing '\"is_disconnected\":true': %s", jsonStr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE EventMatchUpdate")
	}
}

// ============================================================================
// Adversarial Disconnect Suite 3: Concurrency Stress on Session Tracking
// ============================================================================

func TestAdversarialSession_ConcurrentDisconnectsAndConcludes(t *testing.T) {
	st := NewSessionTracker()

	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	// Publisher 1: Rapid RecordActiveMatch with alternating disconnect flags
	wg.Add(1)
	go func() {
		defer wg.Done()
		rng := rand.New(rand.NewSource(99))
		localTeam := 0
		for i := 0; ; i++ {
			select {
			case <-stopCh:
				return
			default:
			}
			isDrop := (rng.Intn(2) == 0)
			snap := &playertrack.CurrentMatchResponse{
				ActiveMatch:  true,
				MatchGUID:    fmt.Sprintf("guid-race-%d", i%5),
				PlaylistID:   11,
				LocalTeam:    &localTeam,
				UpdatedAt:    time.Now().UTC(),
				Teammates: []playertrack.LobbyPlayer{
					{
						PlayerID:       "Steam|churn_tm|0",
						TeamNum:        0,
						IsDisconnected: isDrop,
					},
				},
			}
			st.RecordActiveMatch(snap)
			time.Sleep(500 * time.Microsecond)
		}
	}()

	// Publisher 2: Periodic ConcludeMatch with disconnected participants
	wg.Add(1)
	go func() {
		defer wg.Done()
		localTeam := 0
		winnerTeam := 0
		for i := 0; ; i++ {
			select {
			case <-stopCh:
				return
			default:
			}
			match := &playertrack.CurrentMatchResponse{
				ActiveMatch:  false,
				MatchEnded:   true,
				MatchGUID:    fmt.Sprintf("conclude-race-%d", i),
				PlaylistID:   11,
				LocalTeam:    &localTeam,
				WinnerTeam:   &winnerTeam,
				Result:       "victory",
				UpdatedAt:    time.Now().UTC(),
				Teammates: []playertrack.LobbyPlayer{
					{
						PlayerID:       "Steam|churn_tm|0",
						TeamNum:        0,
						IsDisconnected: true,
						Stats:          playertrack.PlayerStatsSummary{Goals: 1},
					},
				},
			}
			st.ConcludeMatch(match)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Readers: GetSessionSummary
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
				}
				_ = st.GetSessionSummary()
				time.Sleep(300 * time.Microsecond)
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stopCh)
	wg.Wait()
}
