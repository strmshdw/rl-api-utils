package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
)

func TestSaveTokensToFile_YAMLPreservesCommentsAndStructure(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	initialYAML := `# Rocket League Sync Configuration
auth:
  provider: "epic"
  # Epic Games Settings
  epic:
    refresh_token: "old-refresh-token" # Long term token
    account_id: "acct-123"

# Polling Account Settings
polling_auth:
  enabled: true
  provider: "steam"
  steam:
    session_ticket: ""
    steam_id_64: ""
    login_key: ""
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	newEpicToken := "new-epic-token-xyz"
	newTicket := "01000000DEADBEEF"
	newSteamID := "76561198000000001"
	newLoginKey := "remembered-key-abc"

	updates := config.TokenUpdates{
		Primary: config.AuthTokenUpdate{
			EpicRefreshToken: &newEpicToken,
		},
		Polling: config.AuthTokenUpdate{
			SteamSessionTicket: &newTicket,
			SteamID64:          &newSteamID,
			SteamLoginKey:      &newLoginKey,
		},
	}

	if err := config.SaveTokensToFile(configPath, updates); err != nil {
		t.Fatalf("SaveTokensToFile returned error: %v", err)
	}

	updatedBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}

	updatedStr := string(updatedBytes)

	// Verify updated values are present
	if !strings.Contains(updatedStr, "new-epic-token-xyz") {
		t.Errorf("expected new-epic-token-xyz in YAML output, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "01000000DEADBEEF") {
		t.Errorf("expected 01000000DEADBEEF in YAML output, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "76561198000000001") {
		t.Errorf("expected 76561198000000001 in YAML output, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "remembered-key-abc") {
		t.Errorf("expected remembered-key-abc in YAML output, got:\n%s", updatedStr)
	}

	// Verify comments were preserved
	if !strings.Contains(updatedStr, "# Rocket League Sync Configuration") {
		t.Errorf("expected header comment preserved in YAML, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "# Epic Games Settings") {
		t.Errorf("expected Epic comment preserved in YAML, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "# Polling Account Settings") {
		t.Errorf("expected Polling comment preserved in YAML, got:\n%s", updatedStr)
	}
}
