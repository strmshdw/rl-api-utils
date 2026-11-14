package playertrack

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/dank/rlapi"
)

func TestAdversarial_ParseRanksJSON_MalformedAndCorruptPayloads(t *testing.T) {
	corruptPayloads := []string{
		`{`,
		`}`,
		`{"11":`,
		`{"11": {`,
		`{"11": {"tier":`,
		`{"11": {"tier": 14`,
		`{"11": {"tier": 14}`, // missing outer closing brace
		`{"11": {"tier": 14},`,
		`{"11": {"tier": 14,}}`,
		`{"11": 12345,}`,
		`{"11": "unterminated string`,
		`{"11": undefined}`,
		`{"11": NaN}`,
		`{"11": Infinity}`,
		`{"11": -Infinity}`,
		`{"11": 0123}`,        // leading zero in JSON number is invalid
		`{"11": +123}`,        // explicit plus sign is invalid in JSON
		`{"11": .123}`,        // missing leading zero
		`{"11": 123.}`,        // trailing dot
		`{11: {"tier": 1}}`,   // unquoted key
		`{'11': {'tier': 1}}`, // single-quoted JSON
		`{"11": {"tier": 17}} trailing garbage`,
		`{"11": {"tier": 17}} {"13": {"tier": 16}}`, // multiple roots
		"{\"11\": {\"tier\": 17\x00}}",              // null byte
		"{\"11\": {\"tier\": 17\x01\x02}}",          // control characters
		`<!-- XML/HTML injected -->`,
		`[{"playlist_id": 11, "tier": 17}]`, // array instead of map
		`"just a string"`,
		`123456`,
		`true`,
		`false`,
	}

	for idx, payload := range corruptPayloads {
		t.Run(fmt.Sprintf("Corrupt_%d", idx), func(t *testing.T) {
			_, err := ParseRanksJSON(payload)
			if err == nil {
				t.Fatalf("expected ParseRanksJSON to fail on corrupt payload %q, got nil error", payload)
			}
			if !strings.Contains(err.Error(), "failed to unmarshal ranks_json") {
				t.Errorf("error message missing expected prefix: %v", err)
			}
		})
	}
}

func TestAdversarial_ParseRanksJSON_DeeplyNestedJSON(t *testing.T) {
	// Deep nesting: 50, 100, 500, 2000 levels
	for _, depth := range []int{50, 100, 500, 2000} {
		var b strings.Builder
		b.WriteString(`{"11": {"tier": 14`)
		for i := 0; i < depth; i++ {
			b.WriteString(`, "nested": {`)
		}
		b.WriteString(`"val": 1`)
		for i := 0; i < depth; i++ {
			b.WriteString(`}`)
		}
		b.WriteString(`}}`)

		payload := b.String()
		snapshot, err := ParseRanksJSON(payload)
		// Should either parse successfully (ignoring unknown nested struct) or fail gracefully without panic/OOM
		if err == nil {
			rank, ok := snapshot.GetRank(11)
			if !ok || rank.Tier != 14 {
				t.Fatalf("depth %d: rank tier expected 14, got %+v", depth, rank)
			}
		}
	}
}

func TestAdversarial_ParseRanksJSON_FuzzCorruptedBytes(t *testing.T) {
	validTemplate := `{"11":{"playlist_id":11,"playlist_name":"Ranked Doubles (2v2)","tier":17,"division":3,"rank_name":"Champion II Division IV","mmr":1150.5,"matches_played":120}}`
	rawBytes := []byte(validTemplate)

	rng := rand.New(rand.NewSource(999))
	const fuzzRounds = 5000

	for round := 0; round < fuzzRounds; round++ {
		mutated := make([]byte, len(rawBytes))
		copy(mutated, rawBytes)

		// Apply random mutations: bit flip, byte insertion, truncation, deletion
		mutationType := rng.Intn(4)
		switch mutationType {
		case 0: // Bit flip
			pos := rng.Intn(len(mutated))
			mutated[pos] ^= byte(1 << rng.Intn(8))
		case 1: // Truncation
			truncLen := rng.Intn(len(mutated))
			mutated = mutated[:truncLen]
		case 2: // Byte replacement with random byte
			pos := rng.Intn(len(mutated))
			mutated[pos] = byte(rng.Intn(256))
		case 3: // Slice deletion
			start := rng.Intn(len(mutated))
			end := start + rng.Intn(len(mutated)-start)
			mutated = append(mutated[:start], mutated[end:]...)
		}

		// Must never panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ParseRanksJSON panicked on mutated input %q: %v", string(mutated), r)
				}
			}()
			_, _ = ParseRanksJSON(string(mutated))
		}()
	}
}

func TestAdversarial_ParseRanksJSON_NullAndEdgeCases(t *testing.T) {
	t.Run("JSON null literal", func(t *testing.T) {
		snapshot, err := ParseRanksJSON("null")
		if err != nil {
			t.Fatalf("ParseRanksJSON('null') failed: %v", err)
		}
		// snapshot may be nil or empty map, but GetRank must not panic!
		_, ok := snapshot.GetRank(11)
		if ok {
			t.Errorf("GetRank on null snapshot returned ok=true")
		}
	})

	t.Run("Map containing null entry", func(t *testing.T) {
		// {"11": null}
		snapshot, err := ParseRanksJSON(`{"11": null}`)
		if err != nil {
			t.Fatalf("ParseRanksJSON('{\"11\": null}') failed: %v", err)
		}
		rank, ok := snapshot.GetRank(11)
		if !ok {
			t.Fatalf("expected playlist 11 to exist for null struct")
		}
		// Automatic backfill must populate PlaylistID, PlaylistName, and RankName
		if rank.PlaylistID != 11 {
			t.Errorf("rank.PlaylistID = %d; want 11", rank.PlaylistID)
		}
		if rank.PlaylistName != "Ranked Doubles (2v2)" {
			t.Errorf("rank.PlaylistName = %q; want 'Ranked Doubles (2v2)'", rank.PlaylistName)
		}
		if rank.RankName != "Unranked" {
			t.Errorf("rank.RankName = %q; want 'Unranked'", rank.RankName)
		}
	})

	t.Run("Non-numeric map key", func(t *testing.T) {
		// {"custom_key": {"tier": 16, "division": 1}}
		snapshot, err := ParseRanksJSON(`{"custom_key": {"tier": 16, "division": 1}}`)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		rank, ok := snapshot["custom_key"]
		if !ok {
			t.Fatalf("missing custom_key")
		}
		if rank.PlaylistID != 0 {
			t.Errorf("expected PlaylistID 0 for non-numeric key, got %d", rank.PlaylistID)
		}
		if rank.RankName != "Champion I Division II" {
			t.Errorf("rank.RankName = %q; want 'Champion I Division II'", rank.RankName)
		}
	})

	t.Run("Negative playlist key", func(t *testing.T) {
		// {"-10": {"tier": 14, "division": 0}}
		snapshot, err := ParseRanksJSON(`{"-10": {"tier": 14, "division": 0}}`)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		rank, ok := snapshot.GetRank(-10)
		if !ok {
			t.Fatalf("missing playlist -10")
		}
		if rank.PlaylistID != -10 {
			t.Errorf("rank.PlaylistID = %d; want -10", rank.PlaylistID)
		}
		if rank.PlaylistName != "Playlist -10" {
			t.Errorf("rank.PlaylistName = %q; want 'Playlist -10'", rank.PlaylistName)
		}
		if rank.RankName != "Diamond II Division I" {
			t.Errorf("rank.RankName = %q; want 'Diamond II Division I'", rank.RankName)
		}
	})

	t.Run("Unranked tier 0 division backfill does not append division", func(t *testing.T) {
		// If legacy JSON provides tier 0, division 3 without rank_name:
		snapshot, err := ParseRanksJSON(`{"11": {"tier": 0, "division": 3}}`)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		rank, ok := snapshot.GetRank(11)
		if !ok {
			t.Fatalf("missing playlist 11")
		}
		if rank.RankName != "Unranked" {
			t.Errorf("backfill produced %q; want 'Unranked' (division must be suppressed)", rank.RankName)
		}
	})

	t.Run("SSL tier 22 division backfill does not append division", func(t *testing.T) {
		// If legacy JSON provides tier 22, division 3 without rank_name:
		snapshot, err := ParseRanksJSON(`{"11": {"tier": 22, "division": 3}}`)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		rank, ok := snapshot.GetRank(11)
		if !ok {
			t.Fatalf("missing playlist 11")
		}
		if rank.RankName != "Supersonic Legend" {
			t.Errorf("backfill produced %q; want 'Supersonic Legend' (division must be suppressed)", rank.RankName)
		}
	})

	t.Run("Extreme values in JSON integers", func(t *testing.T) {
		payload := fmt.Sprintf(`{"11": {"tier": %d, "division": %d, "matches_played": %d, "mmr": 99999.9}}`,
			math.MaxInt32, math.MinInt32, math.MaxInt32)
		snapshot, err := ParseRanksJSON(payload)
		if err != nil {
			t.Fatalf("failed on extreme integers: %v", err)
		}
		rank, ok := snapshot.GetRank(11)
		if !ok {
			t.Fatalf("missing rank 11")
		}
		if rank.RankName != "Unknown" {
			t.Errorf("extreme tier produced %q; want 'Unknown'", rank.RankName)
		}
	})

	t.Run("Huge payload with 2000 playlists", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString(`{`)
		for i := 0; i < 2000; i++ {
			if i > 0 {
				sb.WriteString(`,`)
			}
			sb.WriteString(fmt.Sprintf(`"%d":{"tier":%d,"division":%d,"mmr":%f,"matches_played":%d}`,
				i, i%23, i%4, float64(i)*1.5, i*2))
		}
		sb.WriteString(`}`)

		snapshot, err := ParseRanksJSON(sb.String())
		if err != nil {
			t.Fatalf("failed on 2000 playlist payload: %v", err)
		}
		if len(snapshot) != 2000 {
			t.Errorf("len(snapshot) = %d; want 2000", len(snapshot))
		}
		// Verify arbitrary element
		r500, ok := snapshot.GetRank(500)
		if !ok {
			t.Fatalf("missing playlist 500")
		}
		if r500.PlaylistID != 500 {
			t.Errorf("r500.PlaylistID = %d; want 500", r500.PlaylistID)
		}
		if r500.PlaylistName != "Playlist 500" {
			t.Errorf("r500.PlaylistName = %q; want 'Playlist 500'", r500.PlaylistName)
		}
	})
}

// ============================================================================
// Adversarial Suite 5: Concurrent Operations & Thread Safety
// ============================================================================

func TestAdversarial_Concurrency_FormatAndParse(t *testing.T) {
	const goroutines = 25
	const iterations = 500

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Worker group 1: Concurrent FormatRank
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				tier := (id*iterations+i)%30 - 5
				div := (id*iterations+i)%8 - 2
				_ = FormatRank(tier, div)
			}
		}(g)
	}

	// Worker group 2: Concurrent FormatPlaylist
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				pid := (id*iterations+i)%50 - 5
				_ = FormatPlaylist(pid)
			}
		}(g)
	}

	// Worker group 3: Concurrent Serialize and Parse
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				skills := []rlapi.Skill{
					{Playlist: 11, Tier: (id + i) % 23, Division: i % 4, MMR: float64(1000 + i)},
					{Playlist: 13, Tier: 22, Division: 0, MMR: 1800.0},
					{Playlist: 27, Tier: 0, Division: 2, MMR: 600.0},
				}
				jsonStr, err := SerializeRanksJSON(skills)
				if err != nil {
					t.Errorf("concurrent serialize failed: %v", err)
					return
				}
				snapshot, err := ParseRanksJSON(jsonStr)
				if err != nil {
					t.Errorf("concurrent parse failed: %v", err)
					return
				}
				if r27, ok := snapshot.GetRank(27); !ok || r27.RankName != "Unranked" {
					t.Errorf("concurrent parse corrupt rank 27: %+v", r27)
				}
			}
		}(g)
	}

	wg.Wait()
}

// ============================================================================
// Adversarial Suite 6: Go Native Fuzz Tests
// ============================================================================

func FuzzParseRanksJSON(f *testing.F) {
	f.Add("")
	f.Add("{}")
	f.Add("null")
	f.Add(`{"11":{"tier":17,"division":3,"mmr":1150.5,"matches_played":100}}`)
	f.Add(`{"11":{"tier":0,"division":2}}`)
	f.Add(`{"13":{"tier":22,"division":1}}`)
	f.Add(`{"-1":{"tier":999,"division":-999}}`)
	f.Add(`{"abc":null}`)

	f.Fuzz(func(t *testing.T, data string) {
		snapshot, err := ParseRanksJSON(data)
		if err != nil {
			return // graceful rejection of invalid JSON
		}
		// Invariant: Snapshot must never crash on GetRank
		_, _ = snapshot.GetRank(11)
		_, _ = snapshot.GetRank(0)
		_, _ = snapshot.GetRank(-1)
		_, _ = snapshot.GetRank(math.MaxInt)

		// Invariant: For all ranks in snapshot, division suppression must hold
		for _, rank := range snapshot {
			if rank.Tier == 0 {
				if strings.Contains(rank.RankName, "Division") {
					t.Fatalf("RankName for Tier 0 contains Division: %q", rank.RankName)
				}
			}
			if rank.Tier == 22 {
				if strings.Contains(rank.RankName, "Division") {
					t.Fatalf("RankName for Tier 22 contains Division: %q", rank.RankName)
				}
			}
		}
	})
}
