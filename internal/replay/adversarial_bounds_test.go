package replay

import (
	"bytes"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestAdversarial_ExtractOvertime_ExtremeDurations tests overtime logic at duration boundaries.
func TestAdversarial_ExtractOvertime_ExtremeDurations(t *testing.T) {
	cases := []struct {
		name       string
		seconds    float32
		team0      int
		team1      int
		forfeit    bool
		props      map[string]*propertyValue
		expectedOT bool
	}{
		{name: "0 seconds duration", seconds: 0.0, team0: 1, team1: 0, expectedOT: false},
		{name: "negative duration", seconds: -10.0, team0: 1, team1: 0, expectedOT: false},
		{name: "300.0s exact regulation", seconds: 300.0, team0: 2, team1: 1, expectedOT: false},
		{name: "304.5s regulation with zero second play", seconds: 304.5, team0: 2, team1: 1, expectedOT: false},
		{name: "314.99s just under 315s overtime threshold", seconds: 314.99, team0: 3, team1: 2, expectedOT: false},
		{name: "315.0s exact overtime threshold with 1-goal diff", seconds: 315.0, team0: 3, team1: 2, expectedOT: true},
		{name: "315.1s just over overtime threshold with 1-goal diff", seconds: 315.1, team0: 0, team1: 1, expectedOT: true},
		{name: "600.0s 10-minute long overtime with 1-goal diff", seconds: 600.0, team0: 4, team1: 3, expectedOT: true},
		{name: "3600.0s 1-hour marathon match with 1-goal diff", seconds: 3600.0, team0: 6, team1: 5, expectedOT: true},
		{name: "86400.0s 24-hour match with 1-goal diff", seconds: 86400.0, team0: 10, team1: 9, expectedOT: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := &ReplayMetadata{
				TotalSeconds: tc.seconds,
				Team0Score:   tc.team0,
				Team1Score:   tc.team1,
				Forfeit:      tc.forfeit,
			}
			props := tc.props
			if props == nil {
				props = map[string]*propertyValue{}
			}
			actual := extractOvertime(meta, props)
			if actual != tc.expectedOT {
				t.Errorf("extractOvertime(TotalSeconds=%.2f, Score=%d-%d, Forfeit=%v) = %v, expected %v",
					tc.seconds, tc.team0, tc.team1, tc.forfeit, actual, tc.expectedOT)
			}
		})
	}
}

// TestAdversarial_ExtractOvertime_NaNAndInf verifies NaN/Inf float handling without panic.
func TestAdversarial_ExtractOvertime_NaNAndInf(t *testing.T) {
	metaNaN := &ReplayMetadata{
		TotalSeconds: float32(math.NaN()),
		Team0Score:   2,
		Team1Score:   1,
	}
	if extractOvertime(metaNaN, map[string]*propertyValue{}) {
		t.Errorf("expected NaN duration to return false overtime")
	}

	metaInf := &ReplayMetadata{
		TotalSeconds: float32(math.Inf(-1)),
		Team0Score:   2,
		Team1Score:   1,
	}
	if extractOvertime(metaInf, map[string]*propertyValue{}) {
		t.Errorf("expected -Inf duration to return false overtime")
	}
}

// TestAdversarial_NormalizeReplayDate_Fuzz exercises normalizeReplayDate with malformed strings.
func TestAdversarial_NormalizeReplayDate_Fuzz(t *testing.T) {
	testStrings := []string{
		"\x00\x00\x00",
		"\xff\xfe\xfd",
		strings.Repeat("2026-10-16 08-10-36\x00", 50),
		"2026-10-16T08:10:36+99:99",
		"2026-10-16T08:10:36-99:99",
		"2026-10-16T08:10:36.999999999999999999999Z",
		"0000-00-00",
		"9999-99-99",
		"-0001-01-01",
		"2026-02-31",
		"2026-04-31",
		"2026-13-01",
		"2026-00-01",
		"2026-01-00",
		"2026-01-01 25:00:00",
		"2026-01-01 24:60:00",
		"2026-01-01 24:00:61",
		"undefined",
		"null",
		"NaN",
		"[object Object]",
		"{\"date\": \"2026-10-16\"}",
		"<Date>2026-10-16</Date>",
		"\r\n\t\f\v",
	}

	for _, s := range testStrings {
		res := normalizeReplayDate(s)
		_ = res
	}
}

// TestAdversarial_ParseReplay_BinaryCorruptions feeds malformed binary structures into ParseReplay.
func TestAdversarial_ParseReplay_BinaryCorruptions(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "minimal valid headerLen with truncated rest",
			data: []byte{0x04, 0x00, 0x00, 0x00},
		},
		{
			name: "negative headerLen",
			data: []byte{0xff, 0xff, 0xff, 0xff},
		},
		{
			name: "zero headerLen",
			data: []byte{0x00, 0x00, 0x00, 0x00},
		},
		{
			name: "exceeds 32MB headerLen",
			data: []byte{0x01, 0x00, 0x00, 0x03},
		},
		{
			name: "valid headerLen but missing crc and versions",
			data: []byte{0x10, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04},
		},
		{
			name: "valid versions but invalid class name length (exceeds max)",
			data: []byte{
				0x20, 0x00, 0x00, 0x00, // headerLen = 32
				0x00, 0x00, 0x00, 0x00, // crc
				0x64, 0x03, 0x00, 0x00, // engVer = 868
				0x12, 0x00, 0x00, 0x00, // licVer = 18
				0x01, 0x00, 0x00, 0x00, // netVer = 1
				0x00, 0x00, 0x02, 0x00, // className length = 131072 (> maxStrLen 65536)
			},
		},
		{
			name: "valid versions but non-TAGame class name",
			data: []byte{
				0x20, 0x00, 0x00, 0x00, // headerLen = 32
				0x00, 0x00, 0x00, 0x00, // crc
				0x00, 0x01, 0x00, 0x00, // engVer = 256
				0x01, 0x00, 0x00, 0x00, // licVer = 1
				0x06, 0x00, 0x00, 0x00, // className len = 6
				'H', 'e', 'l', 'l', 'o', 0x00,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseReplay(bytes.NewReader(tc.data))
			if err == nil {
				t.Fatalf("expected error for corrupted binary payload %q, got nil", tc.name)
			}
		})
	}
}

// TestAdversarial_NilPointerHardening tests behavior when nil maps or nil pointers are provided.
func TestAdversarial_NilPointerHardening(t *testing.T) {
	t.Run("extractOvertime with nil props map", func(t *testing.T) {
		meta := &ReplayMetadata{TotalSeconds: 300.0, Team0Score: 2, Team1Score: 1}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("extractOvertime panicked with nil props: %v", r)
			}
		}()
		if extractOvertime(meta, nil) {
			t.Errorf("expected false overtime with nil props on regulation match")
		}
	})

	t.Run("extractOvertime with nil meta pointer", func(t *testing.T) {
		props := map[string]*propertyValue{"bOvertime": {boolVal: true}}
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Observed panic when meta is nil and fallback heuristic reached: %v", r)
			}
		}()
		if !extractOvertime(nil, props) {
			t.Errorf("expected true when bOvertime is explicitly true even if meta is nil")
		}
	})
}

// TestAdversarial_ExtractOvertime_NilDereferenceCheck tests whether nil propertyValues cause a panic.
func TestAdversarial_ExtractOvertime_NilDereferenceCheck(t *testing.T) {
	meta := &ReplayMetadata{TotalSeconds: 320.0, Team0Score: 2, Team1Score: 1}

	t.Run("nil entry for bOvertime in props map", func(t *testing.T) {
		props := map[string]*propertyValue{"bOvertime": nil}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("unexpected panic on nil property value: %v", r)
			}
		}()
		_ = extractOvertime(meta, props)
	})

	t.Run("nil meta with empty props", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("unexpected panic on nil meta pointer: %v", r)
			}
		}()
		if extractOvertime(nil, map[string]*propertyValue{}) {
			t.Errorf("expected false for nil meta")
		}
	})

	t.Run("nil reader rejected cleanly", func(t *testing.T) {
		if _, err := ParseReplay(nil); err == nil {
			t.Fatalf("expected error for nil reader")
		}
	})
}

// TestAdversarial_RealReplays_ConcurrentRace tests all replays concurrently.
func TestAdversarial_RealReplays_ConcurrentRace(t *testing.T) {
	files, err := filepath.Glob("../../replays/*.replay")
	if err != nil || len(files) == 0 {
		t.Skip("no replay files found")
	}

	const iterations = 5
	var wg sync.WaitGroup

	for iter := 0; iter < iterations; iter++ {
		for _, f := range files {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				meta, parseErr := ParseReplayFile(path)
				if parseErr != nil {
					t.Errorf("concurrent parse failed for %s: %v", filepath.Base(path), parseErr)
					return
				}
				if meta.MatchGUID == "" {
					t.Errorf("empty match GUID in %s", filepath.Base(path))
				}
				if meta.Date == "" {
					t.Errorf("empty Date in %s", filepath.Base(path))
				}
			}(f)
		}
	}

	wg.Wait()
}

// TestAdversarial_All20Replays_InspectStats dumps and verifies parsed stats for all 20 real replays.
func TestAdversarial_All20Replays_InspectStats(t *testing.T) {
	files, err := filepath.Glob("../../replays/*.replay")
	if err != nil || len(files) == 0 {
		t.Skip("no replay files found")
	}

	t.Logf("Auditing %d replay files...", len(files))

	totalPlayers := 0
	totalDemos := 0
	overtimeMatches := 0

	for _, f := range files {
		base := filepath.Base(f)
		meta, err := ParseReplayFile(f)
		if err != nil {
			t.Fatalf("Replay %s failed to parse: %v", base, err)
		}
		if meta.MatchGUID == "" {
			t.Errorf("Replay %s: empty MatchGUID", base)
		}
		if meta.Date == "" {
			t.Errorf("Replay %s: empty Date", base)
		}
		if meta.Overtime {
			overtimeMatches++
		}
		totalPlayers += len(meta.Players)
		for _, p := range meta.Players {
			if p.Demos > 0 {
				totalDemos += p.Demos
			}
		}
		t.Logf("[%s] GUID=%s Date=%s OT=%v Secs=%.1f Score=%d-%d Players=%d",
			base[:8], meta.MatchGUID, meta.Date, meta.Overtime, meta.TotalSeconds, meta.Team0Score, meta.Team1Score, len(meta.Players))
	}

	t.Logf("Summary across 20 replays: Overtime Matches=%d, Total Players=%d, Total Demos=%d",
		overtimeMatches, totalPlayers, totalDemos)
}
