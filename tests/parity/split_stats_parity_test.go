package parity_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestRoleStatsAccumulator_ParityWithDomainAndTelemetry(t *testing.T) {
	t.Run("BoxScoreAndWinRateParity", func(t *testing.T) {
		rows := []domain.MatchPlayerRow{
			{MatchGUID: "g1", Role: "teammate", Outcome: "victory", Score: 640, Goals: 3, Assists: 1, Saves: 2, Shots: 6, Demos: 1},
			{MatchGUID: "g2", Role: "teammate", Outcome: "defeat", Score: 410, Goals: 1, Assists: 0, Saves: 1, Shots: 4, Demos: 2},
			{MatchGUID: "g3", Role: "teammate", Outcome: "victory", Score: 525, Goals: 2, Assists: 2, Saves: 0, Shots: 5, Demos: 0},
			{MatchGUID: "g4", Role: "opponent", Outcome: "defeat", Score: 330, Goals: 1, Assists: 1, Saves: 3, Shots: 3, Demos: 1},
			{MatchGUID: "g5", Role: "opponent", Outcome: "victory", Score: 490, Goals: 2, Assists: 0, Saves: 1, Shots: 7, Demos: 3},
		}
		domainSummary := domain.BuildRoleStatsSummary(rows)

		var tmAcc, oppAcc storage.RoleStatsAccumulator
		for _, r := range rows {
			won := r.Outcome == "victory"
			if r.Role == "teammate" {
				tmAcc.AddMatchOutcome(won, false)
				tmAcc.AddPlayerBoxAndTelemetry(r.Score, r.Goals, r.Assists, r.Saves, r.Shots, r.Demos, "")
			} else {
				oppAcc.AddMatchOutcome(won, false)
				oppAcc.AddPlayerBoxAndTelemetry(r.Score, r.Goals, r.Assists, r.Saves, r.Shots, r.Demos, "")
			}
		}

		tmGot := tmAcc.Finalize(0, 0)
		oppGot := oppAcc.Finalize(0, 0)

		assertRoleMatchesDomain := func(label string, got *storage.PlayerRoleStatsSummary, d contracts.RoleStatsBreakdown) {
			t.Helper()
			if got.Games != d.Games || got.Wins != d.Wins || got.Losses != d.Losses {
				t.Fatalf("[%s] W-L mismatch: got (%d/%d/%d), domain (%d/%d/%d)", label, got.Games, got.Wins, got.Losses, d.Games, d.Wins, d.Losses)
			}
			if got.WinRate != d.WinRate {
				t.Errorf("[%s] WinRate = %.2f, domain = %.2f", label, got.WinRate, d.WinRate)
			}
			if got.TotalScore != d.Score || got.TotalGoals != d.Goals || got.TotalAssists != d.Assists || got.TotalSaves != d.Saves || got.TotalShots != d.Shots || got.TotalDemosInflicted != d.Demos {
				t.Errorf("[%s] totals mismatch: got %+v vs domain %+v", label, got, d)
			}
			if got.AvgGoals != d.AvgGoals || got.AvgAssists != d.AvgAssists || got.AvgSaves != d.AvgSaves || got.AvgShots != d.AvgShots || got.AvgDemosInflicted != d.AvgDemos || got.ShootingPct != d.ShotPct {
				t.Errorf("[%s] averages mismatch: got %+v vs domain %+v", label, got, d)
			}
			wantAvgScore1 := domain.RoundFloat(float64(d.Score)/float64(d.Games), 1)
			if got.AvgScore != wantAvgScore1 {
				t.Errorf("[%s] AvgScore = %.2f, want %.2f", label, got.AvgScore, wantAvgScore1)
			}
		}

		assertRoleMatchesDomain("teammate", tmGot, domainSummary.Teammate)
		assertRoleMatchesDomain("opponent", oppGot, domainSummary.Opponent)
	})

	t.Run("FullTelemetryAccumulationAndDraws", func(t *testing.T) {
		var acc storage.RoleStatsAccumulator

		p1 := storage.ReplayPlayerStats{
			Demo: &domain.DemoStats{Inflicted: 2, Taken: 1},
			Boost: &domain.BoostStats{
				BPM: 420.5, BCPM: 380.2, AvgAmount: 45.6,
				AmountStolen: 120, CountCollectedBig: 8, CountCollectedSmall: 25,
			},
			Movement: &domain.MovementStats{
				PercentSupersonic: 24.4, PercentBoostSpeed: 40.0, PercentSlowSpeed: 35.6,
				PercentGround: 60.0, PercentLowAir: 30.0, PercentHighAir: 10.0,
				CountPowerslide: 14, TimePowerslide: 6.45,
			},
			Positioning: &domain.PositioningStats{
				PercentDefensiveThird: 35.0, PercentNeutralThird: 45.0, PercentOffensiveThird: 20.0,
				PercentBehindBall: 68.0, PercentMostForward: 30.0, PercentMostBack: 32.0,
				AvgDistanceToBall: 2450.5,
			},
		}
		b1, _ := json.Marshal(p1)

		p2 := storage.ReplayPlayerStats{
			Demo: &domain.DemoStats{Inflicted: 4, Taken: 3},
			Boost: &domain.BoostStats{
				BPM: 380.0, BCPM: 340.0, AvgAmount: 42.0,
				AmountStolen: 80, CountCollectedBig: 6, CountCollectedSmall: 19,
			},
			Movement: &domain.MovementStats{
				PercentSupersonic: 18.0, PercentBoostSpeed: 42.0, PercentSlowSpeed: 40.0,
				PercentGround: 65.0, PercentLowAir: 25.0, PercentHighAir: 10.0,
				CountPowerslide: 10, AvgPowerslideDuration: 0.45,
			},
		}
		b2, _ := json.Marshal(p2)

		// Game 1: victory with full telemetry; Game 2: defeat with partial telemetry (no positioning); Game 3: draw with box score only
		acc.AddMatchOutcome(true, false)
		acc.AddPlayerBoxAndTelemetry(500, 2, 1, 2, 5, 0, string(b1))

		acc.AddMatchOutcome(false, false)
		acc.AddPlayerBoxAndTelemetry(350, 1, 0, 1, 3, 2, string(b2))

		acc.AddMatchOutcome(false, true) // Draw excluded from WinRate denominator
		acc.AddPlayerBoxAndTelemetry(400, 1, 1, 0, 2, 1, "")

		got := acc.Finalize(0, 0)
		if got.Games != 3 || got.Wins != 1 || got.Losses != 1 || got.WinRate != 50.0 {
			t.Fatalf("unexpected game counts/winrate: %+v", got)
		}
		if got.TotalDemosInflicted != 5 { // 2 fallback from Demo.Inflicted + 2 explicit + 1 explicit
			t.Errorf("TotalDemosInflicted = %d, want 5", got.TotalDemosInflicted)
		}
		if got.TotalDemosTaken != 4 || got.TotalBoostStolen != 200 || got.TotalBigPads != 14 || got.TotalSmallPads != 44 {
			t.Errorf("unexpected boost/demo totals: %+v", got)
		}
		if got.AvgBPM != 400.3 || got.AvgBCPM != 360.1 || got.AvgBoost != 43.8 {
			t.Errorf("unexpected boost avgs: bpm=%.1f bcpm=%.1f avg=%.1f", got.AvgBPM, got.AvgBCPM, got.AvgBoost)
		}
		if got.AvgPowerslideCount != 12.0 || got.AvgPowerslideDuration != 3.45 {
			t.Errorf("unexpected powerslide avgs: count=%.1f dur=%.2f", got.AvgPowerslideCount, got.AvgPowerslideDuration)
		}
		// Positioning denominator is posGames=1 (only p1 had positioning)
		if got.AvgDefThirdPct != 35.0 || got.AvgDistToBall != 2450.5 {
			t.Errorf("unexpected positioning avgs: def=%.1f dist=%.1f", got.AvgDefThirdPct, got.AvgDistToBall)
		}
	})

	t.Run("FallbackOverridesWhenGreater", func(t *testing.T) {
		var acc storage.RoleStatsAccumulator
		acc.AddMatchOutcome(true, false)
		got := acc.Finalize(4, 2)
		if got.Games != 6 || got.Wins != 4 || got.Losses != 2 {
			t.Fatalf("expected fallback override (6/4/2), got (%d/%d/%d)", got.Games, got.Wins, got.Losses)
		}
		wantWR := math.Round((4.0/6.0)*1000) / 10
		if got.WinRate != wantWR {
			t.Errorf("WinRate = %.1f, want %.1f", got.WinRate, wantWR)
		}
	})
}

func TestComputeGlobalSpotlights_DeterminismAndTieBreaking(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	t.Run("EmptyAndInsufficient", func(t *testing.T) {
		res := storage.ComputeGlobalSpotlights([]*storage.PlayerSummary{
			{PlayerRecord: storage.PlayerRecord{PlayerID: "p1"}, TotalWinsAsTeammate: 1, TotalWinsAsOpponent: 1},
		})
		if res.BestTeammate.Player != nil || res.BestTeammate.Sub != "NOT ENOUGH GAMES YET" {
			t.Errorf("unexpected BestTeammate: %+v", res.BestTeammate)
		}
		if res.MostFaced.Player != nil || res.MostFaced.Sub != "NOT ENOUGH GAMES YET" {
			t.Errorf("unexpected MostFaced: %+v", res.MostFaced)
		}
		if res.ToughestRival.Player != nil || res.ToughestRival.Sub != "NOT ENOUGH GAMES YET" {
			t.Errorf("unexpected ToughestRival: %+v", res.ToughestRival)
		}
	})

	t.Run("TieBreakOrderIndependent", func(t *testing.T) {
		pA := &storage.PlayerSummary{
			PlayerRecord:        storage.PlayerRecord{PlayerID: "Epic|alpha|0", PlayerName: "Alpha", LastSeenAt: now},
			TotalWinsAsTeammate: 4, TotalLossesAsTeammate: 1, // 80%, 5 games
			TotalWinsAsOpponent: 0, TotalLossesAsOpponent: 3, // 0% WR (NEVER BEATEN), 3 games
		}
		pB := &storage.PlayerSummary{
			PlayerRecord:        storage.PlayerRecord{PlayerID: "Epic|bravo|0", PlayerName: "Bravo", LastSeenAt: now.Add(time.Hour)},
			TotalWinsAsTeammate: 4, TotalLossesAsTeammate: 1, // 80%, 5 games, newer LastSeenAt wins tie
			TotalWinsAsOpponent: 2, TotalLossesAsOpponent: 4, // 6 games total -> MostFaced
		}

		res1 := storage.ComputeGlobalSpotlights([]*storage.PlayerSummary{pA, pB})
		res2 := storage.ComputeGlobalSpotlights([]*storage.PlayerSummary{pB, pA})

		if res1.BestTeammate.Player.PlayerID != pB.PlayerID || res2.BestTeammate.Player.PlayerID != pB.PlayerID {
			t.Errorf("BestTeammate tiebreak failed: %s vs %s", res1.BestTeammate.Player.PlayerID, res2.BestTeammate.Player.PlayerID)
		}
		if res1.MostFaced.Player.PlayerID != pB.PlayerID || res1.MostFaced.Value != "2-4" {
			t.Errorf("MostFaced mismatch: %+v", res1.MostFaced)
		}
		if res1.ToughestRival.Player.PlayerID != pA.PlayerID || res1.ToughestRival.Sub != "NEVER BEATEN" {
			t.Errorf("ToughestRival mismatch: %+v", res1.ToughestRival)
		}
	})
}
