package replay

import (
	"strings"
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
		{name: "completely invalid date string returns raw fallback", input: "not-a-valid-date", expected: "not-a-valid-date"},
		{name: "out-of-bounds month and day", input: "2026-99-99 99-99-99", expected: "2026-99-99 99-99-99"},
		{name: "slashes instead of hyphens", input: "2026/10/16 08:10:36", expected: "2026/10/16 08:10:36"},
		{name: "sql injection attempt", input: "2026-10-16'; DROP TABLE matches;--", expected: "2026-10-16'; DROP TABLE matches;--"},
		{name: "emoji date string", input: "📅2026-10-16 08-10-36", expected: "📅2026-10-16 08-10-36"},
		{name: "very long malformed string (1000 chars)", input: strings.Repeat("A", 1000), expected: strings.Repeat("A", 1000)},

		// Empty strings and whitespace
		{name: "empty string", input: "", expectEmpty: true},
		{name: "spaces only", input: "    ", expectEmpty: true},
		{name: "tabs and newlines only", input: "\t\r\n", expectEmpty: true},

		// Epoch 0
		{name: "epoch 0 psyonix format", input: "1970-01-01 00-00-00", expectUTC: true, expected: "1970-01-01T00:00:00Z"},
		{name: "epoch 0 rfc3339 format", input: "1970-01-01T00:00:00Z", expectUTC: true, expected: "1970-01-01T00:00:00Z"},
		{name: "epoch 0 date only", input: "1970-01-01", expectUTC: true, expected: "1970-01-01T00:00:00Z"},

		// Future dates
		{name: "distant future 2099 psyonix format", input: "2099-12-31 23-59-59", expectUTC: true, expected: "2099-12-31T23:59:59Z"},
		{name: "year 3000 space format", input: "3000-01-01 12:00:00", expectUTC: true, expected: "3000-01-01T12:00:00Z"},

		// Timezones with fractional seconds
		{name: "rfc3339 with positive timezone offset and fractional seconds", input: "2026-10-16T08:10:36.123456+05:30", expectUTC: true, expected: "2026-10-16T02:40:36Z"},
		{name: "rfc3339 with negative timezone offset and fractional seconds", input: "2026-10-16T08:10:36.999999-07:00", expectUTC: true, expected: "2026-10-16T15:10:36Z"},
		{name: "rfc3339 with UTC Z and millisecond precision", input: "2026-10-16T08:10:36.500Z", expectUTC: true, expected: "2026-10-16T08:10:36Z"},
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
		props := map[string]*propertyValue{"MatchStartEpoch": {int64Val: 1729066236}}
		populateMetadata(meta, props)
		if meta.Date != "2024-10-16T08:10:36Z" {
			t.Errorf("expected '2024-10-16T08:10:36Z', got %q", meta.Date)
		}
	})

	t.Run("empty Date with valid MatchStartEpoch", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{"Date": {strVal: "   "}, "MatchStartEpoch": {int64Val: 1729066236}}
		populateMetadata(meta, props)
		if meta.Date != "2024-10-16T08:10:36Z" {
			t.Errorf("expected fallback to MatchStartEpoch '2024-10-16T08:10:36Z', got %q", meta.Date)
		}
	})

	t.Run("missing Date with MatchStartEpoch = 0", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{"MatchStartEpoch": {int64Val: 0}}
		populateMetadata(meta, props)
		if meta.Date != "" {
			t.Errorf("expected empty Date when MatchStartEpoch <= 0, got %q", meta.Date)
		}
	})

	t.Run("missing Date with negative MatchStartEpoch", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{"MatchStartEpoch": {int64Val: -100}}
		populateMetadata(meta, props)
		if meta.Date != "" {
			t.Errorf("expected empty Date when MatchStartEpoch is negative, got %q", meta.Date)
		}
	})

	t.Run("missing both Date and MatchStartEpoch", func(t *testing.T) {
		meta := &ReplayMetadata{}
		populateMetadata(meta, map[string]*propertyValue{})
		if meta.Date != "" {
			t.Errorf("expected empty Date when both missing, got %q", meta.Date)
		}
	})
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
		{name: "1-goal diff Team0 won (3-2) >315s -> true", seconds: 325.0, team0: 3, team1: 2, expectedOT: true},
		{name: "1-goal diff Team1 won (1-2) >315s -> true", seconds: 325.0, team0: 1, team1: 2, expectedOT: true},
		{name: "2-goal diff (4-2) >315s -> false (cannot end sudden death by 2 goals)", seconds: 330.0, team0: 4, team1: 2, expectedOT: false},
		{name: "2-goal diff (0-2) >315s -> false", seconds: 340.0, team0: 0, team1: 2, expectedOT: false},
		{name: "3-goal diff (5-2) >315s -> false", seconds: 350.0, team0: 5, team1: 2, expectedOT: false},
		{name: "tied score (2-2) >315s -> false (sudden death cannot end tied)", seconds: 350.0, team0: 2, team1: 2, expectedOT: false},
		{name: "tied score (0-0) >315s -> false", seconds: 400.0, team0: 0, team1: 0, expectedOT: false},

		// Forfeit behavior
		{name: "forfeit flag true despite >315s and 1-goal diff -> false", seconds: 330.0, team0: 3, team1: 2, forfeit: true, expectedOT: false},
		{name: "forfeit flag true at 600s -> false", seconds: 600.0, team0: 2, team1: 1, forfeit: true, expectedOT: false},
		{name: "explicit bOvertime true overrides forfeit flag", seconds: 330.0, team0: 3, team1: 2, forfeit: true, props: map[string]*propertyValue{"bOvertime": {boolVal: true}}, expectedOT: true},

		// Explicit properties precedence
		{name: "explicit OvertimeSeconds overrides regulation duration", seconds: 200.0, team0: 2, team1: 1, props: map[string]*propertyValue{"OvertimeSeconds": {intVal: 35}}, expectedOT: true},
		{name: "explicit OvertimeSeconds as floatVal", seconds: 200.0, team0: 2, team1: 1, props: map[string]*propertyValue{"OvertimeSeconds": {floatVal: 12.5}}, expectedOT: true},
		{name: "explicit OvertimeSeconds zero does not trigger overtime", seconds: 200.0, team0: 2, team1: 1, props: map[string]*propertyValue{"OvertimeSeconds": {intVal: 0}}, expectedOT: false},
		{name: "explicit OvertimeSeconds negative does not trigger overtime", seconds: 200.0, team0: 2, team1: 1, props: map[string]*propertyValue{"OvertimeSeconds": {intVal: -15}}, expectedOT: false},
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
			if actual := extractOvertime(meta, props); actual != tc.expectedOT {
				t.Errorf("extractOvertime = %v, expected %v", actual, tc.expectedOT)
			}
		})
	}
}

// TestAdversarial_PlayerStats_AdversarialPayloads tests corrupt player arrays, negative demos, and platforms.
func TestAdversarial_PlayerStats_AdversarialPayloads(t *testing.T) {
	meta := &ReplayMetadata{}
	props := map[string]*propertyValue{
		"PlayerStats": {
			subProps: []map[string]*propertyValue{
				{"Name": {strVal: "AggressiveDemolisher"}, "Score": {intVal: -50}, "Demos": {intVal: -5}, "Team": {intVal: 0}},
				{"Name": {strVal: "PlayerWithDemolitionsKey"}, "Demolitions": {intVal: 8}, "Team": {intVal: 1}},
				{"Name": {strVal: "PlayerWithDemolitionKey"}, "Demolition": {intVal: 4}},
				{"Name": {strVal: "PlayerWithDemosKey"}, "demos": {intVal: 2}},
				{"Name": {strVal: "Alex_Smith_99"}, "bBot": {boolVal: true}, "Team": {intVal: 0}},
				{"Name": {strVal: "Saltie"}, "bBot": {boolVal: false}, "Team": {intVal: 1}},
				{"Name": {strVal: "CloudGamer"}, "Platform": {strVal: "AmazonLuna"}},
				{"Name": {strVal: "DingoPlayer"}, "Platform": {strVal: "OnlinePlatform_Dingo"}},
				{"Name": {strVal: "ConsolePlayer"}, "Platform": {strVal: "OnlinePlatform_PS5"}},
				{"Name": {strVal: "MysteriousPlayer"}, "Platform": {strVal: ""}},
				{
					"Name": {strVal: "EpicPro"},
					"PlayerID": {
						subProps: []map[string]*propertyValue{
							{"EpicAccountId": {strVal: "epic-acc-12345"}, "Platform": {strVal: "OnlinePlatform_EOS"}},
						},
					},
				},
				{
					"Name": {strVal: "SteamGamer"},
					"PlayerID": {
						subProps: []map[string]*propertyValue{{"Uid": {int64Val: 76561198012345678}}},
					},
				},
				{},
			},
		},
	}

	populateMetadata(meta, props)
	if len(meta.Players) != 13 {
		t.Fatalf("expected 13 players, got %d", len(meta.Players))
	}

	if p0 := meta.Players[0]; p0.Demos != -5 || p0.Score != -50 {
		t.Errorf("p0 demos=%d, score=%d, expected -5, -50", p0.Demos, p0.Score)
	}
	if p1 := meta.Players[1]; p1.Demos != 8 {
		t.Errorf("p1 demos=%d, expected 8", p1.Demos)
	}
	if p2 := meta.Players[2]; p2.Demos != 4 {
		t.Errorf("p2 demos=%d, expected 4", p2.Demos)
	}
	if p3 := meta.Players[3]; p3.Demos != 2 {
		t.Errorf("p3 demos=%d, expected 2", p3.Demos)
	}
	if !meta.Players[4].IsBot {
		t.Errorf("p4 expected IsBot=true")
	}
	if meta.Players[5].IsBot {
		t.Errorf("p5 expected IsBot=false")
	}
	if p6 := meta.Players[6]; p6.Platform != "AmazonLuna" {
		t.Errorf("p6 expected Platform='AmazonLuna', got %q", p6.Platform)
	}
	if p7 := meta.Players[7]; p7.Platform != "Xbox" {
		t.Errorf("p7 expected Platform='Xbox', got %q", p7.Platform)
	}
	if p8 := meta.Players[8]; p8.Platform != "PlayStation" {
		t.Errorf("p8 expected Platform='PlayStation', got %q", p8.Platform)
	}
	if p9 := meta.Players[9]; p9.Platform != "Unknown" {
		t.Errorf("p9 expected Platform='Unknown', got %q", p9.Platform)
	}
	if p10 := meta.Players[10]; p10.OnlineID != "epic-acc-12345" || p10.Platform != "Epic" {
		t.Errorf("p10 OnlineID=%q, Platform=%q, expected 'epic-acc-12345', 'Epic'", p10.OnlineID, p10.Platform)
	}
	if p11 := meta.Players[11]; p11.OnlineID != "76561198012345678" || p11.Platform != "Steam" {
		t.Errorf("p11 OnlineID=%q, Platform=%q, expected '76561198012345678', 'Steam'", p11.OnlineID, p11.Platform)
	}
	if p12 := meta.Players[12]; p12.Name != "" || p12.Score != 0 || p12.Platform != "Unknown" {
		t.Errorf("p12 expected empty defaults with Platform='Unknown', got %+v", p12)
	}
}
