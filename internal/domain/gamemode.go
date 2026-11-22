package domain

import (
	"fmt"
	"strings"
)

// Canonical game mode buckets used for replay file naming and Ballchasing group naming.
const (
	GameMode1v1   = "1v1"
	GameMode2v2   = "2v2"
	GameMode3v3   = "3v3"
	GameModeExtra = "Extra"
)

// Canonical match outcome suffixes used for replay file naming.
const (
	ReplayResultWin     = "WIN"
	ReplayResultLoss    = "LOSS"
	ReplayResultUnknown = "UNKNOWN"
)

// GameModeCategory maps a Rocket League playlist ID (and optional replay TeamSize fallback)
// to one of the four canonical buckets: "1v1", "2v2", "3v3", or "Extra".
func GameModeCategory(playlistID, teamSize int) string {
	switch playlistID {
	case 1, 10:
		return GameMode1v1
	case 2, 11:
		return GameMode2v2
	case 3, 12, 13:
		return GameMode3v3
	case 0:
		switch teamSize {
		case 1:
			return GameMode1v1
		case 2:
			return GameMode2v2
		case 3:
			return GameMode3v3
		default:
			return GameModeExtra
		}
	default:
		return GameModeExtra
	}
}

// NormalizeGameModeCategory normalizes a game mode string to one of "1v1", "2v2", "3v3", or "Extra".
func NormalizeGameModeCategory(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1v1", "duel", "duels":
		return GameMode1v1
	case "2v2", "doubles":
		return GameMode2v2
	case "3v3", "standard":
		return GameMode3v3
	default:
		return GameModeExtra
	}
}

// NormalizeReplayResult normalizes a match outcome string into "WIN", "LOSS", or "UNKNOWN".
func NormalizeReplayResult(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "win", "w", "victory", "won":
		return ReplayResultWin
	case "loss", "l", "defeat", "lost":
		return ReplayResultLoss
	default:
		return ReplayResultUnknown
	}
}

// FormatReplayBaseName returns the canonical replay base name (without .replay extension):
// YYYY_MM_DD_XXX_GameMode_Result (e.g., "2026_11_21_001_2v2_WIN").
func FormatReplayBaseName(dateKey string, dailyGameNum int, gameMode, result string) string {
	if dailyGameNum < 1 {
		dailyGameNum = 1
	}
	mode := NormalizeGameModeCategory(gameMode)
	res := NormalizeReplayResult(result)
	return fmt.Sprintf("%s_%03d_%s_%s", strings.TrimSpace(dateKey), dailyGameNum, mode, res)
}

// FormatReplayFileName returns the canonical replay filename including the .replay extension:
// YYYY_MM_DD_XXX_GameMode_Result.replay (e.g., "2026_11_21_001_2v2_WIN.replay").
func FormatReplayFileName(dateKey string, dailyGameNum int, gameMode, result string) string {
	return FormatReplayBaseName(dateKey, dailyGameNum, gameMode, result) + ".replay"
}

// FormatBallchasingGroupName returns the canonical Ballchasing group name for a session and game mode:
// YYYY_MM_DD_SessionNum_GameMode (e.g., "2026_11_21_1_2v2").
func FormatBallchasingGroupName(dateKey string, sessionNum int, gameMode string) string {
	if sessionNum < 1 {
		sessionNum = 1
	}
	mode := NormalizeGameModeCategory(gameMode)
	return fmt.Sprintf("%s_%d_%s", strings.TrimSpace(dateKey), sessionNum, mode)
}
