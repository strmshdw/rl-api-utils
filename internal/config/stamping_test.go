package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStampingConfigDefaultsClampPatchAndPersist(t *testing.T) {
	def := DefaultStampingConfig()
	if !def.AltDetectionEnabled || def.AltMinTier != 12 || def.AltHighSigma != 3.35 ||
		def.AltHighSigmaMaxGames != 35 || def.AltStreakSigma != 2.75 || def.AltStreakMaxGames != 45 ||
		def.AltMinWinStreak != 4 || !def.ShowOffModePeak || def.OffModeMinGames != 15 ||
		def.OffModeSelfTierDiff != 2.0 || def.OffModeVsYouTierDiff != 1.5 ||
		def.CarryMmrDiff != 100 || def.HigherMmrDiff != 60 || !def.ShowHotStreak ||
		def.HotStreakMinWins != 4 || def.ShowTeammateOffModePeak || def.RelationshipMinGames != 3 {
		t.Fatalf("unexpected DefaultStampingConfig: %+v", def)
	}

	// Zero-value Config should safely return DefaultStampingConfig via EffectiveStampingConfig
	zeroCfg := &Config{}
	if eff := zeroCfg.EffectiveStampingConfig(); eff != def {
		t.Fatalf("zeroCfg.EffectiveStampingConfig() = %+v, want %+v", eff, def)
	}

	// Out-of-range values clamp cleanly
	clamped := ClampStampingConfig(StampingConfig{
		AltMinTier:           99,
		AltHighSigma:         0.5,
		AltHighSigmaMaxGames: 9999,
		AltStreakSigma:       20.0,
		AltStreakMaxGames:    1,
		AltMinWinStreak:      0,
		OffModeMinGames:      -5,
		OffModeSelfTierDiff:  0.1,
		OffModeVsYouTierDiff: 25.0,
		CarryMmrDiff:         5,
		HigherMmrDiff:        999,
		HotStreakMinWins:     99,
		RelationshipMinGames: 0,
	})
	if clamped.AltMinTier != 22 || clamped.AltHighSigma != 2.50 || clamped.AltHighSigmaMaxGames != 250 ||
		clamped.AltStreakSigma != 8.50 || clamped.AltStreakMaxGames != 5 || clamped.AltMinWinStreak != 2 ||
		clamped.OffModeMinGames != 1 || clamped.OffModeSelfTierDiff != 0.5 || clamped.OffModeVsYouTierDiff != 10.0 ||
		clamped.CarryMmrDiff != 20 || clamped.HigherMmrDiff != 500 || clamped.HotStreakMinWins != 25 ||
		clamped.RelationshipMinGames != 1 {
		t.Fatalf("unexpected clamped StampingConfig: %+v", clamped)
	}

	// Apply patch
	carry := 140
	higher := 85
	showTeammate := true
	relMin := 5
	selfDiff := 2.5
	patched := ApplyStampingPatch(def, StampingConfigPatch{
		CarryMmrDiff:            &carry,
		HigherMmrDiff:           &higher,
		ShowTeammateOffModePeak: &showTeammate,
		RelationshipMinGames:    &relMin,
		OffModeSelfTierDiff:     &selfDiff,
	})
	if patched.CarryMmrDiff != 140 || patched.HigherMmrDiff != 85 || !patched.ShowTeammateOffModePeak ||
		patched.RelationshipMinGames != 5 || patched.OffModeSelfTierDiff != 2.5 {
		t.Fatalf("unexpected patched StampingConfig: %+v", patched)
	}

	// Persist to YAML while preserving comments
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	initialYAML := "# Top-level user comment\nauth:\n  provider: epic\nballchasing:\n  api_key: test-key\n"
	if err := os.WriteFile(yamlPath, []byte(initialYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveStampingToFile(yamlPath, patched); err != nil {
		t.Fatalf("SaveStampingToFile YAML failed: %v", err)
	}
	rawYAML, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawYAML), "# Top-level user comment") {
		t.Fatalf("SaveStampingToFile lost YAML comment:\n%s", string(rawYAML))
	}

	loaded, err := Load(CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("Load after SaveStampingToFile YAML failed: %v", err)
	}
	if loaded.EffectiveStampingConfig() != patched {
		t.Fatalf("loaded YAML stamping = %+v, want %+v", loaded.EffectiveStampingConfig(), patched)
	}

	// Persist to JSON
	jsonPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(jsonPath, []byte(`{"auth":{"provider":"epic"},"ballchasing":{"api_key":"test-key"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveStampingToFile(jsonPath, patched); err != nil {
		t.Fatalf("SaveStampingToFile JSON failed: %v", err)
	}
	loadedJSON, err := Load(CLIFlags{ConfigPath: jsonPath})
	if err != nil {
		t.Fatalf("Load after SaveStampingToFile JSON failed: %v", err)
	}
	if loadedJSON.EffectiveStampingConfig() != patched {
		t.Fatalf("loaded JSON stamping = %+v, want %+v", loadedJSON.EffectiveStampingConfig(), patched)
	}
}
