package domain

import (
	"math"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/contracts"
)

// RoundFloat rounds val to precision decimal places.
func RoundFloat(val float64, precision int) float64 {
	if precision <= 0 {
		return math.Round(val)
	}
	ratio := math.Pow10(precision)
	return math.Round(val*ratio) / ratio
}

// ComputeWinRate calculates percentage wins (0..100) rounded to 1 decimal place.
func ComputeWinRate(wins, total int) float64 {
	if total <= 0 {
		return 0
	}
	return RoundFloat((float64(wins)/float64(total))*100.0, 1)
}

// ComputeMMRDelta computes end - start rounded to 2 decimal places when both are positive.
func ComputeMMRDelta(start, end float64) float64 {
	if start <= 0 || end <= 0 {
		return 0
	}
	return RoundFloat(end-start, 2)
}

// MatchPlayerRow represents one persisted player participation row used for split role stats aggregation.
type MatchPlayerRow struct {
	MatchGUID string
	Role      string // "teammate" or "opponent"
	Outcome   string // "victory", "defeat", or ""
	Score     int
	Goals     int
	Assists   int
	Saves     int
	Shots     int
	Demos     int
}

// RoleAccumulator aggregates unique matches and totals for a single role ("teammate" or "opponent").
type RoleAccumulator struct {
	seenGUIDs map[string]struct{}
	Games     int
	Wins      int
	Losses    int
	Score     int
	Goals     int
	Assists   int
	Saves     int
	Shots     int
	Demos     int
}

// Add folds a MatchPlayerRow into the accumulator, deduplicating identical match GUIDs.
func (a *RoleAccumulator) Add(r MatchPlayerRow) {
	if r.MatchGUID != "" {
		if a.seenGUIDs == nil {
			a.seenGUIDs = make(map[string]struct{})
		}
		if _, exists := a.seenGUIDs[r.MatchGUID]; exists {
			return
		}
		a.seenGUIDs[r.MatchGUID] = struct{}{}
	}
	a.Games++
	switch strings.ToLower(strings.TrimSpace(r.Outcome)) {
	case "victory", "win":
		a.Wins++
	case "defeat", "loss":
		a.Losses++
	}
	a.Score += r.Score
	a.Goals += r.Goals
	a.Assists += r.Assists
	a.Saves += r.Saves
	a.Shots += r.Shots
	a.Demos += r.Demos
}

// Breakdown materializes per-game averages and shooting percentage into the generated contract DTO.
func (a *RoleAccumulator) Breakdown() contracts.RoleStatsBreakdown {
	out := contracts.RoleStatsBreakdown{
		Games:   a.Games,
		Wins:    a.Wins,
		Losses:  a.Losses,
		WinRate: ComputeWinRate(a.Wins, a.Games),
		Goals:   a.Goals,
		Assists: a.Assists,
		Saves:   a.Saves,
		Shots:   a.Shots,
		Demos:   a.Demos,
		Score:   a.Score,
	}
	if a.Games > 0 {
		g := float64(a.Games)
		out.AvgGoals = RoundFloat(float64(a.Goals)/g, 2)
		out.AvgAssists = RoundFloat(float64(a.Assists)/g, 2)
		out.AvgSaves = RoundFloat(float64(a.Saves)/g, 2)
		out.AvgShots = RoundFloat(float64(a.Shots)/g, 2)
		out.AvgDemos = RoundFloat(float64(a.Demos)/g, 2)
		out.AvgScore = RoundFloat(float64(a.Score)/g, 2)
	}
	if a.Shots > 0 {
		out.ShotPct = RoundFloat((float64(a.Goals)/float64(a.Shots))*100.0, 1)
	}
	return out
}

// BuildRoleStatsSummary partitions MatchPlayerRow entries into teammate vs opponent breakdowns.
func BuildRoleStatsSummary(rows []MatchPlayerRow) contracts.PlayerRoleStatsSummary {
	var tm, opp RoleAccumulator
	for _, r := range rows {
		switch strings.ToLower(strings.TrimSpace(r.Role)) {
		case "teammate", "with":
			tm.Add(r)
		case "opponent", "against":
			opp.Add(r)
		}
	}
	return contracts.PlayerRoleStatsSummary{
		Teammate: tm.Breakdown(),
		Opponent: opp.Breakdown(),
	}
}

// DateRangeCutoff returns the earliest inclusive timestamp for a filter preset, or zero time for "all".
func DateRangeCutoff(dateRange string, now time.Time) time.Time {
	switch strings.ToLower(strings.TrimSpace(dateRange)) {
	case "", "all":
		return time.Time{}
	case "7d", "week":
		return now.UTC().Add(-7 * 24 * time.Hour)
	case "30d", "month":
		return now.UTC().Add(-30 * 24 * time.Hour)
	case "90d", "season":
		return now.UTC().Add(-90 * 24 * time.Hour)
	default:
		return time.Time{}
	}
}
