package playertrack

import (
	"context"
	"fmt"
	"strings"

	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/statsapi"
)

// ClassifyPlayer returns a player's relationship classification relative to the local team.
func ClassifyPlayer(p LobbyPlayer, localTeam *int) PlayerClassification {
	if p.IsLocal {
		return ClassificationSelf
	}
	if p.IsBot {
		return ClassificationBot
	}
	if localTeam != nil && (*localTeam == 0 || *localTeam == 1) {
		if p.TeamNum == *localTeam {
			return ClassificationTeammate
		}
		if p.TeamNum == 0 || p.TeamNum == 1 {
			return ClassificationOpponent
		}
	}
	return ClassificationSpectator
}

// ParsePlayerPlatform extracts the normalized platform segment from a composite player ID.
func ParsePlayerPlatform(primaryID string) string {
	parsed, _ := domain.ParsePrimaryID(primaryID)
	return parsed.Platform
}

// ResolveLocalPlayer runs the local player resolution hierarchy.
// Exported for testing and diagnostic visibility.
func (t *Tracker) ResolveLocalPlayer(players []statsapi.StatsPlayer) (*ResolvedPlayer, int) {
	return t.resolveLocalPlayer(context.Background(), players)
}

func (t *Tracker) resolveLocalPlayer(ctx context.Context, players []statsapi.StatsPlayer) (*ResolvedPlayer, int) {
	var humanPlayers []statsapi.StatsPlayer
	for _, p := range players {
		if !p.IsBot() {
			humanPlayers = append(humanPlayers, p)
		}
	}
	if len(humanPlayers) == 0 {
		return nil, -1
	}

	// Tier 1: Explicit configured LocalPlayerID
	if configuredID := strings.TrimSpace(t.cfg.LocalPlayerID); configuredID != "" {
		for _, p := range humanPlayers {
			if reason, ok := matchConfiguredPlayerID(p, configuredID); ok {
				return t.buildResolvedPlayer(p, reason), p.TeamNum
			}
		}
	}

	// Tier 2: Primary non-polling Auth account credentials (config, provider, or auth store)
	authPlatform, authAccountID := t.resolveAuthCredentials(ctx)
	if authPlatform != "" && authAccountID != "" {
		for _, p := range humanPlayers {
			if parsed, err := domain.ParsePrimaryID(p.PrimaryId); err == nil {
				if strings.EqualFold(parsed.Platform, authPlatform) && strings.EqualFold(parsed.AccountID, authAccountID) {
					return t.buildResolvedPlayer(p, "Tier 2: Primary Auth ID"), p.TeamNum
				}
			}
			expectedPrimaryID := fmt.Sprintf("%s|%s|0", authPlatform, authAccountID)
			if strings.EqualFold(strings.TrimSpace(p.PrimaryId), expectedPrimaryID) {
				return t.buildResolvedPlayer(p, "Tier 2: Primary Auth ID (Exact match)"), p.TeamNum
			}
		}
	}

	// Tier 2b: Store historical frequency local IDs (excludes *_polling providers per 3744274)
	if t.store != nil {
		if knownIDs, err := t.store.GetLocalPlayerIDs(ctx); err == nil {
			for _, knownID := range knownIDs {
				knownID = strings.TrimSpace(knownID)
				if knownID == "" {
					continue
				}
				for _, p := range humanPlayers {
					if _, ok := matchConfiguredPlayerID(p, knownID); ok {
						return t.buildResolvedPlayer(p, "Tier 2b: Store LocalPlayerID"), p.TeamNum
					}
				}
			}
		}
	}

	// Tier 3: Configured LocalPlayerName
	if configuredName := strings.TrimSpace(t.cfg.LocalPlayerName); configuredName != "" {
		for _, p := range humanPlayers {
			if strings.EqualFold(strings.TrimSpace(p.Name), configuredName) {
				return t.buildResolvedPlayer(p, "Tier 3: LocalPlayerName"), p.TeamNum
			}
		}
	}

	// Tier 4: Primary Auth Display Name
	if authDisplayName := t.resolveAuthDisplayName(ctx); authDisplayName != "" {
		for _, p := range humanPlayers {
			if strings.EqualFold(strings.TrimSpace(p.Name), authDisplayName) {
				return t.buildResolvedPlayer(p, "Tier 4: Auth Display Name"), p.TeamNum
			}
		}
	}

	return nil, -1
}

func matchConfiguredPlayerID(p statsapi.StatsPlayer, targetID string) (string, bool) {
	trimmedRaw := strings.TrimSpace(p.PrimaryId)
	if strings.EqualFold(trimmedRaw, targetID) {
		return "Tier 1: LocalPlayerID", true
	}
	parsed, err := domain.ParsePrimaryID(trimmedRaw)
	if err == nil && strings.EqualFold(strings.TrimSpace(parsed.AccountID), targetID) {
		return "Tier 1: LocalPlayerID (AccountID match)", true
	}
	if strings.HasPrefix(strings.ToLower(trimmedRaw), strings.ToLower(targetID)+"|") {
		return "Tier 1: LocalPlayerID (Prefix match)", true
	}
	return "", false
}

func (t *Tracker) buildResolvedPlayer(p statsapi.StatsPlayer, reason string) *ResolvedPlayer {
	parsed, _ := domain.ParsePrimaryID(p.PrimaryId)
	return &ResolvedPlayer{
		PrimaryID:   p.PrimaryId,
		Platform:    parsed.Platform,
		AccountID:   parsed.AccountID,
		PlayerName:  p.Name,
		TeamNum:     p.TeamNum,
		MatchReason: reason,
	}
}

func (t *Tracker) getLocalPlayerIDsLocked() []string {
	seen := make(map[string]bool)
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	if t.cfg.LocalPlayerID != "" {
		add(t.cfg.LocalPlayerID)
	}
	if t.currentMatch != nil && t.currentMatch.LocalPlayer != nil {
		add(t.currentMatch.LocalPlayer.PlayerID)
	}
	if t.cfg.LocalPlayerID == "" {
		if t.authCfg.Epic.AccountID != "" {
			add(domain.FormatCompositeID("Epic", t.authCfg.Epic.AccountID))
			add(t.authCfg.Epic.AccountID)
		}
		if t.authCfg.Steam.SteamID64 != "" {
			add(domain.FormatCompositeID("Steam", t.authCfg.Steam.SteamID64))
			add(t.authCfg.Steam.SteamID64)
		}
	}
	return ids
}

// resolveAuthCredentials resolves non-polling primary auth credentials.
func (t *Tracker) resolveAuthCredentials(ctx context.Context) (platform, accountID string) {
	provider := strings.ToLower(strings.TrimSpace(t.authCfg.Provider))
	if strings.HasSuffix(provider, "_polling") {
		return "", ""
	}
	switch provider {
	case "epic":
		platform = "Epic"
		accountID = strings.TrimSpace(t.authCfg.Epic.AccountID)
		if accountID == "" && t.authProvider != nil {
			if tok := t.authProvider.TokenInfo(); tok != nil {
				accountID = strings.TrimSpace(tok.EpicAccountID)
				if accountID == "" {
					accountID = strings.TrimSpace(tok.AccountID)
				}
			}
		}
		if accountID == "" && t.store != nil {
			_, acct, _, _ := t.store.GetAuthState(ctx, "epic")
			accountID = strings.TrimSpace(acct)
		}
	case "steam":
		platform = "Steam"
		accountID = strings.TrimSpace(t.authCfg.Steam.SteamID64)
		if accountID == "" && t.authProvider != nil {
			if tok := t.authProvider.TokenInfo(); tok != nil {
				accountID = strings.TrimSpace(tok.AccountID)
			}
		}
		if accountID == "" && t.store != nil {
			_, acct, _, _ := t.store.GetAuthState(ctx, "steam")
			accountID = strings.TrimSpace(acct)
		}
	}
	return platform, accountID
}

// resolveAuthDisplayName resolves non-polling primary auth display name.
func (t *Tracker) resolveAuthDisplayName(ctx context.Context) string {
	provider := strings.ToLower(strings.TrimSpace(t.authCfg.Provider))
	if strings.HasSuffix(provider, "_polling") {
		return ""
	}
	var cfgName string
	switch provider {
	case "epic":
		cfgName = strings.TrimSpace(t.authCfg.Epic.DisplayName)
	case "steam":
		cfgName = strings.TrimSpace(t.authCfg.Steam.AccountName)
	default:
		return ""
	}
	if cfgName != "" {
		return cfgName
	}
	if t.authProvider != nil {
		if tok := t.authProvider.TokenInfo(); tok != nil && strings.TrimSpace(tok.DisplayName) != "" {
			return strings.TrimSpace(tok.DisplayName)
		}
	}
	if t.store != nil {
		_, _, display, _ := t.store.GetAuthState(ctx, provider)
		return strings.TrimSpace(display)
	}
	return ""
}
