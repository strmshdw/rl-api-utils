package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/dank/rl-api-utils/internal/domain"
)

// ComputeGlobalSpotlights calculates dataset-wide spotlights from candidate player summaries.
func ComputeGlobalSpotlights(candidates []*PlayerSummary) *GlobalSpotlights {
	var bestTeammate *PlayerSummary
	bestTeammateRate := -1.0
	bestTeammateGames := -1

	var mostFaced *PlayerSummary
	mostFacedGames := -1

	var toughestRival *PlayerSummary
	toughestRivalRate := 2.0
	toughestRivalGames := -1

	// Ensure deterministic evaluation order before tie-breaking.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].PlayerID < candidates[j].PlayerID
	})

	for _, p := range candidates {
		tmGames := p.TotalWinsAsTeammate + p.TotalLossesAsTeammate
		if tmGames >= 2 {
			rate := float64(p.TotalWinsAsTeammate) / float64(tmGames)
			if rate > bestTeammateRate || (rate == bestTeammateRate && tmGames > bestTeammateGames) ||
				(rate == bestTeammateRate && tmGames == bestTeammateGames && (bestTeammate == nil || p.LastSeenAt.After(bestTeammate.LastSeenAt) || (p.LastSeenAt.Equal(bestTeammate.LastSeenAt) && p.PlayerID < bestTeammate.PlayerID))) {
				bestTeammate = p
				bestTeammateRate = rate
				bestTeammateGames = tmGames
			}
		}

		oppGames := p.TotalWinsAsOpponent + p.TotalLossesAsOpponent
		if oppGames >= 2 {
			if oppGames > mostFacedGames || (oppGames == mostFacedGames && (mostFaced == nil || p.LastSeenAt.After(mostFaced.LastSeenAt) || (p.LastSeenAt.Equal(mostFaced.LastSeenAt) && p.PlayerID < mostFaced.PlayerID))) {
				mostFaced = p
				mostFacedGames = oppGames
			}

			rate := float64(p.TotalWinsAsOpponent) / float64(oppGames)
			if rate < toughestRivalRate || (rate == toughestRivalRate && oppGames > toughestRivalGames) ||
				(rate == toughestRivalRate && oppGames == toughestRivalGames && (toughestRival == nil || p.LastSeenAt.After(toughestRival.LastSeenAt) || (p.LastSeenAt.Equal(toughestRival.LastSeenAt) && p.PlayerID < toughestRival.PlayerID))) {
				toughestRival = p
				toughestRivalRate = rate
				toughestRivalGames = oppGames
			}
		}
	}

	res := &GlobalSpotlights{}
	if bestTeammate != nil {
		res.BestTeammate = &SpotlightSummary{
			Player: bestTeammate,
			Value:  fmt.Sprintf("%d%%", int(math.Round(bestTeammateRate*100))),
			Sub:    fmt.Sprintf("%d-%d TOGETHER", bestTeammate.TotalWinsAsTeammate, bestTeammate.TotalLossesAsTeammate),
			Tone:   "win",
		}
	} else {
		res.BestTeammate = &SpotlightSummary{
			Player: nil,
			Value:  "—",
			Sub:    "NOT ENOUGH GAMES YET",
			Tone:   "win",
		}
	}

	if mostFaced != nil {
		res.MostFaced = &SpotlightSummary{
			Player: mostFaced,
			Value:  fmt.Sprintf("%d-%d", mostFaced.TotalWinsAsOpponent, mostFaced.TotalLossesAsOpponent),
			Sub:    fmt.Sprintf("%d GAMES AS OPPONENT", mostFacedGames),
			Tone:   "neutral",
		}
	} else {
		res.MostFaced = &SpotlightSummary{
			Player: nil,
			Value:  "—",
			Sub:    "NOT ENOUGH GAMES YET",
			Tone:   "neutral",
		}
	}

	if toughestRival != nil {
		subStr := fmt.Sprintf("%d%% WIN RATE", int(math.Round(toughestRivalRate*100)))
		if toughestRival.TotalWinsAsOpponent == 0 {
			subStr = "NEVER BEATEN"
		}
		res.ToughestRival = &SpotlightSummary{
			Player: toughestRival,
			Value:  fmt.Sprintf("%d-%d", toughestRival.TotalWinsAsOpponent, toughestRival.TotalLossesAsOpponent),
			Sub:    subStr,
			Tone:   "loss",
		}
	} else {
		res.ToughestRival = &SpotlightSummary{
			Player: nil,
			Value:  "—",
			Sub:    "NOT ENOUGH GAMES YET",
			Tone:   "loss",
		}
	}

	return res
}

// @perf-isolation(parity_test="tests/parity/split_stats_parity_test.go")
// What: Streaming single-pass RoleStatsAccumulator aggregating match outcomes, box scores, and optional JSON telemetry.
// Why: Eliminates allocating intermediate ReplayPlayerStats slices across thousands of match_players rows during directory search/bulk stats aggregation.
// How: Accumulates raw float64 sums per sub-struct (Boost, Movement, Positioning, Demo) inline and delegates fixed-decimal rounding to domain.RoundFloat and domain.ComputeWinRate in Finalize.
type RoleStatsAccumulator struct {
	Summary     PlayerRoleStatsSummary
	statGames   int
	boostGames  int
	moveGames   int
	posGames    int
	sumBPM      float64
	sumBCPM     float64
	sumAvgBoost float64
	sumSuperPct float64
	sumBoostPct float64
	sumSlowPct  float64
	sumGround   float64
	sumLowAir   float64
	sumHighAir  float64
	sumPSCount  float64
	sumPSDur    float64
	sumDefThird float64
	sumNeuThird float64
	sumOffThird float64
	sumBehind   float64
	sumFirstMan float64
	sumThirdMan float64
	sumDistBall float64
}

// AddMatchOutcome records a single game win/loss outcome (from the local player's perspective).
func (a *RoleStatsAccumulator) AddMatchOutcome(localWon bool, isDraw bool) {
	a.Summary.Games++
	if !isDraw {
		if localWon {
			a.Summary.Wins++
		} else {
			a.Summary.Losses++
		}
	}
}

// AddPlayerBoxAndTelemetry accumulates a player's box score and optional statsJSON telemetry payload.
func (a *RoleStatsAccumulator) AddPlayerBoxAndTelemetry(score, goals, assists, saves, shots, demos int, statsJSON string) {
	a.statGames++
	a.Summary.TotalScore += score
	a.Summary.TotalGoals += goals
	a.Summary.TotalAssists += assists
	a.Summary.TotalSaves += saves
	a.Summary.TotalShots += shots
	demosInflicted := demos

	if strings.TrimSpace(statsJSON) != "" && statsJSON != "{}" {
		var rps ReplayPlayerStats
		if err := json.Unmarshal([]byte(statsJSON), &rps); err == nil {
			if rps.Demo != nil {
				if demosInflicted == 0 && rps.Demo.Inflicted > 0 {
					demosInflicted = rps.Demo.Inflicted
				}
				a.Summary.TotalDemosTaken += rps.Demo.Taken
			}
			if rps.Boost != nil {
				a.boostGames++
				a.sumBPM += rps.Boost.BPM
				a.sumBCPM += rps.Boost.BCPM
				a.sumAvgBoost += rps.Boost.AvgAmount
				a.Summary.TotalBoostStolen += rps.Boost.AmountStolen
				a.Summary.TotalBigPads += rps.Boost.CountCollectedBig
				a.Summary.TotalSmallPads += rps.Boost.CountCollectedSmall
			}
			if rps.Movement != nil {
				a.moveGames++
				a.sumSuperPct += rps.Movement.PercentSupersonic
				a.sumBoostPct += rps.Movement.PercentBoostSpeed
				a.sumSlowPct += rps.Movement.PercentSlowSpeed
				a.sumGround += rps.Movement.PercentGround
				a.sumLowAir += rps.Movement.PercentLowAir
				a.sumHighAir += rps.Movement.PercentHighAir
				a.sumPSCount += float64(rps.Movement.CountPowerslide)
				if rps.Movement.TimePowerslide > 0 {
					a.sumPSDur += rps.Movement.TimePowerslide
				} else {
					a.sumPSDur += rps.Movement.AvgPowerslideDuration
				}
			}
			if rps.Positioning != nil {
				a.posGames++
				a.sumDefThird += rps.Positioning.PercentDefensiveThird
				a.sumNeuThird += rps.Positioning.PercentNeutralThird
				a.sumOffThird += rps.Positioning.PercentOffensiveThird
				a.sumBehind += rps.Positioning.PercentBehindBall
				a.sumFirstMan += rps.Positioning.PercentMostForward
				a.sumThirdMan += rps.Positioning.PercentMostBack
				a.sumDistBall += rps.Positioning.AvgDistanceToBall
			}
		}
	}
	a.Summary.TotalDemosInflicted += demosInflicted
}

// Finalize computes all per-game averages and win rates, applying fallbackWins/fallbackLosses if higher than recorded match rows.
func (a *RoleStatsAccumulator) Finalize(fallbackWins, fallbackLosses int) *PlayerRoleStatsSummary {
	if fallbackWins+fallbackLosses > a.Summary.Games {
		a.Summary.Wins = fallbackWins
		a.Summary.Losses = fallbackLosses
		a.Summary.Games = fallbackWins + fallbackLosses
	}
	decided := a.Summary.Wins + a.Summary.Losses
	if decided > 0 {
		a.Summary.WinRate = domain.ComputeWinRate(a.Summary.Wins, decided)
	}
	if a.statGames > 0 {
		fg := float64(a.statGames)
		a.Summary.AvgScore = domain.RoundFloat(float64(a.Summary.TotalScore)/fg, 1)
		a.Summary.AvgGoals = domain.RoundFloat(float64(a.Summary.TotalGoals)/fg, 2)
		a.Summary.AvgAssists = domain.RoundFloat(float64(a.Summary.TotalAssists)/fg, 2)
		a.Summary.AvgSaves = domain.RoundFloat(float64(a.Summary.TotalSaves)/fg, 2)
		a.Summary.AvgShots = domain.RoundFloat(float64(a.Summary.TotalShots)/fg, 2)
		a.Summary.AvgDemosInflicted = domain.RoundFloat(float64(a.Summary.TotalDemosInflicted)/fg, 2)
		a.Summary.AvgDemosTaken = domain.RoundFloat(float64(a.Summary.TotalDemosTaken)/fg, 2)
		if a.Summary.TotalShots > 0 {
			a.Summary.ShootingPct = domain.RoundFloat((float64(a.Summary.TotalGoals)/float64(a.Summary.TotalShots))*100.0, 1)
		}
	}
	if a.boostGames > 0 {
		bg := float64(a.boostGames)
		a.Summary.AvgBPM = domain.RoundFloat(a.sumBPM/bg, 1)
		a.Summary.AvgBCPM = domain.RoundFloat(a.sumBCPM/bg, 1)
		a.Summary.AvgBoost = domain.RoundFloat(a.sumAvgBoost/bg, 1)
	}
	if a.moveGames > 0 {
		mg := float64(a.moveGames)
		a.Summary.AvgSupersonicPct = domain.RoundFloat(a.sumSuperPct/mg, 1)
		a.Summary.AvgBoostSpeedPct = domain.RoundFloat(a.sumBoostPct/mg, 1)
		a.Summary.AvgSlowSpeedPct = domain.RoundFloat(a.sumSlowPct/mg, 1)
		a.Summary.AvgGroundPct = domain.RoundFloat(a.sumGround/mg, 1)
		a.Summary.AvgLowAirPct = domain.RoundFloat(a.sumLowAir/mg, 1)
		a.Summary.AvgHighAirPct = domain.RoundFloat(a.sumHighAir/mg, 1)
		a.Summary.AvgPowerslideCount = domain.RoundFloat(a.sumPSCount/mg, 1)
		a.Summary.AvgPowerslideDuration = domain.RoundFloat(a.sumPSDur/mg, 2)
	}
	if a.posGames > 0 {
		pg := float64(a.posGames)
		a.Summary.AvgDefThirdPct = domain.RoundFloat(a.sumDefThird/pg, 1)
		a.Summary.AvgNeutralThirdPct = domain.RoundFloat(a.sumNeuThird/pg, 1)
		a.Summary.AvgOffThirdPct = domain.RoundFloat(a.sumOffThird/pg, 1)
		a.Summary.AvgBehindBallPct = domain.RoundFloat(a.sumBehind/pg, 1)
		a.Summary.AvgFirstManPct = domain.RoundFloat(a.sumFirstMan/pg, 1)
		a.Summary.AvgThirdManPct = domain.RoundFloat(a.sumThirdMan/pg, 1)
		a.Summary.AvgDistToBall = domain.RoundFloat(a.sumDistBall/pg, 1)
	}
	out := a.Summary
	return &out
}
