package domain

import (
	"fmt"

	"github.com/dank/rl-api-utils/internal/contracts"
)

// TierName returns the base name of a competitive tier (0..22), or "Unknown" out of bounds.
func TierName(tier int) string {
	if tier < 0 || tier >= len(contracts.TierNames) {
		return "Unknown"
	}
	return contracts.TierNames[tier]
}

// DivisionName returns a 0-based division string ("Division I".."Division IV"), or "" out of bounds.
func DivisionName(division int) string {
	if division < 0 || division >= len(contracts.ZeroBasedDivisionNames) {
		return ""
	}
	return contracts.ZeroBasedDivisionNames[division]
}

// FormatRank formats a tier (0..22) and 0-based division (0..3) into a human-readable rank string.
// Unranked (0) and Supersonic Legend (22) omit divisions; out-of-bounds tiers return "Unknown".
func FormatRank(tier, division int) string {
	if tier < 0 || tier >= len(contracts.TierNames) {
		return "Unknown"
	}
	base := contracts.TierNames[tier]
	if tier == 0 || tier == 22 {
		return base
	}
	if division >= 0 && division < len(contracts.ZeroBasedDivisionNames) {
		return base + " " + contracts.ZeroBasedDivisionNames[division]
	}
	return base
}

// FormatShortRank formats a tier (0..22) and 0-based division (0..3) into an abbreviated tactical label.
func FormatShortRank(tier, division int) string {
	if tier < 0 || tier >= len(contracts.ShortLiveTierNames) {
		return "UNRANKED"
	}
	base := contracts.ShortLiveTierNames[tier]
	if tier == 0 || tier == 22 {
		return base
	}
	if division >= 0 && division < len(contracts.ZeroBasedShortDivisionNames) {
		return base + " · " + contracts.ZeroBasedShortDivisionNames[division]
	}
	return base
}

// FormatStorageRank formats a tier (0..22) and 1-based division (1..4) as persisted in storage repair.
func FormatStorageRank(tier, division int) string {
	if tier < 0 || tier >= len(contracts.TierNames) {
		return fmt.Sprintf("Tier %d", tier)
	}
	base := contracts.TierNames[tier]
	if tier == 0 || tier >= 22 {
		return base
	}
	if division >= 1 && division <= len(contracts.OneBasedStorageDivisionNames) {
		return base + " " + contracts.OneBasedStorageDivisionNames[division-1]
	}
	return base
}

// FormatStorageShortRank formats a tier (0..22) and 1-based division (1..4) for storage badges.
func FormatStorageShortRank(tier, division int) string {
	if tier < 0 || tier >= len(contracts.ShortStorageTierNames) {
		return fmt.Sprintf("T%d", tier)
	}
	base := contracts.ShortStorageTierNames[tier]
	if tier == 0 || tier >= 22 {
		return base
	}
	if division >= 1 && division <= len(contracts.OneBasedShortDivisionNames) {
		return base + " · " + contracts.OneBasedShortDivisionNames[division-1]
	}
	return base
}

// FormatPlaylist returns the canonical playlist name or "Playlist <id>".
func FormatPlaylist(playlistID int) string {
	return contracts.FormatPlaylist(playlistID)
}

// FormatStoragePlaylist returns the legacy storage playlist label or "Playlist <id>".
func FormatStoragePlaylist(playlistID int) string {
	return contracts.FormatStoragePlaylist(playlistID)
}

// IsRankedPlaylist reports whether a playlist ID is competitive or tournament.
func IsRankedPlaylist(playlistID int) bool {
	return contracts.IsRankedPlaylist(playlistID)
}

// IsExtraMode reports whether a playlist ID represents an Extra Mode.
func IsExtraMode(playlistID int) bool {
	return contracts.IsExtraMode(playlistID)
}

// MapPlaylistSlugToID converts a Ballchasing playlist slug to its numeric playlist ID.
func MapPlaylistSlugToID(slug string) int {
	return contracts.MapPlaylistSlugToID(slug)
}
