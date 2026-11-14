package playertrack

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rlapi"
)

// CheckCredentialCollision inspects configuration to verify that polling_auth credentials
// do not conflict with primary auth credentials. If a collision is detected, connecting
// the secondary account would terminate the player's active game with Error 67.
func CheckCredentialCollision(primary config.AuthConfig, polling config.PollingAuthConfig) error {
	primaryProvider := strings.ToLower(strings.TrimSpace(primary.Provider))
	pollingProvider := strings.ToLower(strings.TrimSpace(polling.Provider))

	if primaryProvider == "" || pollingProvider == "" || primaryProvider != pollingProvider {
		return nil
	}

	switch primaryProvider {
	case "epic":
		primAcc := strings.TrimSpace(primary.Epic.AccountID)
		pollAcc := strings.TrimSpace(polling.Epic.AccountID)
		if primAcc != "" && pollAcc != "" && strings.EqualFold(primAcc, pollAcc) {
			return fmt.Errorf("%w: epic account_id %q matches primary account", ErrCredentialCollision, pollAcc)
		}
		primTok := strings.TrimSpace(primary.Epic.RefreshToken)
		pollTok := strings.TrimSpace(polling.Epic.RefreshToken)
		if primTok != "" && pollTok != "" && primTok == pollTok {
			return fmt.Errorf("%w: epic refresh_token matches primary account", ErrCredentialCollision)
		}
		primCode := strings.TrimSpace(primary.Epic.AuthCode)
		pollCode := strings.TrimSpace(polling.Epic.AuthCode)
		if primCode != "" && pollCode != "" && primCode == pollCode {
			return fmt.Errorf("%w: epic auth_code matches primary account", ErrCredentialCollision)
		}

	case "steam":
		primSteam := strings.TrimSpace(primary.Steam.SteamID64)
		pollSteam := strings.TrimSpace(polling.Steam.SteamID64)
		if primSteam != "" && pollSteam != "" && primSteam == pollSteam {
			return fmt.Errorf("%w: steam steam_id_64 %q matches primary account", ErrCredentialCollision, pollSteam)
		}
		primTicket := strings.TrimSpace(primary.Steam.SessionTicket)
		pollTicket := strings.TrimSpace(polling.Steam.SessionTicket)
		if primTicket != "" && pollTicket != "" && primTicket == pollTicket {
			return fmt.Errorf("%w: steam session_ticket matches primary account", ErrCredentialCollision)
		}
		primUser := strings.ToLower(strings.TrimSpace(primary.Steam.Username))
		pollUser := strings.ToLower(strings.TrimSpace(polling.Steam.Username))
		if primUser != "" && pollUser != "" && primUser == pollUser {
			return fmt.Errorf("%w: steam username %q matches primary account", ErrCredentialCollision, pollUser)
		}
	}

	return nil
}

// TierName returns the base name of a competitive tier (0..22), or "Unknown" if out of bounds.
func TierName(tier int) string {
	return domain.TierName(tier)
}

// DivisionName returns the human-readable 0-based division string ("Division I".."Division IV"), or "" if out of bounds.
func DivisionName(division int) string {
	return domain.DivisionName(division)
}

// FormatRank maps a Rocket League competitive Tier (0..22) and 0-based Division (0..3) into a human-readable string.
func FormatRank(tier, division int) string {
	return domain.FormatRank(tier, division)
}

// FormatShortRank returns an abbreviated tactical rank string (e.g., "C3 · Div II", "GC1 · Div IV", "SSL").
func FormatShortRank(tier, division int) string {
	return domain.FormatShortRank(tier, division)
}

// FormatPlaylist returns a human-readable name for a Rocket League playlist ID.
func FormatPlaylist(playlistID int) string {
	return domain.FormatPlaylist(playlistID)
}

// IsRankedPlaylist returns true if the given playlist ID corresponds to a ranked competitive mode or tournament.
func IsRankedPlaylist(playlistID int) bool {
	return domain.IsRankedPlaylist(playlistID)
}

// IsExtraMode returns true if the given playlist ID represents an Extra Mode.
func IsExtraMode(playlistID int) bool {
	return domain.IsExtraMode(playlistID)
}

// PlayerPlaylistRank stores the normalized rank and skill metrics for a specific playlist.
type PlayerPlaylistRank = contracts.PlayerPlaylistRank

// PlayerRanksSnapshot maps string-formatted playlist IDs (e.g. "11", "13") to PlayerPlaylistRank.
type PlayerRanksSnapshot map[string]PlayerPlaylistRank

// GetRank returns the PlayerPlaylistRank for a specific playlist ID, or false if not present.
func (s PlayerRanksSnapshot) GetRank(playlistID int) (PlayerRanksSnapshotRank PlayerPlaylistRank, ok bool) {
	if s == nil {
		return PlayerPlaylistRank{}, false
	}
	rank, ok := s[strconv.Itoa(playlistID)]
	return rank, ok
}

// ConvertSkillRatingToMMR converts a raw PsyNet skill rating decimal (e.g. 47.5)
// to true in-game MMR using the standard Rocket League formula: round((SkillRating * 20) + 100).
// Ratings <= 0.0 return 0.0; ratings > 150.0 are treated as already converted in-game MMR.
func ConvertSkillRatingToMMR(skillRating float64) float64 {
	if skillRating <= 0.0 {
		return 0.0
	}
	if skillRating > 150.0 {
		return skillRating
	}
	return math.Round((skillRating * 20.0) + 100.0)
}

// FromSkill constructs a PlayerPlaylistRank from an rlapi.Skill telemetry entry.
func FromSkill(s rlapi.Skill) PlayerPlaylistRank {
	return PlayerPlaylistRank{
		PlaylistID:    s.Playlist,
		PlaylistName:  FormatPlaylist(s.Playlist),
		Tier:          s.Tier,
		Division:      s.Division,
		RankName:      FormatRank(s.Tier, s.Division),
		ShortRank:     FormatShortRank(s.Tier, s.Division),
		MMR:           ConvertSkillRatingToMMR(s.MMR),
		MatchesPlayed: s.MatchesPlayed,
	}
}

// BuildRanksSnapshot constructs a PlayerRanksSnapshot from a slice of rlapi.Skill.
func BuildRanksSnapshot(skills []rlapi.Skill) PlayerRanksSnapshot {
	snapshot := make(PlayerRanksSnapshot, len(skills))
	for _, s := range skills {
		snapshot[strconv.Itoa(s.Playlist)] = FromSkill(s)
	}
	return snapshot
}

// SerializeRanksJSON serializes a slice of rlapi.Skill into a JSON string.
// If skills is nil or empty, it returns "{}" without error.
func SerializeRanksJSON(skills []rlapi.Skill) (string, error) {
	if len(skills) == 0 {
		return "{}", nil
	}
	snapshot := BuildRanksSnapshot(skills)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "{}", fmt.Errorf("failed to serialize ranks to JSON: %w", err)
	}
	return string(data), nil
}

// ParseRanksJSON parses a stored ranks_json string into a PlayerRanksSnapshot.
// Tolerates empty string or "{}" by returning an empty, non-nil map and backfills legacy fields.
func ParseRanksJSON(raw string) (PlayerRanksSnapshot, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return make(PlayerRanksSnapshot), nil
	}

	var snapshot PlayerRanksSnapshot
	if err := json.Unmarshal([]byte(trimmed), &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ranks_json: %w", err)
	}

	for key, rank := range snapshot {
		changed := false
		if rank.PlaylistID == 0 {
			if pid, err := strconv.Atoi(key); err == nil {
				rank.PlaylistID = pid
				changed = true
			}
		}
		if rank.PlaylistName == "" && rank.PlaylistID != 0 {
			rank.PlaylistName = FormatPlaylist(rank.PlaylistID)
			changed = true
		}
		if rank.RankName == "" {
			rank.RankName = FormatRank(rank.Tier, rank.Division)
			changed = true
		}
		if rank.ShortRank == "" && rank.Tier >= 0 && rank.Tier < len(contracts.ShortLiveTierNames) {
			rank.ShortRank = FormatShortRank(rank.Tier, rank.Division)
			changed = true
		}
		if rank.MMR > 0.0 && rank.MMR <= 150.0 {
			rank.MMR = ConvertSkillRatingToMMR(rank.MMR)
			changed = true
		} else if rank.MMR < 0.0 {
			rank.MMR = 0.0
			changed = true
		}
		if changed {
			snapshot[key] = rank
		}
	}

	return snapshot, nil
}

// MockSkillFetcher provides an in-memory double of SkillFetcher for unit tests and downstream packages.
type MockSkillFetcher struct {
	mu        sync.Mutex
	SkillsMap map[rlapi.PlayerID][]rlapi.Skill
	Enabled   bool
	Err       error
	Calls     [][]rlapi.PlayerID
	Closed    bool
}

// NewMockSkillFetcher creates a new initialized MockSkillFetcher.
func NewMockSkillFetcher() *MockSkillFetcher {
	return &MockSkillFetcher{
		SkillsMap: make(map[rlapi.PlayerID][]rlapi.Skill),
		Enabled:   true,
	}
}

// GetPlayersSkills simulates retrieving player skills from an in-memory map.
func (m *MockSkillFetcher) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Closed {
		return nil, ErrRankClientClosed
	}

	m.Calls = append(m.Calls, playerIDs)
	if m.Err != nil {
		return nil, m.Err
	}

	var res []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		if skills, exists := m.SkillsMap[pid]; exists {
			res = append(res, rlapi.PlayerWithSkills{
				PlayerID: pid,
				Skills:   skills,
			})
		}
	}
	return res, nil
}

// IsEnabled returns true if the mock fetcher is enabled and not closed.
func (m *MockSkillFetcher) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Enabled && !m.Closed
}

// Close marks the mock fetcher as closed.
func (m *MockSkillFetcher) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Closed = true
	return nil
}
