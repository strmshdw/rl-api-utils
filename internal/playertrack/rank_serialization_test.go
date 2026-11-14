package playertrack

import (
	"strings"
	"testing"

	"github.com/dank/rlapi"
)

func TestSerializeRanksJSON_ValidSkills(t *testing.T) {
	skills := []rlapi.Skill{
		{
			Playlist:      11,
			Tier:          17,
			Division:      3,
			MMR:           1150.5,
			MatchesPlayed: 120,
		},
		{
			Playlist:      13,
			Tier:          16,
			Division:      2,
			MMR:           1080.2,
			MatchesPlayed: 45,
		},
	}

	jsonStr, err := SerializeRanksJSON(skills)
	if err != nil {
		t.Fatalf("SerializeRanksJSON failed: %v", err)
	}

	snapshot, err := ParseRanksJSON(jsonStr)
	if err != nil {
		t.Fatalf("ParseRanksJSON failed: %v", err)
	}

	if len(snapshot) != 2 {
		t.Fatalf("expected 2 playlists in snapshot, got %d", len(snapshot))
	}

	// Verify Playlist 11 (Doubles)
	p11, ok := snapshot.GetRank(11)
	if !ok {
		t.Fatalf("snapshot missing playlist 11")
	}
	if p11.PlaylistID != 11 {
		t.Errorf("p11.PlaylistID = %d; want 11", p11.PlaylistID)
	}
	if p11.PlaylistName != "Ranked Doubles (2v2)" {
		t.Errorf("p11.PlaylistName = %q; want 'Ranked Doubles (2v2)'", p11.PlaylistName)
	}
	if p11.Tier != 17 {
		t.Errorf("p11.Tier = %d; want 17", p11.Tier)
	}
	if p11.Division != 3 {
		t.Errorf("p11.Division = %d; want 3", p11.Division)
	}
	if p11.RankName != "Champion II Division IV" {
		t.Errorf("p11.RankName = %q; want 'Champion II Division IV'", p11.RankName)
	}
	if p11.MMR != 1150.5 {
		t.Errorf("p11.MMR = %f; want 1150.5", p11.MMR)
	}
	if p11.MatchesPlayed != 120 {
		t.Errorf("p11.MatchesPlayed = %d; want 120", p11.MatchesPlayed)
	}

	// Verify Playlist 13 (Standard)
	p13, ok := snapshot.GetRank(13)
	if !ok {
		t.Fatalf("snapshot missing playlist 13")
	}
	if p13.RankName != "Champion I Division III" {
		t.Errorf("p13.RankName = %q; want 'Champion I Division III'", p13.RankName)
	}
}

func TestSerializeRanksJSON_EmptyAndNil(t *testing.T) {
	gotNil, err := SerializeRanksJSON(nil)
	if err != nil {
		t.Fatalf("SerializeRanksJSON(nil) error: %v", err)
	}
	if gotNil != "{}" {
		t.Errorf("SerializeRanksJSON(nil) = %q; want '{}'", gotNil)
	}

	gotEmpty, err := SerializeRanksJSON([]rlapi.Skill{})
	if err != nil {
		t.Fatalf("SerializeRanksJSON(empty) error: %v", err)
	}
	if gotEmpty != "{}" {
		t.Errorf("SerializeRanksJSON(empty) = %q; want '{}'", gotEmpty)
	}
}

func TestParseRanksJSON_RoundTrip(t *testing.T) {
	skills := []rlapi.Skill{
		{
			Playlist:      10,
			Tier:          14,
			Division:      1,
			MMR:           890.25,
			MatchesPlayed: 25,
		},
		{
			Playlist:      27,
			Tier:          22,
			Division:      0,
			MMR:           1350.0,
			MatchesPlayed: 80,
		},
	}

	jsonStr, err := SerializeRanksJSON(skills)
	if err != nil {
		t.Fatalf("Serialize error: %v", err)
	}

	snapshot, err := ParseRanksJSON(jsonStr)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	sslRank, ok := snapshot.GetRank(27)
	if !ok {
		t.Fatalf("missing playlist 27")
	}
	if sslRank.RankName != "Supersonic Legend" {
		t.Errorf("sslRank.RankName = %q; want 'Supersonic Legend'", sslRank.RankName)
	}
	if sslRank.PlaylistName != "Ranked Hoops" {
		t.Errorf("sslRank.PlaylistName = %q; want 'Ranked Hoops'", sslRank.PlaylistName)
	}
}

func TestParseRanksJSON_EmptyAndNull(t *testing.T) {
	inputs := []string{"", "{}", "   ", "\n\t"}
	for _, in := range inputs {
		snapshot, err := ParseRanksJSON(in)
		if err != nil {
			t.Errorf("ParseRanksJSON(%q) returned error: %v", in, err)
		}
		if snapshot == nil {
			t.Errorf("ParseRanksJSON(%q) returned nil map", in)
		}
		if len(snapshot) != 0 {
			t.Errorf("ParseRanksJSON(%q) returned non-empty map: %+v", in, snapshot)
		}
	}
}

func TestParseRanksJSON_LegacyOrPartialJSON(t *testing.T) {
	legacyJSON := `{"11":{"tier":15,"division":2}}`
	snapshot, err := ParseRanksJSON(legacyJSON)
	if err != nil {
		t.Fatalf("failed to parse legacy JSON: %v", err)
	}

	rank, ok := snapshot.GetRank(11)
	if !ok {
		t.Fatalf("failed to get playlist 11 from legacy snapshot")
	}
	if rank.Tier != 15 || rank.Division != 2 {
		t.Errorf("unexpected tier/division: %+v", rank)
	}
	// Verify automatic backfill
	if rank.PlaylistID != 11 {
		t.Errorf("rank.PlaylistID = %d; want 11", rank.PlaylistID)
	}
	if rank.PlaylistName != "Ranked Doubles (2v2)" {
		t.Errorf("rank.PlaylistName = %q; want 'Ranked Doubles (2v2)'", rank.PlaylistName)
	}
	if rank.RankName != "Diamond III Division III" {
		t.Errorf("rank.RankName = %q; want 'Diamond III Division III'", rank.RankName)
	}
}

func TestParseRanksJSON_MalformedJSON(t *testing.T) {
	malformed := `{"11": broken-json`
	_, err := ParseRanksJSON(malformed)
	if err == nil {
		t.Fatalf("expected error parsing malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal ranks_json") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPlayerRanksSnapshot_GetRank(t *testing.T) {
	var nilSnapshot PlayerRanksSnapshot
	_, ok := nilSnapshot.GetRank(11)
	if ok {
		t.Errorf("nil snapshot should return ok=false")
	}

	s := PlayerRanksSnapshot{
		"11": PlayerPlaylistRank{PlaylistID: 11, RankName: "Champion I"},
	}

	r, ok := s.GetRank(11)
	if !ok || r.RankName != "Champion I" {
		t.Errorf("GetRank(11) failed: ok=%v, rank=%+v", ok, r)
	}

	_, ok = s.GetRank(13)
	if ok {
		t.Errorf("GetRank(13) unexpectedly found rank")
	}
}

// ============================================================================
// Category 7: Mock Skill Fetcher Tests
// ============================================================================

func TestConvertSkillRatingToMMR_StandardValues(t *testing.T) {
	tests := []struct {
		name     string
		raw      float64
		expected float64
	}{
		{"Champion I standard", 47.5, 1050.0},
		{"Diamond III boundary", 47.0, 1040.0},
		{"Gold III fractional", 30.4646, 709.0},
		{"Grand Champion pro", 81.8645, 1737.0},
		{"Exact half step", 41.25, 925.0},
		{"Rounding down", 41.24, 925.0},
		{"Rounding up", 41.26, 925.0},
		{"Bronze I minimal positive", 5.0, 200.0},
		{"Bronze I very low", 2.5, 150.0},
		{"Pro raw rating (Zen/Vatira)", 125.0, 2600.0},
		{"Boundary raw rating 150.0", 150.0, 3100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tt.raw)
			if got != tt.expected {
				t.Errorf("ConvertSkillRatingToMMR(%v) = %v; want %v", tt.raw, got, tt.expected)
			}
		})
	}
}

func TestConvertSkillRatingToMMR_CasualAndNegative(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"Zero casual rating", 0.0, 0.0},
		{"Negative float small", -0.01, 0.0},
		{"Negative float large", -5.0, 0.0},
		{"Extreme loss streak", -50.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tt.input)
			if got != tt.expected {
				t.Errorf("ConvertSkillRatingToMMR(%v) = %v; want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConvertSkillRatingToMMR_HighMMRAndIdempotency(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"Already converted boundary", 150.1, 150.1},
		{"Standard in-game fixture 1040", 1040.0, 1040.0},
		{"Standard in-game fixture 1050", 1050.0, 1050.0},
		{"Standard in-game fixture 1150.5", 1150.5, 1150.5},
		{"High in-game fixture 1900", 1900.0, 1900.0},
		{"SSL in-game fixture 2600", 2600.0, 2600.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tt.input)
			if got != tt.expected {
				t.Errorf("ConvertSkillRatingToMMR(%v) = %v; want %v", tt.input, got, tt.expected)
			}
			// Test idempotency: Convert(Convert(v)) == Convert(v)
			second := ConvertSkillRatingToMMR(got)
			if second != got {
				t.Errorf("Idempotency violation for %v: second conversion returned %v", tt.input, second)
			}
		})
	}
}

func TestFromSkill_TrueInGameMMR(t *testing.T) {
	t.Run("Standard raw skill rating conversion", func(t *testing.T) {
		skill := rlapi.Skill{
			Playlist:      11,
			Tier:          16,
			Division:      2,
			MMR:           47.5,
			MatchesPlayed: 50,
		}
		rank := FromSkill(skill)
		if rank.MMR != 1050.0 {
			t.Fatalf("FromSkill MMR = %v; want 1050.0", rank.MMR)
		}
		if rank.RankName != "Champion I Division III" {
			t.Errorf("FromSkill RankName = %q; want 'Champion I Division III'", rank.RankName)
		}
		if rank.PlaylistName != "Ranked Doubles (2v2)" {
			t.Errorf("FromSkill PlaylistName = %q; want 'Ranked Doubles (2v2)'", rank.PlaylistName)
		}
	})

	t.Run("Casual playlist zero rating guard", func(t *testing.T) {
		skill := rlapi.Skill{
			Playlist:      2,
			Tier:          0,
			Division:      0,
			MMR:           0.0,
			MatchesPlayed: 10,
		}
		rank := FromSkill(skill)
		if rank.MMR != 0.0 {
			t.Fatalf("FromSkill casual MMR = %v; want 0.0", rank.MMR)
		}
	})

	t.Run("Pre-scaled MMR passthrough", func(t *testing.T) {
		skill := rlapi.Skill{
			Playlist:      13,
			Tier:          17,
			Division:      1,
			MMR:           1120.0,
			MatchesPlayed: 75,
		}
		rank := FromSkill(skill)
		if rank.MMR != 1120.0 {
			t.Fatalf("FromSkill pre-scaled MMR = %v; want 1120.0", rank.MMR)
		}
	})
}

func TestParseRanksJSON_LegacyRawRatingMigration(t *testing.T) {
	legacyJSON := `{"11":{"tier":16,"division":2,"mmr":47.5}}`
	snapshot, err := ParseRanksJSON(legacyJSON)
	if err != nil {
		t.Fatalf("ParseRanksJSON failed: %v", err)
	}

	rank, ok := snapshot.GetRank(11)
	if !ok {
		t.Fatalf("failed to retrieve playlist 11")
	}
	if rank.MMR != 1050.0 {
		t.Errorf("rank.MMR = %v; want 1050.0", rank.MMR)
	}
	if rank.PlaylistName != "Ranked Doubles (2v2)" {
		t.Errorf("rank.PlaylistName = %q; want 'Ranked Doubles (2v2)'", rank.PlaylistName)
	}
	if rank.RankName != "Champion I Division III" {
		t.Errorf("rank.RankName = %q; want 'Champion I Division III'", rank.RankName)
	}
}

func TestParseRanksJSON_MultiPlaylistMixedPayload(t *testing.T) {
	mixedJSON := `{
		"10": {"tier": 0, "division": 0, "mmr": 0.0},
		"11": {"tier": 16, "division": 2, "mmr": 47.5},
		"13": {"tier": 19, "division": 0, "mmr": 62.5},
		"27": {"tier": 22, "division": 0, "mmr": 1400.0},
		"99": {"tier": 0, "division": 0, "mmr": -5.0}
	}`

	snapshot, err := ParseRanksJSON(mixedJSON)
	if err != nil {
		t.Fatalf("ParseRanksJSON failed: %v", err)
	}

	// 1. Casual / unranked (0.0 -> 0.0)
	r10, ok := snapshot.GetRank(10)
	if !ok || r10.MMR != 0.0 {
		t.Errorf("playlist 10 MMR = %v; want 0.0", r10.MMR)
	}

	// 2. Legacy raw rating (47.5 -> 1050.0)
	r11, ok := snapshot.GetRank(11)
	if !ok || r11.MMR != 1050.0 {
		t.Errorf("playlist 11 MMR = %v; want 1050.0", r11.MMR)
	}

	// 3. Legacy raw rating (62.5 -> 1350.0)
	r13, ok := snapshot.GetRank(13)
	if !ok || r13.MMR != 1350.0 {
		t.Errorf("playlist 13 MMR = %v; want 1350.0", r13.MMR)
	}

	// 4. Already scaled fixture (1400.0 -> 1400.0)
	r27, ok := snapshot.GetRank(27)
	if !ok || r27.MMR != 1400.0 {
		t.Errorf("playlist 27 MMR = %v; want 1400.0", r27.MMR)
	}

	// 5. Negative rating clamped (-5.0 -> 0.0)
	r99, ok := snapshot.GetRank(99)
	if !ok || r99.MMR != 0.0 {
		t.Errorf("playlist 99 MMR = %v; want 0.0", r99.MMR)
	}
}

func TestBuildRanksSnapshot_And_SerializeRanksJSON_TrueInGameMMR(t *testing.T) {
	skills := []rlapi.Skill{
		{
			Playlist:      11,
			Tier:          16,
			Division:      2,
			MMR:           47.5,
			MatchesPlayed: 60,
		},
		{
			Playlist:      2,
			Tier:          0,
			Division:      0,
			MMR:           0.0,
			MatchesPlayed: 15,
		},
	}

	// Test BuildRanksSnapshot converts raw to in-game MMR
	snapshot := BuildRanksSnapshot(skills)
	r11, ok := snapshot.GetRank(11)
	if !ok || r11.MMR != 1050.0 {
		t.Errorf("BuildRanksSnapshot playlist 11 MMR = %v; want 1050.0", r11.MMR)
	}
	r2, ok := snapshot.GetRank(2)
	if !ok || r2.MMR != 0.0 {
		t.Errorf("BuildRanksSnapshot casual playlist 2 MMR = %v; want 0.0", r2.MMR)
	}

	// Test SerializeRanksJSON serializes in-game MMR
	jsonStr, err := SerializeRanksJSON(skills)
	if err != nil {
		t.Fatalf("SerializeRanksJSON failed: %v", err)
	}

	if !strings.Contains(jsonStr, `"mmr":1050`) {
		t.Errorf("serialized JSON missing '\"mmr\":1050': %s", jsonStr)
	}
	if strings.Contains(jsonStr, "47.5") {
		t.Errorf("serialized JSON unexpectedly contained raw '47.5': %s", jsonStr)
	}

	// Test ParseRanksJSON deserializes serialized JSON correctly
	parsed, err := ParseRanksJSON(jsonStr)
	if err != nil {
		t.Fatalf("ParseRanksJSON failed on serialized output: %v", err)
	}
	pr11, ok := parsed.GetRank(11)
	if !ok || pr11.MMR != 1050.0 {
		t.Errorf("Parsed snapshot playlist 11 MMR = %v; want 1050.0", pr11.MMR)
	}
}
