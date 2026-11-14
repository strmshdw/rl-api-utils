package playertrack

import (
	"fmt"
	"testing"
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
