package auth

import (
	"github.com/dank/rl-api-utils/internal/config"
)

// ConfigTokenSaver defines a function callback that updates tokens in configuration files.
type ConfigTokenSaver func(updates config.TokenUpdates) error

// NewConfigTokenSaver returns a ConfigTokenSaver targeting the given config file path.
// If configPath is empty, it returns a no-op function.
func NewConfigTokenSaver(configPath string) ConfigTokenSaver {
	if configPath == "" {
		return func(updates config.TokenUpdates) error {
			return nil
		}
	}
	return func(updates config.TokenUpdates) error {
		return config.SaveTokensToFile(configPath, updates)
	}
}

// BuildEpicTokenUpdate constructs a TokenUpdates payload for Epic Games refresh token based on AccountRole.
func BuildEpicTokenUpdate(role AccountRole, refreshToken string) config.TokenUpdates {
	var updates config.TokenUpdates
	if role == RolePolling {
		updates.Polling.EpicRefreshToken = &refreshToken
	} else {
		updates.Primary.EpicRefreshToken = &refreshToken
	}
	return updates
}

// BuildSteamTokenUpdate constructs a TokenUpdates payload for Steam credentials based on AccountRole.
func BuildSteamTokenUpdate(role AccountRole, ticket, steamID, loginKey string) config.TokenUpdates {
	var updates config.TokenUpdates
	var u *config.AuthTokenUpdate
	if role == RolePolling {
		u = &updates.Polling
	} else {
		u = &updates.Primary
	}
	if ticket != "" {
		u.SteamSessionTicket = &ticket
	}
	if steamID != "" {
		u.SteamID64 = &steamID
	}
	if loginKey != "" {
		u.SteamLoginKey = &loginKey
	}
	return updates
}
