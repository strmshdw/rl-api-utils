package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParsedPlayerID holds the parsed components of a composite Rocket League ID (<Platform>|<AccountID>|<Splitscreen>).
type ParsedPlayerID struct {
	Platform    string
	AccountID   string
	Splitscreen int
	Raw         string
	IsBot       bool
}

// ParsePrimaryID parses "<Platform>|<AccountID>|<Splitscreen>" identifiers.
func ParsePrimaryID(raw string) (ParsedPlayerID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ParsedPlayerID{}, errors.New("empty primary ID")
	}
	parts := strings.Split(trimmed, "|")
	if len(parts) != 3 {
		return ParsedPlayerID{Raw: trimmed}, fmt.Errorf("invalid PrimaryId format %q: expected 3 pipe-separated segments", raw)
	}
	platform := strings.TrimSpace(parts[0])
	accountID := strings.TrimSpace(parts[1])
	splitscreen, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		return ParsedPlayerID{Raw: trimmed, Platform: platform, AccountID: accountID}, fmt.Errorf("invalid splitscreen index in %q: %w", raw, err)
	}
	isBot := strings.EqualFold(platform, "Unknown") || accountID == "0"
	return ParsedPlayerID{
		Platform:    platform,
		AccountID:   accountID,
		Splitscreen: splitscreen,
		Raw:         trimmed,
		IsBot:       isBot,
	}, nil
}

// IsBotID reports whether a composite player ID represents an AI bot.
func IsBotID(raw string) bool {
	parsed, err := ParsePrimaryID(raw)
	if err != nil {
		return strings.EqualFold(strings.TrimSpace(raw), "Unknown|0|0")
	}
	return parsed.IsBot
}

// NormalizeID normalizes an identifier for case-insensitive comparison.
func NormalizeID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

// CanonicalLocalAlias reports whether an HTTP lookup token asks for the authenticated primary player.
func CanonicalLocalAlias(id string) bool {
	norm := NormalizeID(id)
	return norm == "me" || norm == "you"
}

// FormatCompositeID returns "<Platform>|<AccountID>|<Splitscreen>" or empty string if inputs are incomplete.
func FormatCompositeID(platform, accountID string) string {
	p := strings.TrimSpace(platform)
	a := strings.TrimSpace(accountID)
	if p == "" || a == "" {
		return ""
	}
	return p + "|" + a + "|0"
}
