package playertrack

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rlapi"
)

// ============================================================================
// Category 1: Exhaustive 23-Tier Formatting Tests
// ============================================================================

func TestFormatRank_All23Tiers(t *testing.T) {
	tests := []struct {
		tier     int
		div      int
		expected string
	}{
		{0, 0, "Unranked"},
		{1, 0, "Bronze I Division I"},
		{2, 0, "Bronze II Division I"},
		{3, 0, "Bronze III Division I"},
		{4, 0, "Silver I Division I"},
		{5, 0, "Silver II Division I"},
		{6, 0, "Silver III Division I"},
		{7, 0, "Gold I Division I"},
		{8, 0, "Gold II Division I"},
		{9, 0, "Gold III Division I"},
		{10, 0, "Platinum I Division I"},
		{11, 0, "Platinum II Division I"},
		{12, 0, "Platinum III Division I"},
		{13, 0, "Diamond I Division I"},
		{14, 0, "Diamond II Division I"},
		{15, 0, "Diamond III Division I"},
		{16, 0, "Champion I Division I"},
		{17, 0, "Champion II Division I"},
		{18, 0, "Champion III Division I"},
		{19, 0, "Grand Champion I Division I"},
		{20, 0, "Grand Champion II Division I"},
		{21, 0, "Grand Champion III Division I"},
		{22, 0, "Supersonic Legend"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Tier_%d", tt.tier), func(t *testing.T) {
			got := FormatRank(tt.tier, tt.div)
			if got != tt.expected {
				t.Errorf("FormatRank(%d, %d) = %q; want %q", tt.tier, tt.div, got, tt.expected)
			}
		})
	}
}

func TestTierName_Exhaustive(t *testing.T) {
	expectedNames := []string{
		"Unranked",
		"Bronze I", "Bronze II", "Bronze III",
		"Silver I", "Silver II", "Silver III",
		"Gold I", "Gold II", "Gold III",
		"Platinum I", "Platinum II", "Platinum III",
		"Diamond I", "Diamond II", "Diamond III",
		"Champion I", "Champion II", "Champion III",
		"Grand Champion I", "Grand Champion II", "Grand Champion III",
		"Supersonic Legend",
	}

	for tier, expected := range expectedNames {
		got := TierName(tier)
		if got != expected {
			t.Errorf("TierName(%d) = %q; want %q", tier, got, expected)
		}
	}

	// Out of bounds
	if got := TierName(-1); got != "Unknown" {
		t.Errorf("TierName(-1) = %q; want Unknown", got)
	}
	if got := TierName(23); got != "Unknown" {
		t.Errorf("TierName(23) = %q; want Unknown", got)
	}
	if got := TierName(100); got != "Unknown" {
		t.Errorf("TierName(100) = %q; want Unknown", got)
	}
}

// ============================================================================
// Category 2: 4-Division Formatting Tests
// ============================================================================

func TestFormatRank_All4Divisions(t *testing.T) {
	sampleTiers := []struct {
		tier     int
		baseName string
	}{
		{13, "Diamond I"},
		{16, "Champion I"},
		{17, "Champion II"},
		{19, "Grand Champion I"},
	}

	divExpectations := []struct {
		div     int
		numeral string
	}{
		{0, "Division I"},
		{1, "Division II"},
		{2, "Division III"},
		{3, "Division IV"},
	}

	for _, st := range sampleTiers {
		for _, de := range divExpectations {
			t.Run(fmt.Sprintf("%s_%s", st.baseName, de.numeral), func(t *testing.T) {
				expected := fmt.Sprintf("%s %s", st.baseName, de.numeral)
				got := FormatRank(st.tier, de.div)
				if got != expected {
					t.Errorf("FormatRank(%d, %d) = %q; want %q", st.tier, de.div, got, expected)
				}
			})
		}
	}
}

func TestDivisionName_Exhaustive(t *testing.T) {
	expectedDivs := []string{"Division I", "Division II", "Division III", "Division IV"}
	for i, expected := range expectedDivs {
		got := DivisionName(i)
		if got != expected {
			t.Errorf("DivisionName(%d) = %q; want %q", i, got, expected)
		}
	}

	// Out of bounds
	if got := DivisionName(-1); got != "" {
		t.Errorf("DivisionName(-1) = %q; want empty string", got)
	}
	if got := DivisionName(4); got != "" {
		t.Errorf("DivisionName(4) = %q; want empty string", got)
	}
	if got := DivisionName(10); got != "" {
		t.Errorf("DivisionName(10) = %q; want empty string", got)
	}
}

// ============================================================================
// Category 3: Special Rank Edge Cases (Unranked & SSL)
// ============================================================================

func TestFormatRank_UnrankedEdgeCases(t *testing.T) {
	divisionsToTest := []int{0, 1, 2, 3, -1, 4, 100}
	for _, div := range divisionsToTest {
		t.Run(fmt.Sprintf("Unranked_Div_%d", div), func(t *testing.T) {
			got := FormatRank(0, div)
			if got != "Unranked" {
				t.Errorf("FormatRank(0, %d) = %q; want 'Unranked' (divisions must NEVER be appended to Unranked)", div, got)
			}
		})
	}
}

func TestFormatRank_SSLEdgeCases(t *testing.T) {
	divisionsToTest := []int{0, 1, 2, 3, -1, 4, 100}
	for _, div := range divisionsToTest {
		t.Run(fmt.Sprintf("SSL_Div_%d", div), func(t *testing.T) {
			got := FormatRank(22, div)
			if got != "Supersonic Legend" {
				t.Errorf("FormatRank(22, %d) = %q; want 'Supersonic Legend' (divisions must NEVER be appended to SSL)", div, got)
			}
		})
	}
}

// ============================================================================
// Category 4: Error & Out-of-Bounds Handling
// ============================================================================

func TestFormatRank_NegativeAndOutOfBoundsTiers(t *testing.T) {
	outOfBoundsTiers := []int{-1, -2, -100, 23, 24, 25, 99, 1000}
	for _, tier := range outOfBoundsTiers {
		t.Run(fmt.Sprintf("Tier_%d", tier), func(t *testing.T) {
			got := FormatRank(tier, 0)
			if got != "Unknown" {
				t.Errorf("FormatRank(%d, 0) = %q; want 'Unknown'", tier, got)
			}
		})
	}
}

func TestFormatRank_OutOfBoundsDivisions(t *testing.T) {
	tests := []struct {
		tier     int
		div      int
		expected string
	}{
		{14, -1, "Diamond II"},
		{14, 4, "Diamond II"},
		{14, 10, "Diamond II"},
		{16, -10, "Champion I"},
		{16, 5, "Champion I"},
		{17, -1, "Champion II"},
		{17, 99, "Champion II"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Tier_%d_Div_%d", tt.tier, tt.div), func(t *testing.T) {
			got := FormatRank(tt.tier, tt.div)
			if got != tt.expected {
				t.Errorf("FormatRank(%d, %d) = %q; want %q", tt.tier, tt.div, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// Category 5: Playlist Mapping Tests
// ============================================================================

func TestFormatPlaylist_CanonicalPlaylists(t *testing.T) {
	tests := []struct {
		playlistID int
		expected   string
	}{
		{1, "Casual Duel (1v1)"},
		{2, "Casual Doubles (2v2)"},
		{3, "Casual Standard (3v3)"},
		{4, "Casual Chaos (4v4)"},
		{10, "Ranked Duel (1v1)"},
		{11, "Ranked Doubles (2v2)"},
		{12, "Ranked Solo Standard (3v3)"},
		{13, "Ranked Standard (3v3)"},
		{27, "Ranked Hoops"},
		{28, "Ranked Rumble"},
		{29, "Ranked Dropshot"},
		{30, "Ranked Snow Day"},
		{34, "Competitive Tournaments"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Playlist_%d", tt.playlistID), func(t *testing.T) {
			got := FormatPlaylist(tt.playlistID)
			if got != tt.expected {
				t.Errorf("FormatPlaylist(%d) = %q; want %q", tt.playlistID, got, tt.expected)
			}
		})
	}
}

func TestFormatPlaylist_UnknownPlaylists(t *testing.T) {
	tests := []struct {
		playlistID int
		expected   string
	}{
		{0, "Playlist 0"},
		{99, "Playlist 99"},
		{999, "Playlist 999"},
		{-1, "Playlist -1"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Playlist_%d", tt.playlistID), func(t *testing.T) {
			got := FormatPlaylist(tt.playlistID)
			if got != tt.expected {
				t.Errorf("FormatPlaylist(%d) = %q; want %q", tt.playlistID, got, tt.expected)
			}
		})
	}
}

func TestIsRankedPlaylist(t *testing.T) {
	rankedPlaylists := []int{10, 11, 12, 13, 27, 28, 29, 30, 34}
	for _, pid := range rankedPlaylists {
		if !IsRankedPlaylist(pid) {
			t.Errorf("IsRankedPlaylist(%d) = false; want true", pid)
		}
	}

	unrankedPlaylists := []int{1, 2, 3, 4, 0, 99, -1}
	for _, pid := range unrankedPlaylists {
		if IsRankedPlaylist(pid) {
			t.Errorf("IsRankedPlaylist(%d) = true; want false", pid)
		}
	}
}

func TestIsExtraMode(t *testing.T) {
	extraModes := []int{27, 28, 29, 30}
	for _, pid := range extraModes {
		if !IsExtraMode(pid) {
			t.Errorf("IsExtraMode(%d) = false; want true", pid)
		}
	}

	nonExtra := []int{1, 2, 3, 10, 11, 13, 34, 0}
	for _, pid := range nonExtra {
		if IsExtraMode(pid) {
			t.Errorf("IsExtraMode(%d) = true; want false", pid)
		}
	}
}

// ============================================================================
// Category 6: ranks_json Serialization & Deserialization
// ============================================================================

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

func TestMockSkillFetcher_BatchSuccess(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()

	p1 := rlapi.PlayerID("Epic|account1|0")
	p2 := rlapi.PlayerID("Steam|76561198000000001|0")

	mock.SkillsMap[p1] = []rlapi.Skill{
		{Playlist: 11, Tier: 17, Division: 3, MMR: 1150.5, MatchesPlayed: 100},
	}
	mock.SkillsMap[p2] = []rlapi.Skill{
		{Playlist: 11, Tier: 16, Division: 1, MMR: 1060.0, MatchesPlayed: 50},
	}

	results, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p1, p2})
	if err != nil {
		t.Fatalf("GetPlayersSkills failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 RPC call, got %d", len(mock.Calls))
	}

	if results[0].PlayerID != p1 {
		t.Errorf("results[0].PlayerID = %q; want %q", results[0].PlayerID, p1)
	}
	if len(results[0].Skills) != 1 || results[0].Skills[0].Tier != 17 {
		t.Errorf("results[0].Skills mismatch: %+v", results[0].Skills)
	}
}

func TestMockSkillFetcher_EmptyPlayerList(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()

	results, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{})
	if err != nil {
		t.Fatalf("GetPlayersSkills with empty list failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestMockSkillFetcher_UnrankedPlayer(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()

	p := rlapi.PlayerID("Epic|unrankedPlayer|0")
	mock.SkillsMap[p] = []rlapi.Skill{
		{Playlist: 11, Tier: 0, Division: 0, MMR: 600.0, MatchesPlayed: 0},
	}

	results, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p})
	if err != nil {
		t.Fatalf("GetPlayersSkills failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	rankName := FormatRank(results[0].Skills[0].Tier, results[0].Skills[0].Division)
	if rankName != "Unranked" {
		t.Errorf("rankName = %q; want 'Unranked'", rankName)
	}
}

func TestMockSkillFetcher_SSLRankedPlayer(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()

	p := rlapi.PlayerID("Epic|sslPlayer|0")
	mock.SkillsMap[p] = []rlapi.Skill{
		{Playlist: 11, Tier: 22, Division: 0, MMR: 1850.0, MatchesPlayed: 300},
	}

	results, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p})
	if err != nil {
		t.Fatalf("GetPlayersSkills failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	rankName := FormatRank(results[0].Skills[0].Tier, results[0].Skills[0].Division)
	if rankName != "Supersonic Legend" {
		t.Errorf("rankName = %q; want 'Supersonic Legend'", rankName)
	}
}

func TestMockSkillFetcher_RpcError(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()
	mock.Err = errors.New("PsyNet RPC: Session expired")

	p := rlapi.PlayerID("Epic|test|0")
	_, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p})
	if err == nil {
		t.Fatalf("expected RPC error, got nil")
	}
	if !strings.Contains(err.Error(), "Session expired") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMockSkillFetcher_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := NewMockSkillFetcher()
	p := rlapi.PlayerID("Epic|test|0")
	_, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestMockSkillFetcher_Close(t *testing.T) {
	mock := NewMockSkillFetcher()
	if !mock.IsEnabled() {
		t.Errorf("expected mock to be enabled initially")
	}
	if err := mock.Close(); err != nil {
		t.Errorf("unexpected error on Close: %v", err)
	}
	if mock.IsEnabled() {
		t.Errorf("expected mock to be disabled after Close")
	}
	_, err := mock.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|test|0"})
	if !errors.Is(err, ErrRankClientClosed) {
		t.Errorf("expected ErrRankClientClosed, got: %v", err)
	}
}

// ============================================================================
// Category 8: Offline Fallback, Degradation & Collision Checks
// ============================================================================

func TestNoOpRankClient(t *testing.T) {
	client := NewNoOpRankClient()
	if client.IsEnabled() {
		t.Errorf("NoOpRankClient.IsEnabled() = true; want false")
	}

	ctx := context.Background()
	results, err := client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|test|0"})
	if err != nil {
		t.Errorf("NoOpRankClient returned error: %v", err)
	}
	if results != nil {
		t.Errorf("NoOpRankClient returned non-nil results: %+v", results)
	}
	if err := client.Close(); err != nil {
		t.Errorf("NoOpRankClient.Close() returned error: %v", err)
	}
}

func TestCheckCredentialCollision(t *testing.T) {
	tests := []struct {
		name      string
		primary   config.AuthConfig
		polling   config.PollingAuthConfig
		expectErr bool
	}{
		{
			name: "different_providers_epic_and_steam",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok1", AccountID: "acc1"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "ticket1", SteamID64: "76561198000000001"},
			},
			expectErr: false,
		},
		{
			name: "epic_matching_refresh_token",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "dup-token"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "dup-token"},
			},
			expectErr: true,
		},
		{
			name: "epic_matching_account_id",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok1", AccountID: "same-acct"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok2", AccountID: "same-acct"},
			},
			expectErr: true,
		},
		{
			name: "epic_matching_auth_code",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{AuthCode: "same-code"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{AuthCode: "same-code"},
			},
			expectErr: true,
		},
		{
			name: "steam_matching_session_ticket",
			primary: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "same-ticket", SteamID64: "111"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "same-ticket", SteamID64: "222"},
			},
			expectErr: true,
		},
		{
			name: "steam_matching_steam_id_64",
			primary: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "ticket1", SteamID64: "same-steam-id"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "ticket2", SteamID64: "same-steam-id"},
			},
			expectErr: true,
		},
		{
			name: "epic_distinct_tokens",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok1", AccountID: "acc1"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok2", AccountID: "acc2"},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckCredentialCollision(tt.primary, tt.polling)
			if tt.expectErr && err == nil {
				t.Fatalf("expected collision error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("expected no collision, got: %v", err)
			}
			if tt.expectErr && !errors.Is(err, ErrCredentialCollision) {
				t.Errorf("expected ErrCredentialCollision, got: %v", err)
			}
		})
	}
}

func TestNewRankClient_Factory(t *testing.T) {
	t.Run("disabled returns NoOpRankClient", func(t *testing.T) {
		client, err := NewRankClient(RankClientConfig{
			PollingAuth: config.PollingAuthConfig{Enabled: false},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.IsEnabled() {
			t.Errorf("expected disabled NoOpRankClient")
		}
		if _, ok := client.(*NoOpRankClient); !ok {
			t.Errorf("expected *NoOpRankClient type, got %T", client)
		}
	})

	t.Run("credential collision returns NoOpRankClient", func(t *testing.T) {
		client, err := NewRankClient(RankClientConfig{
			PrimaryAuth: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "duplicate"},
			},
			PollingAuth: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "duplicate"},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.IsEnabled() {
			t.Errorf("expected disabled NoOpRankClient on collision")
		}
	})

	t.Run("missing credentials returns NoOpRankClient", func(t *testing.T) {
		client, err := NewRankClient(RankClientConfig{
			PrimaryAuth: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "t1", SteamID64: "1"},
			},
			PollingAuth: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				// No RefreshToken or AuthCode
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.IsEnabled() {
			t.Errorf("expected disabled NoOpRankClient on missing credentials")
		}
	})

	t.Run("valid polling config returns active client with RPCFactory", func(t *testing.T) {
		mockRPC := &testSkillRPC{connected: true}
		client, err := NewRankClient(RankClientConfig{
			PrimaryAuth: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "t1", SteamID64: "1"},
			},
			PollingAuth: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "secondary-token"},
			},
			CredentialsSupplier: psynet.StaticCredentials{
				Platform:  "Epic",
				AuthToken: "eos-token",
				AccountID: "sec-acc",
			},
			RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
				return mockRPC, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !client.IsEnabled() {
			t.Errorf("expected active client")
		}

		res, err := client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|test|0"})
		if err != nil {
			t.Fatalf("GetPlayersSkills failed: %v", err)
		}
		if len(res) != 1 {
			t.Errorf("expected 1 result, got %d", len(res))
		}
		_ = client.Close()
	})
}

// ============================================================================
// Category 9: PsyNetRankClient Connection Lifecycle & Reconnect Tests
// ============================================================================

type testSkillRPC struct {
	mu           sync.Mutex
	skills       map[rlapi.PlayerID][]rlapi.Skill
	connected    bool
	closed       bool
	callCount    int
	failNextWith error
}

func (r *testSkillRPC) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.callCount++
	if r.failNextWith != nil {
		err := r.failNextWith
		r.failNextWith = nil
		return nil, err
	}

	var results []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		skills := r.skills[pid]
		results = append(results, rlapi.PlayerWithSkills{
			PlayerID: pid,
			Skills:   skills,
		})
	}
	return results, nil
}

func (r *testSkillRPC) IsConnected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connected && !r.closed
}

func (r *testSkillRPC) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.connected = false
	return nil
}

func TestPsyNetRankClient_EmptyPlayerIDs_NoNetworkCall(t *testing.T) {
	var rpcCreated int32
	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			atomic.AddInt32(&rpcCreated, 1)
			return &testSkillRPC{connected: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}
	defer client.Close()

	res, err := client.GetPlayersSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil results for empty playerIDs")
	}
	if atomic.LoadInt32(&rpcCreated) != 0 {
		t.Errorf("expected no RPC connection created for empty request")
	}
}

func TestPsyNetRankClient_TransparentReconnectOnDrop(t *testing.T) {
	var factoryCalls int32
	firstRPC := &testSkillRPC{
		connected:    true,
		failNextWith: rlapi.ErrConnectionClosed, // First call fails with dropped connection
	}
	secondRPC := &testSkillRPC{
		connected: true,
		skills: map[rlapi.PlayerID][]rlapi.Skill{
			"Epic|reconnect-player|0": {
				{Playlist: 11, Tier: 17, Division: 3, MMR: 1150.0},
			},
		},
	}

	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			count := atomic.AddInt32(&factoryCalls, 1)
			if count == 1 {
				return firstRPC, nil
			}
			return secondRPC, nil
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	res, err := client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|reconnect-player|0"})
	if err != nil {
		t.Fatalf("expected transparent reconnect to succeed, got: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if res[0].Skills[0].Tier != 17 {
		t.Errorf("expected Tier 17, got %d", res[0].Skills[0].Tier)
	}
	if atomic.LoadInt32(&factoryCalls) != 2 {
		t.Errorf("expected 2 factory calls (initial + reconnect), got %d", atomic.LoadInt32(&factoryCalls))
	}
}

func TestPsyNetRankClient_CloseAndClosedErrors(t *testing.T) {
	mockRPC := &testSkillRPC{connected: true}
	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			return mockRPC, nil
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if !client.IsEnabled() {
		t.Errorf("expected client to be enabled")
	}

	// Trigger connection
	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|p1|0"})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// Close client
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if client.IsEnabled() {
		t.Errorf("expected client to be disabled after Close()")
	}
	if !mockRPC.closed {
		t.Errorf("expected underlying RPC to be closed")
	}

	// Double close should succeed cleanly
	if err := client.Close(); err != nil {
		t.Errorf("double close failed: %v", err)
	}

	// Queries after close return ErrRankClientClosed
	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|p1|0"})
	if !errors.Is(err, ErrRankClientClosed) {
		t.Errorf("expected ErrRankClientClosed, got: %v", err)
	}
}

func TestPsyNetRankClient_RuntimeCollisionDetection(t *testing.T) {
	primaryAcct := "primary-account-guid"
	client, err := NewPsyNetRankClient(RankClientConfig{
		PrimaryAuth: config.AuthConfig{
			Provider: "epic",
			Epic:     config.EpicConfig{AccountID: primaryAcct},
		},
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: primaryAcct, // Collision!
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			return &testSkillRPC{connected: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|p1|0"})
	if err == nil {
		t.Fatalf("expected collision error, got nil")
	}
	if !errors.Is(err, ErrCredentialCollision) && !strings.Contains(err.Error(), "conflict") {
		t.Errorf("expected collision error, got: %v", err)
	}
}

func TestPsyNetRankClient_Concurrency(t *testing.T) {
	mockRPC := &testSkillRPC{
		connected: true,
		skills: map[rlapi.PlayerID][]rlapi.Skill{
			"Epic|concurrent|0": {
				{Playlist: 11, Tier: 16, Division: 2, MMR: 1080.0},
			},
		},
	}

	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			return mockRPC, nil
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|concurrent|0"})
		}()
	}

	// Close asynchronously after slight delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = client.Close()
	}()

	wg.Wait()
}

func TestAuthProviderSupplier(t *testing.T) {
	t.Run("nil provider returns ErrMissingCredentials", func(t *testing.T) {
		supplier := NewAuthProviderSupplier(nil)
		_, err := supplier.GetCredentials(context.Background())
		if !errors.Is(err, ErrMissingCredentials) {
			t.Errorf("expected ErrMissingCredentials, got %v", err)
		}
	})

	t.Run("valid mock AuthProvider creates credentials", func(t *testing.T) {
		mockAuth := &mockAuthProvider{
			name: "epic",
			tokenInfo: &auth.TokenInfo{
				AccessToken:   "test-access-token",
				EpicAccountID: "epic-eos-id",
				AccountID:     "epic-account-id",
				DisplayName:   "PollerName",
				ExpiresAt:     time.Now().Add(1 * time.Hour),
			},
		}

		supplier := NewAuthProviderSupplier(mockAuth)
		creds, err := supplier.GetCredentials(context.Background())
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}
		if creds.Platform != "Epic" {
			t.Errorf("creds.Platform = %q; want Epic", creds.Platform)
		}
		if creds.AuthToken != "test-access-token" {
			t.Errorf("creds.AuthToken = %q", creds.AuthToken)
		}
		if creds.AccountID != "epic-eos-id" {
			t.Errorf("creds.AccountID = %q; want epic-eos-id", creds.AccountID)
		}
		if creds.DisplayName != "PollerName" {
			t.Errorf("creds.DisplayName = %q; want PollerName", creds.DisplayName)
		}
	})

	t.Run("steam mock AuthProvider sets SteamAccountID", func(t *testing.T) {
		mockAuth := &mockAuthProvider{
			name: "steam",
			tokenInfo: &auth.TokenInfo{
				AccessToken: "steam-eos-token",
				AccountID:   "76561198000000001",
				DisplayName: "SteamPoller",
				ExpiresAt:   time.Now().Add(1 * time.Hour),
			},
		}

		supplier := NewAuthProviderSupplier(mockAuth)
		creds, err := supplier.GetCredentials(context.Background())
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}
		if creds.Platform != "Steam" {
			t.Errorf("creds.Platform = %q; want Steam", creds.Platform)
		}
		if creds.SteamAccountID != "76561198000000001" {
			t.Errorf("creds.SteamAccountID = %q; want 76561198000000001", creds.SteamAccountID)
		}
	})
}

type mockAuthProvider struct {
	name      string
	tokenInfo *auth.TokenInfo
}

func (m *mockAuthProvider) Name() string { return m.name }
func (m *mockAuthProvider) Authenticate(ctx context.Context) (*auth.TokenInfo, error) {
	return m.tokenInfo, nil
}
func (m *mockAuthProvider) Refresh(ctx context.Context, refreshToken string) (*auth.TokenInfo, error) {
	return m.tokenInfo, nil
}
func (m *mockAuthProvider) TokenInfo() *auth.TokenInfo { return m.tokenInfo }
func (m *mockAuthProvider) Validate() error           { return nil }
