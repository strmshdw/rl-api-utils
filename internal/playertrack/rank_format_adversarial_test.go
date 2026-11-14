package playertrack

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rlapi"
)

// ============================================================================
// Adversarial Suite 1: FormatRank Boundary & Fuzz Stress
// ============================================================================

func TestAdversarial_FormatRank_BoundaryTiersAndDivisions(t *testing.T) {
	tiers := []int{
		math.MinInt,
		math.MinInt32,
		-1000000,
		-999,
		-50,
		-2,
		-1,
		0,  // Unranked
		1,  // Bronze I
		10, // Platinum I
		16, // Champion I
		17, // Champion II
		21, // Grand Champion III
		22, // Supersonic Legend
		23, // Boundary out-of-bounds
		24,
		50,
		999,
		1000000,
		math.MaxInt32,
		math.MaxInt,
	}

	divisions := []int{
		math.MinInt,
		math.MinInt32,
		-1000000,
		-50,
		-1,
		0,
		1,
		2,
		3,
		4,
		5,
		50,
		100,
		1000000,
		math.MaxInt32,
		math.MaxInt,
	}

	for _, tier := range tiers {
		for _, div := range divisions {
			result := FormatRank(tier, div)

			// Oracle 1: Out of bounds tiers MUST return "Unknown"
			if tier < 0 || tier > 22 {
				if result != "Unknown" {
					t.Fatalf("FormatRank(%d, %d) = %q; want 'Unknown'", tier, div, result)
				}
				continue
			}

			// Oracle 2: Tier 0 (Unranked) MUST ALWAYS be "Unranked" regardless of division
			if tier == 0 {
				if result != "Unranked" {
					t.Fatalf("FormatRank(0, %d) = %q; want 'Unranked'", div, result)
				}
				continue
			}

			// Oracle 3: Tier 22 (Supersonic Legend) MUST ALWAYS be "Supersonic Legend" regardless of division
			if tier == 22 {
				if result != "Supersonic Legend" {
					t.Fatalf("FormatRank(22, %d) = %q; want 'Supersonic Legend'", div, result)
				}
				continue
			}

			// Oracle 4: Standard tiers 1-21
			baseName := contracts.TierNames[tier]
			if div >= 0 && div <= 3 {
				expected := fmt.Sprintf("%s %s", baseName, contracts.ZeroBasedDivisionNames[div])
				if result != expected {
					t.Fatalf("FormatRank(%d, %d) = %q; want %q", tier, div, result, expected)
				}
			} else {
				// Out-of-bounds division must fallback to baseName without division
				if result != baseName {
					t.Fatalf("FormatRank(%d, %d) = %q; want %q", tier, div, result, baseName)
				}
			}
		}
	}
}

func TestAdversarial_FormatRank_FuzzRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const iterations = 200000

	for i := 0; i < iterations; i++ {
		// Sample across various ranges: full 64-bit int, near 0, negative, positive
		var tier, div int
		switch i % 5 {
		case 0:
			tier = rng.Intn(50) - 10
			div = rng.Intn(20) - 5
		case 1:
			tier = rng.Int()
			div = rng.Int()
		case 2:
			tier = -rng.Int()
			div = -rng.Int()
		case 3:
			tier = rng.Intn(23) // 0..22
			div = rng.Intn(4)   // 0..3
		case 4:
			tier = rng.Intn(23)
			div = rng.Intn(1000) - 500
		}

		got := FormatRank(tier, div)

		// Assertions:
		if tier < 0 || tier >= len(contracts.TierNames) {
			if got != "Unknown" {
				t.Fatalf("iteration %d: FormatRank(%d, %d) = %q; want Unknown", i, tier, div, got)
			}
		} else if tier == 0 {
			if got != "Unranked" {
				t.Fatalf("iteration %d: FormatRank(0, %d) = %q; want Unranked", i, div, got)
			}
		} else if tier == 22 {
			if got != "Supersonic Legend" {
				t.Fatalf("iteration %d: FormatRank(22, %d) = %q; want Supersonic Legend", i, div, got)
			}
		} else {
			if strings.Contains(got, "Unknown") {
				t.Fatalf("iteration %d: valid tier %d returned %q", i, tier, got)
			}
			if div >= 0 && div < 4 {
				if !strings.Contains(got, "Division") {
					t.Fatalf("iteration %d: expected division in %q for tier %d div %d", i, got, tier, div)
				}
			} else {
				if strings.Contains(got, "Division") {
					t.Fatalf("iteration %d: unexpected division in %q for tier %d div %d", i, got, tier, div)
				}
			}
		}
	}
}

// ============================================================================
// Adversarial Suite 2: Division Suppression Invariants (Unranked & SSL)
// ============================================================================

func TestAdversarial_DivisionSuppression_DenseSweep(t *testing.T) {
	// Dense sweep of all divisions from -5000 to +5000 for Tier 0 and Tier 22
	for div := -5000; div <= 5000; div++ {
		unranked := FormatRank(0, div)
		if unranked != "Unranked" {
			t.Fatalf("Division suppression violated for Unranked at div=%d: got %q", div, unranked)
		}
		if strings.Contains(unranked, "Division") || strings.Contains(unranked, "Div") {
			t.Fatalf("Division text found in Unranked at div=%d: got %q", div, unranked)
		}

		ssl := FormatRank(22, div)
		if ssl != "Supersonic Legend" {
			t.Fatalf("Division suppression violated for SSL at div=%d: got %q", div, ssl)
		}
		if strings.Contains(ssl, "Division") || strings.Contains(ssl, "Div") {
			t.Fatalf("Division text found in SSL at div=%d: got %q", div, ssl)
		}
	}
}

func TestAdversarial_DivisionSuppression_ThroughSerializationPipeline(t *testing.T) {
	for div := -100; div <= 100; div++ {
		skills := []rlapi.Skill{
			{Playlist: 11, Tier: 0, Division: div, MMR: 600.0},
			{Playlist: 13, Tier: 22, Division: div, MMR: 1900.0},
		}

		jsonStr, err := SerializeRanksJSON(skills)
		if err != nil {
			t.Fatalf("SerializeRanksJSON failed at div=%d: %v", div, err)
		}

		snapshot, err := ParseRanksJSON(jsonStr)
		if err != nil {
			t.Fatalf("ParseRanksJSON failed at div=%d: %v", div, err)
		}

		r0, ok := snapshot.GetRank(11)
		if !ok || r0.RankName != "Unranked" {
			t.Fatalf("Unranked rank name contaminated at div=%d: got %+v", div, r0)
		}
		if strings.Contains(r0.RankName, "Division") {
			t.Fatalf("Unranked contains 'Division' at div=%d: %q", div, r0.RankName)
		}

		r22, ok := snapshot.GetRank(13)
		if !ok || r22.RankName != "Supersonic Legend" {
			t.Fatalf("SSL rank name contaminated at div=%d: got %+v", div, r22)
		}
		if strings.Contains(r22.RankName, "Division") {
			t.Fatalf("SSL contains 'Division' at div=%d: %q", div, r22.RankName)
		}
	}
}

// ============================================================================
// Adversarial Suite 3: FormatPlaylist Boundary & Fuzz Stress
// ============================================================================

func TestAdversarial_FormatPlaylist_BoundaryAndFuzz(t *testing.T) {
	boundaries := []int{
		math.MinInt,
		math.MinInt32,
		-999999,
		-100,
		-1,
		0,
		1, 2, 3, 4,
		5, 6, 7, 8, 9,
		10, 11, 12, 13,
		27, 28, 29, 30,
		34,
		35, 50, 99, 100,
		999999,
		math.MaxInt32,
		math.MaxInt,
	}

	for _, pid := range boundaries {
		got := FormatPlaylist(pid)
		if got == "" {
			t.Fatalf("FormatPlaylist(%d) returned empty string", pid)
		}

		if canonicalEntry, exists := contracts.CanonicalPlaylists[pid]; exists {
			if got != canonicalEntry.Name {
				t.Fatalf("FormatPlaylist(%d) = %q; want canonical %q", pid, got, canonicalEntry.Name)
			}
		} else {
			expected := fmt.Sprintf("Playlist %d", pid)
			if got != expected {
				t.Fatalf("FormatPlaylist(%d) = %q; want fallback %q", pid, got, expected)
			}
		}
	}

	// Randomized fuzz
	rng := rand.New(rand.NewSource(1337))
	for i := 0; i < 50000; i++ {
		pid := rng.Int()
		if rng.Intn(2) == 0 {
			pid = -pid
		}
		res := FormatPlaylist(pid)
		if res == "" {
			t.Fatalf("FormatPlaylist(%d) returned empty string", pid)
		}
	}
}

// ============================================================================
// Adversarial Suite 4: ParseRanksJSON Malformed, Truncated, Corrupt, and Weird JSON
// ============================================================================

func FuzzFormatRank(f *testing.F) {
	f.Add(0, 0)
	f.Add(0, 3)
	f.Add(22, 0)
	f.Add(22, 3)
	f.Add(1, 0)
	f.Add(17, 2)
	f.Add(-1, 0)
	f.Add(23, 0)
	f.Add(999, -50)
	f.Add(math.MinInt, math.MinInt)
	f.Add(math.MaxInt, math.MaxInt)

	f.Fuzz(func(t *testing.T, tier int, division int) {
		res := FormatRank(tier, division)
		if tier < 0 || tier >= len(contracts.TierNames) {
			if res != "Unknown" {
				t.Fatalf("tier %d div %d produced %q; want Unknown", tier, division, res)
			}
		} else if tier == 0 {
			if res != "Unranked" {
				t.Fatalf("tier 0 div %d produced %q; want Unranked", division, res)
			}
		} else if tier == 22 {
			if res != "Supersonic Legend" {
				t.Fatalf("tier 22 div %d produced %q; want Supersonic Legend", division, res)
			}
		} else {
			if division >= 0 && division < len(contracts.ZeroBasedDivisionNames) {
				if !strings.Contains(res, "Division") {
					t.Fatalf("tier %d div %d missing Division: %q", tier, division, res)
				}
			} else {
				if strings.Contains(res, "Division") {
					t.Fatalf("tier %d div %d unexpectedly has Division: %q", tier, division, res)
				}
			}
		}
	})
}
