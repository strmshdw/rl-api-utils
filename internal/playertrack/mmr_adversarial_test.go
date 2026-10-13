package playertrack

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dank/rlapi"
)

// ============================================================================
// Adversarial Suite 1: Floating-Point Precision & Rounding Boundaries
// ============================================================================

func TestAdversarial_ConvertSkillRatingToMMR_PrecisionAndRounding(t *testing.T) {
	testCases := []struct {
		name     string
		input    float64
		expected float64
	}{
		// Standard rank anchors
		{"Standard C1 (47.5)", 47.5, 1050.0},
		{"Standard D3 (47.0)", 47.0, 1040.0},
		{"Standard Gold 3 (30.4646)", 30.4646, 709.0},
		{"Standard GC (81.8645)", 81.8645, 1737.0},
		{"Standard C3 (55.0)", 55.0, 1200.0},
		{"Standard GC1 (67.5)", 67.5, 1450.0},

		// Half-integer rounding around 925.0
		{"Half step exact .25 (41.25)", 41.25, 925.0},
		{"Half step slightly below (41.24)", 41.24, 925.0},
		{"Half step slightly above (41.26)", 41.26, 925.0},
		{"Rounding midpoint lower exact (41.225 -> 924.5)", 41.225, 925.0},
		{"Rounding midpoint lower minus epsilon (41.224999 -> 924.49998)", 41.224999, 924.0},
		{"Rounding midpoint upper exact (41.275 -> 925.5)", 41.275, 926.0},
		{"Rounding midpoint upper minus epsilon (41.274999 -> 925.49998)", 41.274999, 925.0},

		// Half-integer rounding around 935.0 (.75 steps)
		{"Half step exact .75 (41.75)", 41.75, 935.0},
		{"Half step slightly below (41.74)", 41.74, 935.0},
		{"Half step slightly above (41.76)", 41.76, 935.0},

		// Exact integer multiples
		{"Integer 1.0", 1.0, 120.0},
		{"Integer 10.0", 10.0, 300.0},
		{"Integer 25.0 (Default Unranked)", 25.0, 600.0},
		{"Integer 50.0", 50.0, 1100.0},
		{"Integer 75.0", 75.0, 1600.0},
		{"Integer 100.0", 100.0, 2100.0},
		{"Integer 125.0", 125.0, 2600.0},
		{"Integer 150.0 (boundary max)", 150.0, 3100.0},

		// Tiny fractional increments near lower bound
		{"Tiny fraction (0.01 -> 100.2)", 0.01, 100.0},
		{"Tiny fraction just under round-up (0.02499 -> 100.4998)", 0.02499, 100.0},
		{"Tiny fraction round-up (0.025 -> 100.5)", 0.025, 101.0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tc.input)
			if got != tc.expected {
				t.Fatalf("ConvertSkillRatingToMMR(%v) = %v, expected %v", tc.input, got, tc.expected)
			}
		})
	}
}

// ============================================================================
// Adversarial Suite 2: Boundary Testing around 0.0 & Casual Ratings
// ============================================================================

func TestAdversarial_ConvertSkillRatingToMMR_ZeroAndCasual(t *testing.T) {
	testCases := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"Exact positive zero", 0.0, 0.0},
		{"Exact negative zero", math.Copysign(0.0, -1.0), 0.0},
		{"Smallest subnormal positive", math.SmallestNonzeroFloat64, 100.0}, // (minPos * 20) + 100 rounds to 100
		{"Smallest subnormal negative", -math.SmallestNonzeroFloat64, 0.0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tc.input)
			if got != tc.expected {
				t.Fatalf("ConvertSkillRatingToMMR(%v) = %v, expected %v", tc.input, got, tc.expected)
			}
		})
	}
}

// ============================================================================
// Adversarial Suite 3: Negative Ratings & Subnormal Extremes
// ============================================================================

func TestAdversarial_ConvertSkillRatingToMMR_NegativesAndExtremes(t *testing.T) {
	testCases := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"Negative epsilon", -0.0000001, 0.0},
		{"Negative fractional", -0.5, 0.0},
		{"Negative integer -1", -1.0, 0.0},
		{"Negative large -10", -10.0, 0.0},
		{"Negative extreme -100", -100.0, 0.0},
		{"Negative huge -1e9", -1e9, 0.0},
		{"Negative infinity", math.Inf(-1), 0.0},

		// Boundary testing at 150.0
		{"Just below 150 (149.999999)", 149.999999, 3100.0},
		{"Exact boundary 150.0", 150.0, 3100.0},
		{"Just above 150 (150.000001)", 150.000001, 150.000001},
		{"Pre-scaled fixture 150.1", 150.1, 150.1},
		{"Pre-scaled fixture 200.0", 200.0, 200.0},
		{"Pre-scaled fixture 1050.0", 1050.0, 1050.0},
		{"Pre-scaled fixture 2600.0", 2600.0, 2600.0},
		{"Pre-scaled extreme 3500.0", 3500.0, 3500.0},
		{"Positive infinity", math.Inf(1), math.Inf(1)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tc.input)
			if math.IsInf(tc.expected, 1) {
				if !math.IsInf(got, 1) {
					t.Fatalf("ConvertSkillRatingToMMR(%v) = %v, expected +Inf", tc.input, got)
				}
			} else if got != tc.expected {
				t.Fatalf("ConvertSkillRatingToMMR(%v) = %v, expected %v", tc.input, got, tc.expected)
			}
		})
	}
}

// ============================================================================
// Adversarial Suite 4: Idempotency Properties & Boundary Slices
// ============================================================================

func TestAdversarial_ConvertSkillRatingToMMR_Idempotency(t *testing.T) {
	// Rule 1: For any already in-game MMR (> 150.0), Convert(x) == x and Convert(Convert(x)) == Convert(x)
	preScaledInputs := []float64{
		150.000001,
		150.1,
		151.0,
		200.0,
		500.0,
		709.0,
		1040.0,
		1050.0,
		1150.5,
		1400.0,
		1737.0,
		2600.0,
		3100.0,
		3500.0,
	}

	for _, v := range preScaledInputs {
		first := ConvertSkillRatingToMMR(v)
		if first != v {
			t.Fatalf("Pre-scaled MMR %v was mutated on first conversion: %v", v, first)
		}
		second := ConvertSkillRatingToMMR(first)
		if second != first {
			t.Fatalf("Pre-scaled MMR %v failed idempotency: first=%v, second=%v", v, first, second)
		}
	}

	// Rule 2: For any casual / negative input (<= 0.0), Convert(x) == 0.0 and Convert(Convert(x)) == 0.0
	zeroAndNegatives := []float64{0.0, -0.0, -0.0001, -1.0, -50.0, -1000.0}
	for _, v := range zeroAndNegatives {
		first := ConvertSkillRatingToMMR(v)
		if first != 0.0 {
			t.Fatalf("Negative/zero input %v gave non-zero: %v", v, first)
		}
		second := ConvertSkillRatingToMMR(first)
		if second != 0.0 {
			t.Fatalf("Negative/zero input %v failed idempotency: %v", v, second)
		}
	}

	// Rule 3: For realistic competitive raw ratings (> 2.5 to 150.0), Convert(x) > 150.0,
	// so second conversion is guaranteed strictly idempotent: Convert(Convert(x)) == Convert(x)
	realisticRawRatings := []float64{
		2.55,    // 2.55 * 20 + 100 = 151.0 (> 150.0)
		3.0,     // 160.0
		5.0,     // 200.0
		10.0,    // 300.0
		25.0,    // 600.0 (Unranked default)
		30.4646, // 709.0
		41.25,   // 925.0
		47.0,    // 1040.0
		47.5,    // 1050.0
		62.5,    // 1350.0
		81.8645, // 1737.0
		125.0,   // 2600.0 (Pro peak)
		150.0,   // 3100.0
	}
	for _, raw := range realisticRawRatings {
		first := ConvertSkillRatingToMMR(raw)
		if first <= 150.0 {
			t.Fatalf("Realistic raw rating %v converted to <= 150.0: %v", raw, first)
		}
		second := ConvertSkillRatingToMMR(first)
		if second != first {
			t.Fatalf("Raw rating %v failed two-step idempotency: first=%v, second=%v", raw, first, second)
		}
	}
}

// TestAdversarial_ConvertSkillRatingToMMR_BoundarySlice explores the (0.0, 2.5] slice
// where round(20x + 100) evaluates between 100.0 and 150.0.
func TestAdversarial_ConvertSkillRatingToMMR_BoundarySlice(t *testing.T) {
	testSlice := []struct {
		raw            float64
		expectedFirst  float64
		expectedSecond float64
	}{
		{0.01, 100.0, 2100.0},
		{0.5, 110.0, 2300.0},
		{1.0, 120.0, 2500.0},
		{2.0, 140.0, 2900.0},
		{2.5, 150.0, 3100.0},
	}

	for _, tc := range testSlice {
		first := ConvertSkillRatingToMMR(tc.raw)
		if first != tc.expectedFirst {
			t.Fatalf("raw %v expected first conversion %v, got %v", tc.raw, tc.expectedFirst, first)
		}
		second := ConvertSkillRatingToMMR(first)
		if second != tc.expectedSecond {
			t.Fatalf("first %v expected second conversion %v, got %v", first, tc.expectedSecond, second)
		}
	}
}

// ============================================================================
// Adversarial Suite 5: Legacy ParseRanksJSON Migration & Edge Cases
// ============================================================================

func TestAdversarial_ParseRanksJSON_LegacyMigrationAndCorruptedInputs(t *testing.T) {
	t.Run("Empty and whitespace payloads", func(t *testing.T) {
		for _, raw := range []string{"", "   ", "\t\n", "{}", "{  }"} {
			snap, err := ParseRanksJSON(raw)
			if err != nil {
				t.Fatalf("ParseRanksJSON(%q) returned error: %v", raw, err)
			}
			if snap == nil || len(snap) != 0 {
				t.Fatalf("ParseRanksJSON(%q) expected empty map, got len=%d", raw, len(snap))
			}
		}
	})

	t.Run("Malformed JSON gracefully returns error", func(t *testing.T) {
		for _, raw := range []string{"{invalid}", "{11: }", `{"11": "not-a-rank"}`} {
			_, err := ParseRanksJSON(raw)
			if err == nil {
				t.Fatalf("ParseRanksJSON(%q) expected error, got nil", raw)
			}
		}
	})

	t.Run("Mixed legacy raw and already in-game MMR migration", func(t *testing.T) {
		payload := `{
			"10": {"tier": 0, "division": 0, "mmr": 0.0},
			"11": {"tier": 16, "division": 2, "mmr": 47.5},
			"13": {"tier": 19, "division": 0, "mmr": 1350.0},
			"27": {"tier": 22, "division": 0, "mmr": 150.0001},
			"28": {"tier": 22, "division": 0, "mmr": 150.0},
			"34": {"tier": 0, "division": 0, "mmr": -12.5}
		}`

		snap, err := ParseRanksJSON(payload)
		if err != nil {
			t.Fatalf("ParseRanksJSON failed: %v", err)
		}

		// 10: casual 0.0 preserved
		r10, ok := snap.GetRank(10)
		if !ok || r10.MMR != 0.0 {
			t.Errorf("playlist 10 MMR = %v, expected 0.0", r10.MMR)
		}

		// 11: legacy raw 47.5 migrated to 1050.0
		r11, ok := snap.GetRank(11)
		if !ok || r11.MMR != 1050.0 {
			t.Errorf("playlist 11 MMR = %v, expected 1050.0", r11.MMR)
		}

		// 13: already in-game 1350.0 preserved
		r13, ok := snap.GetRank(13)
		if !ok || r13.MMR != 1350.0 {
			t.Errorf("playlist 13 MMR = %v, expected 1350.0", r13.MMR)
		}

		// 27: 150.0001 preserved (> 150.0)
		r27, ok := snap.GetRank(27)
		if !ok || r27.MMR != 150.0001 {
			t.Errorf("playlist 27 MMR = %v, expected 150.0001", r27.MMR)
		}

		// 28: 150.0 migrated to 3100.0 (boundary <= 150.0)
		r28, ok := snap.GetRank(28)
		if !ok || r28.MMR != 3100.0 {
			t.Errorf("playlist 28 MMR = %v, expected 3100.0", r28.MMR)
		}

		// 34: negative -12.5 clamped to 0.0
		r34, ok := snap.GetRank(34)
		if !ok || r34.MMR != 0.0 {
			t.Errorf("playlist 34 MMR = %v, expected 0.0", r34.MMR)
		}
	})

	t.Run("High precision decimal input in JSON", func(t *testing.T) {
		payload := `{"11":{"tier":16,"division":2,"mmr":47.50000000000001}}`
		snap, err := ParseRanksJSON(payload)
		if err != nil {
			t.Fatalf("ParseRanksJSON failed: %v", err)
		}
		r11, ok := snap.GetRank(11)
		if !ok || r11.MMR != 1050.0 {
			t.Errorf("playlist 11 MMR = %v, expected 1050.0", r11.MMR)
		}
	})
}

// ============================================================================
// Adversarial Suite 6: Full Round-Trip Serialization Idempotency
// ============================================================================

func TestAdversarial_RoundTrip_SerializationFidelity(t *testing.T) {
	skills := []rlapi.Skill{
		{
			Playlist:      11,
			Tier:          16,
			Division:      2,
			MMR:           47.5,
			MatchesPlayed: 100,
		},
		{
			Playlist:      13,
			Tier:          19,
			Division:      0,
			MMR:           62.5,
			MatchesPlayed: 50,
		},
		{
			Playlist:      2,
			Tier:          0,
			Division:      0,
			MMR:           0.0,
			MatchesPlayed: 10,
		},
	}

	// Step 1: Serialize skills to JSON
	json1, err := SerializeRanksJSON(skills)
	if err != nil {
		t.Fatalf("SerializeRanksJSON failed: %v", err)
	}

	// Ensure raw decimals are not present in serialized JSON
	if strings.Contains(json1, "47.5") || strings.Contains(json1, "62.5") {
		t.Fatalf("SerializeRanksJSON leaked raw TrueSkill decimals: %s", json1)
	}
	if !strings.Contains(json1, `"mmr":1050`) || !strings.Contains(json1, `"mmr":1350`) {
		t.Fatalf("SerializeRanksJSON missing converted in-game MMR numbers: %s", json1)
	}

	// Step 2: Parse back into snapshot
	snap1, err := ParseRanksJSON(json1)
	if err != nil {
		t.Fatalf("ParseRanksJSON failed: %v", err)
	}

	r11, ok := snap1.GetRank(11)
	if !ok || r11.MMR != 1050.0 {
		t.Fatalf("snap1 playlist 11 MMR = %v, expected 1050.0", r11.MMR)
	}

	// Step 3: Serialize snap1 directly back to JSON
	data2, err := json.Marshal(snap1)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	json2 := string(data2)

	// Step 4: Parse back into snap2
	snap2, err := ParseRanksJSON(json2)
	if err != nil {
		t.Fatalf("ParseRanksJSON(json2) failed: %v", err)
	}

	r11Second, ok := snap2.GetRank(11)
	if !ok || r11Second.MMR != 1050.0 {
		t.Fatalf("snap2 playlist 11 MMR = %v, expected 1050.0", r11Second.MMR)
	}
	r13Second, ok := snap2.GetRank(13)
	if !ok || r13Second.MMR != 1350.0 {
		t.Fatalf("snap2 playlist 13 MMR = %v, expected 1350.0", r13Second.MMR)
	}
	r2Second, ok := snap2.GetRank(2)
	if !ok || r2Second.MMR != 0.0 {
		t.Fatalf("snap2 playlist 2 MMR = %v, expected 0.0", r2Second.MMR)
	}
}

// ============================================================================
// Adversarial Suite 7: High-Concurrency Stress Harness
// ============================================================================

func TestAdversarial_Concurrency_HighLoadMMRConversion(t *testing.T) {
	const goroutines = 50
	const iterationsPerGoroutine = 500

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id * 1000)))

			for i := 0; i < iterationsPerGoroutine; i++ {
				// Category A: Pre-scaled in-game MMR (> 150.0) -> must be strictly idempotent
				preScaled := 150.1 + rng.Float64()*2500.0
				gotScaled := ConvertSkillRatingToMMR(preScaled)
				if gotScaled != preScaled {
					t.Errorf("goroutine %d: preScaled %v changed to %v", id, preScaled, gotScaled)
				}
				if ConvertSkillRatingToMMR(gotScaled) != gotScaled {
					t.Errorf("goroutine %d: preScaled idempotency failed for %v", id, preScaled)
				}

				// Category B: Casual / negative rating (<= 0.0) -> must be strictly 0.0
				neg := -rng.Float64() * 100.0
				if ConvertSkillRatingToMMR(neg) != 0.0 {
					t.Errorf("goroutine %d: negative %v produced non-zero", id, neg)
				}
				if ConvertSkillRatingToMMR(0.0) != 0.0 {
					t.Errorf("goroutine %d: 0.0 produced non-zero", id)
				}

				// Category C: Realistic raw competitive ratings (3.0 to 125.0) -> converted > 150.0 and idempotent
				raw := 3.0 + rng.Float64()*120.0
				converted := ConvertSkillRatingToMMR(raw)
				if converted <= 150.0 {
					t.Errorf("goroutine %d: raw %v converted to <= 150.0: %v", id, raw, converted)
				}
				second := ConvertSkillRatingToMMR(converted)
				if second != converted {
					t.Errorf("goroutine %d: raw %v idempotency failed: first=%v second=%v", id, raw, converted, second)
				}

				// Category D: Concurrent ParseRanksJSON with float round-trip fidelity
				jsonPayload := fmt.Sprintf(`{"11":{"tier":16,"division":2,"mmr":%s}}`, strconv.FormatFloat(raw, 'g', -1, 64))
				snap, err := ParseRanksJSON(jsonPayload)
				if err != nil {
					t.Errorf("goroutine %d: ParseRanksJSON failed: %v", id, err)
					continue
				}
				r, ok := snap.GetRank(11)
				if !ok || r.MMR != converted {
					t.Errorf("goroutine %d: parsed MMR %v != converted MMR %v", id, r.MMR, converted)
				}
			}
		}(g)
	}

	wg.Wait()
}
