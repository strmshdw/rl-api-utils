package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/replay"
	"github.com/dank/rl-api-utils/internal/storage"
)

type ballchasingTelemetryResult struct {
	stats        *storage.ReplayMatchStats
	candidates   []ReplayPlayerCandidate
	matchPlayers []*storage.MatchPlayerRecord
	outcomes     []storage.PlayerOutcome
	playlistID   int
	hasDate      bool
	matchTime    time.Time
}

type replayPlayerEntry struct {
	team int
	p    ballchasing.ReplayPlayer
}

func normalizePlatform(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "epic":
		return "Epic"
	case "steam":
		return "Steam"
	case "ps4", "ps5", "playstation":
		return "PlayStation"
	case "xbox", "xboxone", "xboxseries":
		return "Xbox"
	case "switch", "nintendo":
		return "Switch"
	default:
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return "Unknown"
		}
		return strings.ToUpper(trimmed[:1]) + strings.ToLower(trimmed[1:])
	}
}

func canonicalPlayerID(platform, onlineID string) string {
	cleanID := strings.TrimSpace(onlineID)
	if cleanID == "" || strings.Contains(cleanID, "|") {
		return cleanID
	}
	return fmt.Sprintf("%s|%s|0", platform, cleanID)
}

func buildReplayPlayerStats(team int, p ballchasing.ReplayPlayer) storage.ReplayPlayerStats {
	plat := normalizePlatform(p.ID.Platform)
	onlineID := p.ID.ID
	demos := p.Demos.Inflicted
	if demos == 0 && p.Stats != nil && p.Stats.Demo != nil {
		demos = p.Stats.Demo.Inflicted
	}

	rps := storage.ReplayPlayerStats{
		Name:                p.Name,
		Platform:            plat,
		OnlineID:            onlineID,
		Team:                team,
		Score:               p.Score,
		Goals:               p.Goals,
		Assists:             p.Assists,
		Saves:               p.Saves,
		Shots:               p.Shots,
		Demos:               demos,
		IsBot:               false,
		CarID:               p.CarID,
		CarName:             p.CarName,
		MVP:                 p.MVP,
		Camera:              p.Camera,
		SteeringSensitivity: p.SteeringSensitivity,
		Rank:                p.Rank,
	}
	if p.Stats != nil {
		rps.Core = p.Stats.Core
		rps.Boost = p.Stats.Boost
		rps.Movement = p.Stats.Movement
		rps.Positioning = p.Stats.Positioning
		rps.Demo = p.Stats.Demo
		rps.Ball = p.Stats.Ball
	}
	return rps
}

func buildBallchasingTelemetry(
	guid string,
	replayID string,
	playlistID int,
	detail *ballchasing.ReplayItem,
	listItem *ballchasing.ReplayItem,
	identity LocalPlayerIdentity,
) *ballchasingTelemetryResult {
	blueGoals := detail.Blue.Goals
	orangeGoals := detail.Orange.Goals
	if blueGoals == 0 && orangeGoals == 0 && listItem != nil && (listItem.Blue.Goals > 0 || listItem.Orange.Goals > 0) {
		blueGoals = listItem.Blue.Goals
		orangeGoals = listItem.Orange.Goals
	}
	if blueGoals == 0 && orangeGoals == 0 {
		for _, p := range detail.Blue.Players {
			blueGoals += p.Goals
		}
		for _, p := range detail.Orange.Players {
			orangeGoals += p.Goals
		}
	}

	var winnerTeam *int
	if blueGoals > orangeGoals {
		w := 0
		winnerTeam = &w
	} else if orangeGoals > blueGoals {
		w := 1
		winnerTeam = &w
	}

	dateStr := detail.Date
	if dateStr == "" {
		dateStr = detail.Created
	}
	if dateStr == "" && listItem != nil {
		dateStr = listItem.Date
	}
	matchTime, hasDate := storage.ParseReplayDate(dateStr)

	var allPlayers []replayPlayerEntry
	for _, p := range detail.Blue.Players {
		allPlayers = append(allPlayers, replayPlayerEntry{team: 0, p: p})
	}
	for _, p := range detail.Orange.Players {
		allPlayers = append(allPlayers, replayPlayerEntry{team: 1, p: p})
	}

	var (
		candidates  []ReplayPlayerCandidate
		playerStats []storage.ReplayPlayerStats
	)
	for _, entry := range allPlayers {
		rps := buildReplayPlayerStats(entry.team, entry.p)
		canonicalID := canonicalPlayerID(rps.Platform, rps.OnlineID)
		candidates = append(candidates, ReplayPlayerCandidate{
			Name:        entry.p.Name,
			Platform:    rps.Platform,
			OnlineID:    rps.OnlineID,
			CanonicalID: canonicalID,
			Team:        entry.team,
			IsBot:       false,
		})
		playerStats = append(playerStats, rps)
	}

	localIdx, localTeam, found := ResolveLocalPlayer(candidates, identity)

	var matchPlayers []*storage.MatchPlayerRecord
	for i, entry := range allPlayers {
		c := candidates[i]
		playerID := c.CanonicalID
		if playerID == "" {
			playerID = c.Name
		}

		isLocal := (found && i == localIdx) || (localTeam != nil && c.Team == *localTeam && identity.ConfigLocalPlayerName != "" && strings.EqualFold(c.Name, identity.ConfigLocalPlayerName))
		playerStats[i].IsLocal = isLocal

		var won *bool
		if winnerTeam != nil {
			w := (c.Team == *winnerTeam)
			won = &w
		}

		var pStatsJSON string
		if b, err := json.Marshal(playerStats[i]); err == nil {
			pStatsJSON = string(b)
		}

		matchPlayers = append(matchPlayers, &storage.MatchPlayerRecord{
			MatchGUID:  guid,
			PlayerID:   playerID,
			Platform:   c.Platform,
			PlayerName: c.Name,
			TeamNum:    c.Team,
			IsLocal:    isLocal,
			Won:        won,
			Score:      entry.p.Score,
			Goals:      entry.p.Goals,
			Assists:    entry.p.Assists,
			Saves:      entry.p.Saves,
			Shots:      entry.p.Shots,
			Demos:      playerStats[i].Demos,
			CarName:    entry.p.CarName,
			StatsJSON:  pStatsJSON,
		})
	}

	bcLink := detail.Link
	if bcLink == "" && listItem != nil {
		bcLink = listItem.Link
	}
	if bcLink == "" || strings.Contains(bcLink, "/api/replays/") {
		bcLink = fmt.Sprintf("https://ballchasing.com/replay/%s", replayID)
	}

	outcomes := buildOutcomesFromCandidates(candidates, localIdx, localTeam, winnerTeam, found, identity.ConfigLocalPlayerName, identity.ConfigLocalPlayerID)

	return &ballchasingTelemetryResult{
		stats: &storage.ReplayMatchStats{
			MatchGUID:      guid,
			MapName:        detail.MapName,
			Playlist:       playlistID,
			BlueScore:      blueGoals,
			OrangeScore:    orangeGoals,
			WinnerTeam:     winnerTeam,
			Duration:       detail.Duration,
			Overtime:       detail.Overtime,
			Date:           dateStr,
			Players:        playerStats,
			BallchasingID:  replayID,
			BallchasingURL: bcLink,
		},
		candidates:   candidates,
		matchPlayers: matchPlayers,
		outcomes:     outcomes,
		playlistID:   playlistID,
		hasDate:      hasDate,
		matchTime:    matchTime,
	}
}

func buildOutcomesFromCandidates(
	candidates []ReplayPlayerCandidate,
	localIdx int,
	localTeam *int,
	winnerTeam *int,
	found bool,
	localName string,
	localID string,
) []storage.PlayerOutcome {
	if localTeam == nil || winnerTeam == nil {
		return nil
	}
	won := (*localTeam == *winnerTeam)
	var outcomes []storage.PlayerOutcome
	for i, c := range candidates {
		if c.IsBot || (found && i == localIdx) {
			continue
		}
		if localID != "" && strings.EqualFold(c.CanonicalID, localID) {
			continue
		}
		if localName != "" && c.Team == *localTeam && strings.EqualFold(c.Name, localName) {
			continue
		}
		if c.CanonicalID == "" || c.OnlineID == "0" || strings.HasSuffix(c.CanonicalID, "|0|0") {
			continue
		}
		outcomes = append(outcomes, storage.PlayerOutcome{
			PlayerID:   c.CanonicalID,
			Platform:   c.Platform,
			PlayerName: c.Name,
			IsTeammate: c.Team == *localTeam,
			Won:        won,
		})
	}
	return outcomes
}

func recordOutcomesIfNonEmpty(
	ctx context.Context,
	store StateStore,
	logger *slog.Logger,
	sourceDesc string,
	matchGUID string,
	playlistID int,
	outcomes []storage.PlayerOutcome,
	hasDate bool,
	matchTime time.Time,
) {
	if len(outcomes) == 0 {
		return
	}
	var matchTimes []time.Time
	if hasDate {
		matchTimes = []time.Time{matchTime}
	}
	if err := store.RecordMatchResults(ctx, matchGUID, playlistID, outcomes, matchTimes...); err != nil && !errors.Is(err, storage.ErrMatchAlreadyProcessed) {
		logger.Warn("failed to record match results from "+sourceDesc,
			slog.String("guid", matchGUID),
			slog.Any("error", err),
		)
	}
}

// SyncDownloadedReplayStats parses the downloaded replay file and merges replay statistics
// into the local store keyed by matchGUID, sets up match players, and records matchup results.
func (s *Syncer) SyncDownloadedReplayStats(ctx context.Context, matchGUID, localPath string) error {
	meta, err := replay.ParseReplayFile(localPath)
	if err != nil {
		return fmt.Errorf("parsing replay file: %w", err)
	}

	matchTime, hasDate := storage.ParseReplayDate(meta.Date)

	playlist := 0
	if existing, _ := s.store.GetMatch(ctx, matchGUID); existing != nil {
		playlist = existing.Playlist
	}

	var (
		candidates  []ReplayPlayerCandidate
		replayStats []storage.ReplayPlayerStats
	)
	for _, p := range meta.Players {
		plat := normalizePlatform(p.Platform)
		canonicalID := canonicalPlayerID(plat, p.OnlineID)
		candidates = append(candidates, ReplayPlayerCandidate{
			Name:        p.Name,
			Platform:    plat,
			OnlineID:    p.OnlineID,
			CanonicalID: canonicalID,
			Team:        p.Team,
			IsBot:       p.IsBot,
		})
		replayStats = append(replayStats, storage.ReplayPlayerStats{
			Name:     p.Name,
			Platform: plat,
			OnlineID: p.OnlineID,
			Team:     p.Team,
			Score:    p.Score,
			Goals:    p.Goals,
			Assists:  p.Assists,
			Saves:    p.Saves,
			Shots:    p.Shots,
			Demos:    p.Demos,
			IsBot:    p.IsBot,
		})
	}

	identity := s.gatherLocalPlayerIdentity(ctx, meta.PlayerName, meta.LocalTeam, nil)
	localIdx, localTeam, found := ResolveLocalPlayer(candidates, identity)

	var matchPlayers []*storage.MatchPlayerRecord
	for i, p := range meta.Players {
		c := candidates[i]
		playerID := c.CanonicalID
		if playerID == "" {
			playerID = p.Name
		}

		isLocal := (found && i == localIdx) || (localTeam != nil && p.Team == *localTeam && strings.EqualFold(p.Name, meta.PlayerName))
		replayStats[i].IsLocal = isLocal

		var won *bool
		if meta.WinnerTeam != nil {
			w := (p.Team == *meta.WinnerTeam)
			won = &w
		}

		matchPlayers = append(matchPlayers, &storage.MatchPlayerRecord{
			MatchGUID:  matchGUID,
			PlayerID:   playerID,
			Platform:   c.Platform,
			PlayerName: p.Name,
			TeamNum:    p.Team,
			IsLocal:    isLocal,
			Won:        won,
			Score:      p.Score,
			Goals:      p.Goals,
			Assists:    p.Assists,
			Saves:      p.Saves,
			Shots:      p.Shots,
			Demos:      p.Demos,
		})
	}

	st := &storage.ReplayMatchStats{
		MatchGUID:   matchGUID,
		MapName:     meta.MapName,
		Playlist:    playlist,
		BlueScore:   meta.Team0Score,
		OrangeScore: meta.Team1Score,
		WinnerTeam:  meta.WinnerTeam,
		Duration:    int(meta.TotalSeconds),
		Overtime:    meta.Overtime,
		Date:        meta.Date,
		Players:     replayStats,
	}

	if err := s.store.SyncReplayStats(ctx, st); err != nil {
		return fmt.Errorf("storing synced replay stats: %w", err)
	}

	if len(matchPlayers) > 0 {
		_ = s.store.SaveMatchPlayers(ctx, matchGUID, matchPlayers)
	}

	outcomes := buildOutcomesFromCandidates(candidates, localIdx, localTeam, meta.WinnerTeam, found, meta.PlayerName, "")
	recordOutcomesIfNonEmpty(ctx, s.store, s.logger, "downloaded replay", matchGUID, playlist, outcomes, hasDate, matchTime)

	s.logger.Info("synced replay stats into local store",
		slog.String("guid", matchGUID),
		slog.Int("players", len(replayStats)),
		slog.Int("blue_score", meta.Team0Score),
		slog.Int("orange_score", meta.Team1Score),
	)

	return nil
}
