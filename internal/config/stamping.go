package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dank/rl-api-utils/internal/contracts"
	"gopkg.in/yaml.v3"
)

// StampingConfig configures multi-signal live threat, carry, peak, and relationship stamps.
type StampingConfig = contracts.StampingConfig

// StampingConfigPatch represents partial updates to StampingConfig from REST/UI mutations.
type StampingConfigPatch struct {
	AltDetectionEnabled     *bool    `json:"alt_detection_enabled"`
	AltMinTier              *int     `json:"alt_min_tier"`
	AltHighSigma            *float64 `json:"alt_high_sigma"`
	AltHighSigmaMaxGames    *int     `json:"alt_high_sigma_max_games"`
	AltStreakSigma          *float64 `json:"alt_streak_sigma"`
	AltStreakMaxGames       *int     `json:"alt_streak_max_games"`
	AltMinWinStreak         *int     `json:"alt_min_win_streak"`
	ShowOffModePeak         *bool    `json:"show_off_mode_peak"`
	OffModeMinGames         *int     `json:"off_mode_min_games"`
	OffModeSelfTierDiff     *float64 `json:"off_mode_self_tier_diff"`
	OffModeVsYouTierDiff    *float64 `json:"off_mode_vs_you_tier_diff"`
	CarryMmrDiff            *int     `json:"carry_mmr_diff"`
	HigherMmrDiff           *int     `json:"higher_mmr_diff"`
	ShowHotStreak           *bool    `json:"show_hot_streak"`
	HotStreakMinWins        *int     `json:"hot_streak_min_wins"`
	ShowTeammateOffModePeak *bool    `json:"show_teammate_off_mode_peak"`
	RelationshipMinGames    *int     `json:"relationship_min_games"`
}

// DefaultStampingConfig returns the canonical Option 5 multi-signal stamping defaults.
func DefaultStampingConfig() StampingConfig {
	return StampingConfig{
		AltDetectionEnabled:     true,
		AltMinTier:              12,
		AltHighSigma:            3.35,
		AltHighSigmaMaxGames:    35,
		AltStreakSigma:          2.75,
		AltStreakMaxGames:       45,
		AltMinWinStreak:         4,
		ShowOffModePeak:         true,
		OffModeMinGames:         15,
		OffModeSelfTierDiff:     2.0,
		OffModeVsYouTierDiff:    1.5,
		CarryMmrDiff:            100,
		HigherMmrDiff:           60,
		ShowHotStreak:           true,
		HotStreakMinWins:        4,
		ShowTeammateOffModePeak: false,
		RelationshipMinGames:    3,
	}
}

func isZeroStampingConfig(s StampingConfig) bool {
	return s == (StampingConfig{})
}

func clampInt(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func clampFloat(v, minVal, maxVal float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return math.Round(v*100) / 100
}

// ClampStampingConfig normalizes all numeric stamping thresholds into valid operational ranges.
func ClampStampingConfig(s StampingConfig) StampingConfig {
	s.AltMinTier = clampInt(s.AltMinTier, 1, 22)
	s.AltHighSigma = clampFloat(s.AltHighSigma, 2.50, 8.50)
	s.AltHighSigmaMaxGames = clampInt(s.AltHighSigmaMaxGames, 5, 250)
	s.AltStreakSigma = clampFloat(s.AltStreakSigma, 2.50, 8.50)
	s.AltStreakMaxGames = clampInt(s.AltStreakMaxGames, 5, 300)
	s.AltMinWinStreak = clampInt(s.AltMinWinStreak, 2, 25)
	s.OffModeMinGames = clampInt(s.OffModeMinGames, 1, 200)
	s.OffModeSelfTierDiff = clampFloat(s.OffModeSelfTierDiff, 0.50, 10.0)
	s.OffModeVsYouTierDiff = clampFloat(s.OffModeVsYouTierDiff, 0.50, 10.0)
	s.CarryMmrDiff = clampInt(s.CarryMmrDiff, 20, 500)
	s.HigherMmrDiff = clampInt(s.HigherMmrDiff, 10, 500)
	s.HotStreakMinWins = clampInt(s.HotStreakMinWins, 2, 25)
	s.RelationshipMinGames = clampInt(s.RelationshipMinGames, 1, 25)
	return s
}

// ApplyStampingPatch applies non-nil patch fields onto base and clamps numeric bounds.
func ApplyStampingPatch(base StampingConfig, patch StampingConfigPatch) StampingConfig {
	out := base
	if isZeroStampingConfig(out) {
		out = DefaultStampingConfig()
	}
	if patch.AltDetectionEnabled != nil {
		out.AltDetectionEnabled = *patch.AltDetectionEnabled
	}
	if patch.AltMinTier != nil {
		out.AltMinTier = *patch.AltMinTier
	}
	if patch.AltHighSigma != nil {
		out.AltHighSigma = *patch.AltHighSigma
	}
	if patch.AltHighSigmaMaxGames != nil {
		out.AltHighSigmaMaxGames = *patch.AltHighSigmaMaxGames
	}
	if patch.AltStreakSigma != nil {
		out.AltStreakSigma = *patch.AltStreakSigma
	}
	if patch.AltStreakMaxGames != nil {
		out.AltStreakMaxGames = *patch.AltStreakMaxGames
	}
	if patch.AltMinWinStreak != nil {
		out.AltMinWinStreak = *patch.AltMinWinStreak
	}
	if patch.ShowOffModePeak != nil {
		out.ShowOffModePeak = *patch.ShowOffModePeak
	}
	if patch.OffModeMinGames != nil {
		out.OffModeMinGames = *patch.OffModeMinGames
	}
	if patch.OffModeSelfTierDiff != nil {
		out.OffModeSelfTierDiff = *patch.OffModeSelfTierDiff
	}
	if patch.OffModeVsYouTierDiff != nil {
		out.OffModeVsYouTierDiff = *patch.OffModeVsYouTierDiff
	}
	if patch.CarryMmrDiff != nil {
		out.CarryMmrDiff = *patch.CarryMmrDiff
	}
	if patch.HigherMmrDiff != nil {
		out.HigherMmrDiff = *patch.HigherMmrDiff
	}
	if patch.ShowHotStreak != nil {
		out.ShowHotStreak = *patch.ShowHotStreak
	}
	if patch.HotStreakMinWins != nil {
		out.HotStreakMinWins = *patch.HotStreakMinWins
	}
	if patch.ShowTeammateOffModePeak != nil {
		out.ShowTeammateOffModePeak = *patch.ShowTeammateOffModePeak
	}
	if patch.RelationshipMinGames != nil {
		out.RelationshipMinGames = *patch.RelationshipMinGames
	}
	return ClampStampingConfig(out)
}

// EffectiveStampingConfig returns the normalized StampingConfig, falling back to DefaultStampingConfig
// when Config is nil or constructed as a zero struct literal in tests.
func (c *Config) EffectiveStampingConfig() StampingConfig {
	if c == nil || isZeroStampingConfig(c.Stamping) {
		return DefaultStampingConfig()
	}
	return ClampStampingConfig(c.Stamping)
}

// SaveStampingToFile updates the "stamping" section in the configuration file (YAML or JSON)
// while preserving comments, formatting, and unrelated fields.
// If filePath is empty or does not exist on disk, it safely returns nil.
func SaveStampingToFile(filePath string, stamping StampingConfig) error {
	if strings.TrimSpace(filePath) == "" {
		return nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading config file for stamping update %s: %w", filePath, err)
	}
	normalized := ClampStampingConfig(stamping)
	if strings.ToLower(filepath.Ext(filePath)) == ".json" {
		return saveStampingToJSON(filePath, data, normalized)
	}
	return saveStampingToYAML(filePath, data, normalized)
}

func saveStampingToJSON(filePath string, data []byte, stamping StampingConfig) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}
	raw["stamping"] = stamping
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(filePath, append(out, '\n'), 0o600)
}

func saveStampingToYAML(filePath string, data []byte, s StampingConfig) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("unmarshaling YAML AST: %w", err)
	}
	if len(root.Content) == 0 {
		return nil
	}
	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return nil
	}
	sec := findOrCreateMappingKey(docNode, "stamping")
	if sec == nil || sec.Kind != yaml.MappingNode {
		return nil
	}
	setTypedScalar(sec, "alt_detection_enabled", "!!bool", strconv.FormatBool(s.AltDetectionEnabled))
	setTypedScalar(sec, "alt_min_tier", "!!int", strconv.Itoa(s.AltMinTier))
	setTypedScalar(sec, "alt_high_sigma", "!!float", strconv.FormatFloat(s.AltHighSigma, 'f', -1, 64))
	setTypedScalar(sec, "alt_high_sigma_max_games", "!!int", strconv.Itoa(s.AltHighSigmaMaxGames))
	setTypedScalar(sec, "alt_streak_sigma", "!!float", strconv.FormatFloat(s.AltStreakSigma, 'f', -1, 64))
	setTypedScalar(sec, "alt_streak_max_games", "!!int", strconv.Itoa(s.AltStreakMaxGames))
	setTypedScalar(sec, "alt_min_win_streak", "!!int", strconv.Itoa(s.AltMinWinStreak))
	setTypedScalar(sec, "show_off_mode_peak", "!!bool", strconv.FormatBool(s.ShowOffModePeak))
	setTypedScalar(sec, "off_mode_min_games", "!!int", strconv.Itoa(s.OffModeMinGames))
	setTypedScalar(sec, "off_mode_self_tier_diff", "!!float", strconv.FormatFloat(s.OffModeSelfTierDiff, 'f', -1, 64))
	setTypedScalar(sec, "off_mode_vs_you_tier_diff", "!!float", strconv.FormatFloat(s.OffModeVsYouTierDiff, 'f', -1, 64))
	setTypedScalar(sec, "carry_mmr_diff", "!!int", strconv.Itoa(s.CarryMmrDiff))
	setTypedScalar(sec, "higher_mmr_diff", "!!int", strconv.Itoa(s.HigherMmrDiff))
	setTypedScalar(sec, "show_hot_streak", "!!bool", strconv.FormatBool(s.ShowHotStreak))
	setTypedScalar(sec, "hot_streak_min_wins", "!!int", strconv.Itoa(s.HotStreakMinWins))
	setTypedScalar(sec, "show_teammate_off_mode_peak", "!!bool", strconv.FormatBool(s.ShowTeammateOffModePeak))
	setTypedScalar(sec, "relationship_min_games", "!!int", strconv.Itoa(s.RelationshipMinGames))

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return fmt.Errorf("encoding updated YAML AST: %w", err)
	}
	_ = enc.Close()
	return AtomicWriteFile(filePath, buf.Bytes(), 0o600)
}

func setTypedScalar(mappingNode *yaml.Node, key, tag, value string) {
	for i := 0; i < len(mappingNode.Content)-1; i += 2 {
		if mappingNode.Content[i].Value == key {
			mappingNode.Content[i+1].Value = value
			mappingNode.Content[i+1].Tag = tag
			mappingNode.Content[i+1].Style = 0
			return
		}
	}
	mappingNode.Content = append(mappingNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value},
	)
}
