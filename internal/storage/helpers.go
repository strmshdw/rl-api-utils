package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
)

// NewStore initializes a StateStore backend based on the database path or URI.
// If dbPath ends in ".json", a JSONStore is returned; otherwise SQLiteStore is used.
func NewStore(dbPath string) (StateStore, error) {
	if strings.HasSuffix(strings.ToLower(dbPath), ".json") {
		return NewJSONStore(dbPath)
	}
	return NewSQLiteStore(dbPath)
}

func wrapContextErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: context canceled", err)
	}
	return err
}

// FormatPlaylistName returns a standard display name for Rocket League playlist IDs.
func FormatPlaylistName(id int) string {
	return domain.FormatStoragePlaylist(id)
}

// FormatRankName returns a human-readable rank label for tier (0..22) and division (1..4).
func FormatRankName(tier, division int) string {
	return domain.FormatStorageRank(tier, division)
}

// FormatShortRankName returns a concise tactical rank badge (e.g., "GC I Â· DIV II", "SSL").
func FormatShortRankName(tier, division int) string {
	return domain.FormatStorageShortRank(tier, division)
}

// CalculateBackoff computes exponential backoff: baseBackoff * 2^(attempt - 1), capped at maxBackoff.
func CalculateBackoff(attempt int, baseBackoff, maxBackoff time.Duration) time.Duration {
	if attempt <= 0 || baseBackoff <= 0 {
		return 0
	}
	shift := attempt - 1
	if shift > 30 {
		shift = 30
	}
	multiplier := time.Duration(1 << shift)
	if maxBackoff > 0 && multiplier > maxBackoff/baseBackoff {
		return maxBackoff
	}
	delay := baseBackoff * multiplier
	if delay < 0 || (maxBackoff > 0 && delay > maxBackoff) {
		if maxBackoff > 0 {
			return maxBackoff
		}
		return time.Duration(1<<63 - 1)
	}
	return delay
}

// ParseReplayDate attempts to parse date formats used in Rocket League replays and Ballchasing APIs.
func ParseReplayDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15-04-05",
		"2006-01-02:15-04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// NormalizeBallchasingURL converts Ballchasing API location URLs (/api/replays/<id>) to web URLs (/replay/<id>).
func NormalizeBallchasingURL(ballchasingID, ballchasingURL string) string {
	id := strings.TrimSpace(ballchasingID)
	u := strings.TrimSpace(ballchasingURL)
	if id != "" && (u == "" || strings.Contains(u, "/api/replays/")) {
		return "https://ballchasing.com/replay/" + id
	}
	return u
}

// MatchesPlatformFilter reports whether playerPlatform matches filterPlatform across alias families.
func MatchesPlatformFilter(playerPlatform, filterPlatform string) bool {
	fp := strings.ToLower(strings.TrimSpace(filterPlatform))
	if fp == "" || fp == "all" {
		return true
	}
	pp := strings.ToLower(strings.TrimSpace(playerPlatform))
	if pp == fp {
		return true
	}
	switch fp {
	case "playstation", "ps4", "psn", "psynet":
		return pp == "playstation" || pp == "ps4" || pp == "psn" || pp == "psynet"
	case "xbox", "xboxone":
		return pp == "xbox" || pp == "xboxone"
	case "switch", "nintendo":
		return pp == "switch" || pp == "nintendo"
	}
	return false
}

// SamePlatformFamily reports whether two platform strings belong to the same platform family.
func SamePlatformFamily(platA, platB string) bool {
	a := strings.ToLower(strings.TrimSpace(platA))
	b := strings.ToLower(strings.TrimSpace(platB))
	if a == "" || b == "" || a == "unknown" || b == "unknown" {
		return true
	}
	return MatchesPlatformFilter(a, b)
}

// ParsePlaylistFilter maps a query parameter to a list of matching Rocket League playlist IDs.
func ParsePlaylistFilter(raw string) []int {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || s == "all" || s == "0" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return []int{n}
	}
	switch s {
	case "1v1", "duel", "ranked duel", "ranked duel (1v1)", "casual duel", "casual duel (1v1)":
		return []int{10, 1}
	case "2v2", "doubles", "ranked doubles", "ranked doubles (2v2)", "casual doubles", "casual doubles (2v2)":
		return []int{11, 2}
	case "3v3", "standard", "ranked standard", "ranked standard (3v3)", "casual standard", "casual standard (3v3)", "solo standard":
		return []int{13, 3, 12}
	case "4v4", "chaos", "casual chaos", "casual chaos (4v4)":
		return []int{4}
	case "extra", "extra modes", "extras":
		return []int{27, 28, 29, 30, 34}
	case "hoops", "ranked hoops":
		return []int{27}
	case "rumble", "ranked rumble":
		return []int{28}
	case "dropshot", "ranked dropshot":
		return []int{29}
	case "snow day", "snowday", "ranked snow day":
		return []int{30}
	case "tournament", "tournaments", "competitive tournament":
		return []int{34}
	}
	return nil
}

// ParseDateFilter parses YYYY-MM-DD, RFC3339, or Unix seconds into a UTC Unix timestamp.
func ParseDateFilter(raw string, isEndOfDay bool) int64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			if isEndOfDay {
				return t.UTC().Add(24*time.Hour - time.Second).Unix()
			}
			return t.UTC().Unix()
		}
	}
	if t, ok := ParseReplayDate(s); ok {
		return t.UTC().Unix()
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return n
	}
	return 0
}

// NormalizeResultFilter normalizes result filter aliases into "victory", "defeat", "draw", or "".
func NormalizeResultFilter(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "victory", "w", "win", "wins", "won":
		return "victory"
	case "defeat", "l", "loss", "losses", "lost":
		return "defeat"
	case "draw", "tie", "d":
		return "draw"
	default:
		return ""
	}
}

// NormalizeRoleFilter normalizes role filter aliases into "teammate", "opponent", "frequent", "synergy", or "all".
func NormalizeRoleFilter(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "teammate", "with", "with_you", "ally", "mate":
		return "teammate"
	case "opponent", "against", "against_you", "vs", "rival", "foe":
		return "opponent"
	case "frequent":
		return "frequent"
	case "synergy":
		return "synergy"
	default:
		return "all"
	}
}

// ComputeRelationshipTag returns the dynamic broadcast relationship badge for a player based on W-L records.
func ComputeRelationshipTag(tmWins, tmLosses, oppWins, oppLosses int) string {
	tmGames := tmWins + tmLosses
	oppGames := oppWins + oppLosses
	if tmGames == 0 && oppGames == 0 {
		return ""
	}
	if tmGames >= oppGames && tmGames > 0 {
		tmWR := float64(tmWins) / float64(tmGames)
		if tmGames >= 3 && tmWR >= 0.65 {
			return "PRIME DUO"
		}
		if tmWR >= 0.50 {
			return "SYNERGY ALLY"
		}
		return "COLD DUO"
	}
	oppWR := float64(oppWins) / float64(oppGames)
	if oppGames >= 3 && oppWR >= 0.40 && oppWR <= 0.60 {
		return "ARCH-RIVAL"
	}
	if oppWR < 0.50 {
		return "NEMESIS"
	}
	return "FAVORABLE FOE"
}

// Resolve1BasedDivision normalizes a division value to 1..4 (or 0 if none), resolving 0-based PsyNet vs 1-based Ballchasing payloads.
func Resolve1BasedDivision(div int, rankName string) int {
	trimmed := strings.TrimSpace(rankName)
	switch {
	case strings.HasSuffix(trimmed, " Division IV") || strings.HasSuffix(trimmed, " Div IV") || strings.HasSuffix(trimmed, " DIV IV"):
		return 4
	case strings.HasSuffix(trimmed, " Division III") || strings.HasSuffix(trimmed, " Div III") || strings.HasSuffix(trimmed, " DIV III"):
		return 3
	case strings.HasSuffix(trimmed, " Division II") || strings.HasSuffix(trimmed, " Div II") || strings.HasSuffix(trimmed, " DIV II"):
		return 2
	case strings.HasSuffix(trimmed, " Division I") || strings.HasSuffix(trimmed, " Div I") || strings.HasSuffix(trimmed, " DIV I"):
		return 1
	}
	if div >= 1 && div <= 4 {
		return div
	}
	return 0
}

func serializeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	clean := make([]string, 0, len(tags))
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return "[]"
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func parseTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err == nil && tags != nil {
		return tags
	}
	parts := strings.Split(raw, ",")
	tags = make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}

func escapeLike(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '%', '_', '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func chunkStrings(items []string, chunkSize int) [][]string {
	if chunkSize <= 0 || len(items) == 0 {
		return nil
	}
	var out [][]string
	for i := 0; i < len(items); i += chunkSize {
		end := i + chunkSize
		if end > len(items) {
			end = len(items)
		}
		out = append(out, items[i:end])
	}
	return out
}
