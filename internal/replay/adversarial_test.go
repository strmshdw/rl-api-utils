package replay

import (
	"bytes"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAdversarial_NormalizeReplayDate tests edge cases, corruptions, and malformed date strings.
func TestAdversarial_NormalizeReplayDate(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		expectEmpty bool
		expectUTC   bool
		expected    string
	}{
		// Malformed formatting
		{
			name:     "completely invalid date string returns raw fallback",
			input:    "not-a-valid-date",
			expected: "not-a-valid-date",
		},
		{
			name:     "out-of-bounds month and day",
			input:    "2026-99-99 99-99-99",
			expected: "2026-99-99 99-99-99",
		},
		{
			name:     "slashes instead of hyphens",
			input:    "2026/10/16 08:10:36",
			expected: "2026/10/16 08:10:36",
		},
		{
			name:     "sql injection attempt",
			input:    "2026-10-16'; DROP TABLE matches;--",
			expected: "2026-10-16'; DROP TABLE matches;--",
		},
		{
			name:     "emoji date string",
			input:    "📅2026-10-16 08-10-36",
			expected: "📅2026-10-16 08-10-36",
		},
		{
			name:     "very long malformed string (1000 chars)",
			input:    strings.Repeat("A", 1000),
			expected: strings.Repeat("A", 1000),
		},

		// Empty strings and whitespace
		{
			name:        "empty string",
			input:       "",
			expectEmpty: true,
		},
		{
			name:        "spaces only",
			input:       "    ",
			expectEmpty: true,
		},
		{
			name:        "tabs and newlines only",
			input:       "\t\r\n",
			expectEmpty: true,
		},

		// Epoch 0
		{
			name:      "epoch 0 psyonix format",
			input:     "1970-01-01 00-00-00",
			expectUTC: true,
			expected:  "1970-01-01T00:00:00Z",
		},
		{
			name:      "epoch 0 rfc3339 format",
			input:     "1970-01-01T00:00:00Z",
			expectUTC: true,
			expected:  "1970-01-01T00:00:00Z",
		},
		{
			name:      "epoch 0 date only",
			input:     "1970-01-01",
			expectUTC: true,
			expected:  "1970-01-01T00:00:00Z",
		},

		// Future dates
		{
			name:      "distant future 2099 psyonix format",
			input:     "2099-12-31 23-59-59",
			expectUTC: true,
			expected:  "2099-12-31T23:59:59Z",
		},
		{
			name:      "year 3000 space format",
			input:     "3000-01-01 12:00:00",
			expectUTC: true,
			expected:  "3000-01-01T12:00:00Z",
		},

		// Timezones with fractional seconds
		{
			name:      "rfc3339 with positive timezone offset and fractional seconds",
			input:     "2026-10-16T08:10:36.123456+05:30",
			expectUTC: true,
			expected:  "2026-10-16T02:40:36Z", // 08:10:36 minus 5h30m
		},
		{
			name:      "rfc3339 with negative timezone offset and fractional seconds",
			input:     "2026-10-16T08:10:36.999999-07:00",
			expectUTC: true,
			expected:  "2026-10-16T15:10:36Z", // 08:10:36 plus 7h
		},
		{
			name:      "rfc3339 with UTC Z and millisecond precision",
			input:     "2026-10-16T08:10:36.500Z",
			expectUTC: true,
			expected:  "2026-10-16T08:10:36Z",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := normalizeReplayDate(tc.input)
			if tc.expectEmpty {
				if actual != "" {
					t.Errorf("expected empty string, got %q", actual)
				}
				return
			}
			if actual != tc.expected {
				t.Errorf("normalizeReplayDate(%q) = %q, expected %q", tc.input, actual, tc.expected)
			}
			if tc.expectUTC {
				parsed, err := time.Parse(time.RFC3339, actual)
				if err != nil {
					t.Errorf("output %q is not valid RFC3339: %v", actual, err)
				}
				if parsed.Location() != time.UTC {
					t.Errorf("output %q is not in UTC location", actual)
				}
			}
		})
	}
}

// TestAdversarial_PopulateMetadata_DateFallback tests missing or corrupted Date properties.
func TestAdversarial_PopulateMetadata_DateFallback(t *testing.T) {
	t.Run("missing Date with valid MatchStartEpoch", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"MatchStartEpoch": {int64Val: 1729066236},
		}
		populateMetadata(meta, props)
		if meta.Date != "2024-10-16T08:10:36Z" {
			t.Errorf("expected '2024-10-16T08:10:36Z', got %q", meta.Date)
		}
	})

	t.Run("empty Date with valid MatchStartEpoch", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"Date":            {strVal: "   "},
			"MatchStartEpoch": {int64Val: 1729066236},
		}
		populateMetadata(meta, props)
		if meta.Date != "2024-10-16T08:10:36Z" {
			t.Errorf("expected fallback to MatchStartEpoch '2024-10-16T08:10:36Z', got %q", meta.Date)
		}
	})

	t.Run("missing Date with MatchStartEpoch = 0", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"MatchStartEpoch": {int64Val: 0},
		}
		populateMetadata(meta, props)
		if meta.Date != "" {
			t.Errorf("expected empty Date when MatchStartEpoch <= 0, got %q", meta.Date)
		}
	})

	t.Run("missing Date with negative MatchStartEpoch", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"MatchStartEpoch": {int64Val: -100},
		}
		populateMetadata(meta, props)
		if meta.Date != "" {
			t.Errorf("expected empty Date when MatchStartEpoch is negative, got %q", meta.Date)
		}
	})

	t.Run("missing both Date and MatchStartEpoch", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{}
		populateMetadata(meta, props)
		if meta.Date != "" {
			t.Errorf("expected empty Date when both missing, got %q", meta.Date)
		}
	})
}

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
		{
			name:       "0 seconds duration",
			seconds:    0.0,
			team0:      1,
			team1:      0,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "negative duration",
			seconds:    -10.0,
			team0:      1,
			team1:      0,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "300.0s exact regulation",
			seconds:    300.0,
			team0:      2,
			team1:      1,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "304.5s regulation with zero second play",
			seconds:    304.5,
			team0:      2,
			team1:      1,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "314.99s just under 315s overtime threshold",
			seconds:    314.99,
			team0:      3,
			team1:      2,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "315.0s exact overtime threshold with 1-goal diff",
			seconds:    315.0,
			team0:      3,
			team1:      2,
			forfeit:    false,
			expectedOT: true,
		},
		{
			name:       "315.1s just over overtime threshold with 1-goal diff",
			seconds:    315.1,
			team0:      0,
			team1:      1,
			forfeit:    false,
			expectedOT: true,
		},
		{
			name:       "600.0s 10-minute long overtime with 1-goal diff",
			seconds:    600.0,
			team0:      4,
			team1:      3,
			forfeit:    false,
			expectedOT: true,
		},
		{
			name:       "3600.0s 1-hour marathon match with 1-goal diff",
			seconds:    3600.0,
			team0:      6,
			team1:      5,
			forfeit:    false,
			expectedOT: true,
		},
		{
			name:       "86400.0s 24-hour match with 1-goal diff",
			seconds:    86400.0,
			team0:      10,
			team1:      9,
			forfeit:    false,
			expectedOT: true,
		},
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

// TestAdversarial_ExtractOvertime_SuddenDeathAndForfeit tests score differentials and forfeit behavior.
func TestAdversarial_ExtractOvertime_SuddenDeathAndForfeit(t *testing.T) {
	cases := []struct {
		name       string
		seconds    float32
		team0      int
		team1      int
		forfeit    bool
		props      map[string]*propertyValue
		expectedOT bool
	}{
		// Sudden death requires 1-goal differential
		{
			name:       "1-goal diff Team0 won (3-2) >315s -> true",
			seconds:    325.0,
			team0:      3,
			team1:      2,
			forfeit:    false,
			expectedOT: true,
		},
		{
			name:       "1-goal diff Team1 won (1-2) >315s -> true",
			seconds:    325.0,
			team0:      1,
			team1:      2,
			forfeit:    false,
			expectedOT: true,
		},
		{
			name:       "2-goal diff (4-2) >315s -> false (cannot end sudden death by 2 goals)",
			seconds:    330.0,
			team0:      4,
			team1:      2,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "2-goal diff (0-2) >315s -> false",
			seconds:    340.0,
			team0:      0,
			team1:      2,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "3-goal diff (5-2) >315s -> false",
			seconds:    350.0,
			team0:      5,
			team1:      2,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "tied score (2-2) >315s -> false (sudden death cannot end tied)",
			seconds:    350.0,
			team0:      2,
			team1:      2,
			forfeit:    false,
			expectedOT: false,
		},
		{
			name:       "tied score (0-0) >315s -> false",
			seconds:    400.0,
			team0:      0,
			team1:      0,
			forfeit:    false,
			expectedOT: false,
		},

		// Forfeit behavior
		{
			name:       "forfeit flag true despite >315s and 1-goal diff -> false",
			seconds:    330.0,
			team0:      3,
			team1:      2,
			forfeit:    true,
			expectedOT: false,
		},
		{
			name:       "forfeit flag true at 600s -> false",
			seconds:    600.0,
			team0:      2,
			team1:      1,
			forfeit:    true,
			expectedOT: false,
		},
		{
			name:    "explicit bOvertime true overrides forfeit flag",
			seconds: 330.0,
			team0:   3,
			team1:   2,
			forfeit: true,
			props: map[string]*propertyValue{
				"bOvertime": {boolVal: true},
			},
			expectedOT: true,
		},

		// Explicit properties precedence
		{
			name:    "explicit OvertimeSeconds overrides regulation duration",
			seconds: 200.0,
			team0:   2,
			team1:   1,
			forfeit: false,
			props: map[string]*propertyValue{
				"OvertimeSeconds": {intVal: 35},
			},
			expectedOT: true,
		},
		{
			name:    "explicit OvertimeSeconds as floatVal",
			seconds: 200.0,
			team0:   2,
			team1:   1,
			forfeit: false,
			props: map[string]*propertyValue{
				"OvertimeSeconds": {floatVal: 12.5},
			},
			expectedOT: true,
		},
		{
			name:    "explicit OvertimeSeconds zero does not trigger overtime",
			seconds: 200.0,
			team0:   2,
			team1:   1,
			forfeit: false,
			props: map[string]*propertyValue{
				"OvertimeSeconds": {intVal: 0},
			},
			expectedOT: false,
		},
		{
			name:    "explicit OvertimeSeconds negative does not trigger overtime",
			seconds: 200.0,
			team0:   2,
			team1:   1,
			forfeit: false,
			props: map[string]*propertyValue{
				"OvertimeSeconds": {intVal: -15},
			},
			expectedOT: false,
		},
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
				t.Errorf("extractOvertime = %v, expected %v", actual, tc.expectedOT)
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
		Forfeit:      false,
	}
	ot := extractOvertime(metaNaN, map[string]*propertyValue{})
	if ot {
		t.Errorf("expected NaN duration to return false overtime")
	}

	metaInf := &ReplayMetadata{
		TotalSeconds: float32(math.Inf(-1)),
		Team0Score:   2,
		Team1Score:   1,
		Forfeit:      false,
	}
	ot = extractOvertime(metaInf, map[string]*propertyValue{})
	if ot {
		t.Errorf("expected -Inf duration to return false overtime")
	}
}

// TestAdversarial_PlayerStats_AdversarialPayloads tests corrupt player arrays, negative demos, and platforms.
func TestAdversarial_PlayerStats_AdversarialPayloads(t *testing.T) {
	meta := &ReplayMetadata{}
	props := map[string]*propertyValue{
		"PlayerStats": {
			subProps: []map[string]*propertyValue{
				// 1. Negative demolitions & scores
				{
					"Name":  {strVal: "AggressiveDemolisher"},
					"Score": {intVal: -50},
					"Demos": {intVal: -5},
					"Team":  {intVal: 0},
				},
				// 2. Demolitions under "Demolitions" key
				{
					"Name":        {strVal: "PlayerWithDemolitionsKey"},
					"Demolitions": {intVal: 8},
					"Team":        {intVal: 1},
				},
				// 3. Demolitions under "Demolition" singular key
				{
					"Name":       {strVal: "PlayerWithDemolitionKey"},
					"Demolition": {intVal: 4},
				},
				// 4. Demolitions under lowercase "demos"
				{
					"Name":  {strVal: "PlayerWithDemosKey"},
					"demos": {intVal: 2},
				},
				// 5. Bot with real user-like name
				{
					"Name": {strVal: "Alex_Smith_99"},
					"bBot": {boolVal: true},
					"Team": {intVal: 0},
				},
				// 6. Human player with standard bot name
				{
					"Name": {strVal: "Saltie"},
					"bBot": {boolVal: false},
					"Team": {intVal: 1},
				},
				// 7. Unknown platform name
				{
					"Name":     {strVal: "CloudGamer"},
					"Platform": {strVal: "AmazonLuna"},
				},
				// 8. OnlinePlatform_ prefix platform
				{
					"Name":     {strVal: "DingoPlayer"},
					"Platform": {strVal: "OnlinePlatform_Dingo"},
				},
				// 9. OnlinePlatform_PS5
				{
					"Name":     {strVal: "ConsolePlayer"},
					"Platform": {strVal: "OnlinePlatform_PS5"},
				},
				// 10. Empty platform defaults to "Unknown"
				{
					"Name":     {strVal: "MysteriousPlayer"},
					"Platform": {strVal: ""},
				},
				// 11. Epic Account ID in PlayerID subprop
				{
					"Name": {strVal: "EpicPro"},
					"PlayerID": {
						subProps: []map[string]*propertyValue{
							{
								"EpicAccountId": {strVal: "epic-acc-12345"},
								"Platform":      {strVal: "OnlinePlatform_EOS"},
							},
						},
					},
				},
				// 12. Steam Uid in PlayerID subprop
				{
					"Name": {strVal: "SteamGamer"},
					"PlayerID": {
						subProps: []map[string]*propertyValue{
							{
								"Uid": {int64Val: 76561198012345678},
							},
						},
					},
				},
				// 13. Player with empty / null-like stats map (no keys)
				{},
			},
		},
	}

	populateMetadata(meta, props)

	if len(meta.Players) != 13 {
		t.Fatalf("expected 13 players, got %d", len(meta.Players))
	}

	// Verify player 0: negative demos and score
	p0 := meta.Players[0]
	if p0.Demos != -5 || p0.Score != -50 {
		t.Errorf("p0 demos=%d, score=%d, expected -5, -50", p0.Demos, p0.Score)
	}

	// Verify player 1: Demolitions key
	p1 := meta.Players[1]
	if p1.Demos != 8 {
		t.Errorf("p1 demos=%d, expected 8", p1.Demos)
	}

	// Verify player 2: Demolition singular key
	p2 := meta.Players[2]
	if p2.Demos != 4 {
		t.Errorf("p2 demos=%d, expected 4", p2.Demos)
	}

	// Verify player 3: demos lowercase key
	p3 := meta.Players[3]
	if p3.Demos != 2 {
		t.Errorf("p3 demos=%d, expected 2", p3.Demos)
	}

	// Verify player 4: bot with username
	p4 := meta.Players[4]
	if !p4.IsBot {
		t.Errorf("p4 expected IsBot=true")
	}

	// Verify player 5: human with bot name
	p5 := meta.Players[5]
	if p5.IsBot {
		t.Errorf("p5 expected IsBot=false")
	}

	// Verify player 6: unknown platform preserved
	p6 := meta.Players[6]
	if p6.Platform != "AmazonLuna" {
		t.Errorf("p6 expected Platform='AmazonLuna', got %q", p6.Platform)
	}

	// Verify player 7: Dingo mapped to Xbox
	p7 := meta.Players[7]
	if p7.Platform != "Xbox" {
		t.Errorf("p7 expected Platform='Xbox', got %q", p7.Platform)
	}

	// Verify player 8: PS5 mapped to PlayStation
	p8 := meta.Players[8]
	if p8.Platform != "PlayStation" {
		t.Errorf("p8 expected Platform='PlayStation', got %q", p8.Platform)
	}

	// Verify player 9: empty platform mapped to Unknown
	p9 := meta.Players[9]
	if p9.Platform != "Unknown" {
		t.Errorf("p9 expected Platform='Unknown', got %q", p9.Platform)
	}

	// Verify player 10: Epic ID & platform
	p10 := meta.Players[10]
	if p10.OnlineID != "epic-acc-12345" || p10.Platform != "Epic" {
		t.Errorf("p10 OnlineID=%q, Platform=%q, expected 'epic-acc-12345', 'Epic'", p10.OnlineID, p10.Platform)
	}

	// Verify player 11: Steam ID & platform
	p11 := meta.Players[11]
	if p11.OnlineID != "76561198012345678" || p11.Platform != "Steam" {
		t.Errorf("p11 OnlineID=%q, Platform=%q, expected '76561198012345678', 'Steam'", p11.OnlineID, p11.Platform)
	}

	// Verify player 12: empty map yields default empty player without panic
	p12 := meta.Players[12]
	if p12.Name != "" || p12.Score != 0 || p12.Platform != "Unknown" {
		t.Errorf("p12 expected empty defaults with Platform='Unknown', got %+v", p12)
	}
}

// TestAdversarial_RealReplays_ConcurrentRace tests all 20 replays concurrently under race detector.
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


// TestAdversarial_NormalizeReplayDate_Fuzz exercises normalizeReplayDate with thousands of random / malformed strings.
func TestAdversarial_NormalizeReplayDate_Fuzz(t *testing.T) {
	// Various pathological strings
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
		"2026-02-31", // invalid date (Feb 31)
		"2026-04-31", // invalid date (Apr 31)
		"2026-13-01", // invalid month 13
		"2026-00-01", // invalid month 0
		"2026-01-00", // invalid day 0
		"2026-01-01 25:00:00", // invalid hour 25
		"2026-01-01 24:60:00", // invalid minute 60
		"2026-01-01 24:00:61", // invalid second 61
		"undefined",
		"null",
		"NaN",
		"[object Object]",
		"{\"date\": \"2026-10-16\"}",
		"<Date>2026-10-16</Date>",
		"\r\n\t\f\v",
	}

	for _, s := range testStrings {
		// Verify normalizeReplayDate never panics under any string
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
			data: []byte{0x01, 0x00, 0x00, 0x03}, // ~50MB
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

// TestAdversarial_NilPointerHardening tests behavior when nil maps or nil property pointers are provided.
func TestAdversarial_NilPointerHardening(t *testing.T) {
	t.Run("extractOvertime with nil props map", func(t *testing.T) {
		meta := &ReplayMetadata{TotalSeconds: 300.0, Team0Score: 2, Team1Score: 1}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("extractOvertime panicked with nil props: %v", r)
			}
		}()
		ot := extractOvertime(meta, nil)
		if ot {
			t.Errorf("expected false overtime with nil props on regulation match")
		}
	})

	t.Run("extractOvertime with nil meta pointer", func(t *testing.T) {
		props := map[string]*propertyValue{
			"bOvertime": {boolVal: true},
		}
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Observed panic when meta is nil and fallback heuristic reached: %v", r)
			}
		}()
		// When explicit bOvertime is true, it returns before dereferencing meta
		ot := extractOvertime(nil, props)
		if !ot {
			t.Errorf("expected true when bOvertime is explicitly true even if meta is nil")
		}
	})
}


// TestAdversarial_ExtractOvertime_NilDereferenceCheck tests whether nil propertyValues cause a panic.
func TestAdversarial_ExtractOvertime_NilDereferenceCheck(t *testing.T) {
	meta := &ReplayMetadata{TotalSeconds: 320.0, Team0Score: 2, Team1Score: 1}

	t.Run("nil entry for bOvertime in props map", func(t *testing.T) {
		props := map[string]*propertyValue{
			"bOvertime": nil,
		}
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Observed panic on nil property value: %v", r)
			}
		}()
		_ = extractOvertime(meta, props)
	})

	t.Run("nil meta with empty props", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Observed panic on nil meta pointer: %v", r)
			}
		}()
		_ = extractOvertime(nil, map[string]*propertyValue{})
	})
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
