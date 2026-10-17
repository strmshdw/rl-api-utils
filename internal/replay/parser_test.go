package replay

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func TestParseRealReplays(t *testing.T) {
	files, err := filepath.Glob("../../replays/*.replay")
	if err != nil || len(files) == 0 {
		t.Skip("no replay files found")
	}

	successCount := 0
	knownOvertimeReplays := map[string]bool{
		"1208BA9011F1C938988AAE9F4178846A.replay": true,
		"43D1AEEC11F1C92E068A719C098F0CE2.replay": true,
		"A19AF6C611F1C93FEDAC55B0D8DA96A8.replay": true,
	}

	for _, f := range files {
		base := filepath.Base(f)
		meta, err := ParseReplayFile(f)
		if err != nil {
			t.Errorf("Failed parsing %s: %v", base, err)
			continue
		}
		if meta.MatchGUID == "" {
			t.Errorf("Replay %s had empty MatchGUID", base)
		}
		if meta.Date == "" {
			t.Errorf("Replay %s had empty Date", base)
		} else {
			// Verify normalized to RFC3339
			parsedTime, parseErr := time.Parse(time.RFC3339, meta.Date)
			if parseErr != nil {
				t.Errorf("Replay %s Date %q is not valid RFC3339: %v", base, meta.Date, parseErr)
			} else if parsedTime.IsZero() {
				t.Errorf("Replay %s Date %q parsed to zero time", base, meta.Date)
			}
		}

		if expectedOT, ok := knownOvertimeReplays[base]; ok {
			if meta.Overtime != expectedOT {
				t.Errorf("Replay %s Overtime = %v, expected %v (TotalSeconds: %f, Score: %d-%d)",
					base, meta.Overtime, expectedOT, meta.TotalSeconds, meta.Team0Score, meta.Team1Score)
			}
		}

		successCount++
	}

	t.Logf("Successfully parsed %d/%d real replay files", successCount, len(files))
	if successCount != len(files) {
		t.Fatalf("Expected all %d replays to parse, but only %d succeeded", len(files), successCount)
	}
}

func TestNormalizeReplayDate(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "psyonix standard replay header format with hyphens",
			input:    "2026-10-16 08-10-36",
			expected: "2026-10-16T08:10:36Z",
		},
		{
			name:     "psyonix older match history format with colon",
			input:    "2026-10-16:08-10",
			expected: "2026-10-16T08:10:00Z",
		},
		{
			name:     "standard space-separated datetime",
			input:    "2026-10-16 08:10:36",
			expected: "2026-10-16T08:10:36Z",
		},
		{
			name:     "iso-8601 without offset",
			input:    "2026-10-16T08:10:36",
			expected: "2026-10-16T08:10:36Z",
		},
		{
			name:     "canonical rfc3339 utc",
			input:    "2026-10-16T08:10:36Z",
			expected: "2026-10-16T08:10:36Z",
		},
		{
			name:     "canonical rfc3339 with timezone offset",
			input:    "2026-10-16T10:10:36+02:00",
			expected: "2026-10-16T08:10:36Z",
		},
		{
			name:     "rfc3339 nano",
			input:    "2026-10-16T08:10:36.123456789Z",
			expected: "2026-10-16T08:10:36Z",
		},
		{
			name:     "date only fallback",
			input:    "2026-10-16",
			expected: "2026-10-16T00:00:00Z",
		},
		{
			name:     "empty string returns empty",
			input:    "   ",
			expected: "",
		},
		{
			name:     "unrecognized string retained as fallback",
			input:    "unrecognized-date-string",
			expected: "unrecognized-date-string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := normalizeReplayDate(tc.input)
			if actual != tc.expected {
				t.Errorf("normalizeReplayDate(%q) = %q, expected %q", tc.input, actual, tc.expected)
			}
		})
	}
}

func TestExtractOvertime(t *testing.T) {
	t.Run("explicit bOvertime true", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"bOvertime": {boolVal: true},
		}
		if !extractOvertime(meta, props) {
			t.Errorf("expected extractOvertime to return true for bOvertime=true")
		}
	})

	t.Run("explicit bOverTime true", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"bOverTime": {boolVal: true},
		}
		if !extractOvertime(meta, props) {
			t.Errorf("expected extractOvertime to return true for bOverTime=true")
		}
	})

	t.Run("explicit Overtime true", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"Overtime": {boolVal: true},
		}
		if !extractOvertime(meta, props) {
			t.Errorf("expected extractOvertime to return true for Overtime=true")
		}
	})

	t.Run("explicit overtime true", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"overtime": {boolVal: true},
		}
		if !extractOvertime(meta, props) {
			t.Errorf("expected extractOvertime to return true for overtime=true")
		}
	})

	t.Run("explicit OvertimeSeconds positive", func(t *testing.T) {
		meta := &ReplayMetadata{}
		props := map[string]*propertyValue{
			"OvertimeSeconds": {intVal: 45},
		}
		if !extractOvertime(meta, props) {
			t.Errorf("expected extractOvertime to return true for OvertimeSeconds=45")
		}
	})

	t.Run("heuristic overtime true >=315s diff 1 no forfeit", func(t *testing.T) {
		meta := &ReplayMetadata{
			TotalSeconds: 319.36,
			Forfeit:      false,
			Team0Score:   3,
			Team1Score:   4,
		}
		props := map[string]*propertyValue{}
		if !extractOvertime(meta, props) {
			t.Errorf("expected heuristic overtime true for 319.36s, 3-4, no forfeit")
		}
	})

	t.Run("heuristic overtime false regulation zero second play 305s diff 1", func(t *testing.T) {
		meta := &ReplayMetadata{
			TotalSeconds: 305.23,
			Forfeit:      false,
			Team0Score:   4,
			Team1Score:   3,
		}
		props := map[string]*propertyValue{}
		if extractOvertime(meta, props) {
			t.Errorf("expected heuristic overtime false for 305.23s (<315s)")
		}
	})

	t.Run("heuristic overtime false forfeit despite >315s", func(t *testing.T) {
		meta := &ReplayMetadata{
			TotalSeconds: 320.0,
			Forfeit:      true,
			Team0Score:   3,
			Team1Score:   2,
		}
		props := map[string]*propertyValue{}
		if extractOvertime(meta, props) {
			t.Errorf("expected heuristic overtime false when Forfeit=true")
		}
	})

	t.Run("heuristic overtime false diff > 1 despite >315s", func(t *testing.T) {
		meta := &ReplayMetadata{
			TotalSeconds: 330.0,
			Forfeit:      false,
			Team0Score:   5,
			Team1Score:   3,
		}
		props := map[string]*propertyValue{}
		if extractOvertime(meta, props) {
			t.Errorf("expected heuristic overtime false when score diff > 1")
		}
	})
}

func TestExtractDemolitionsAndMatchStartEpoch(t *testing.T) {
	meta := &ReplayMetadata{}
	props := map[string]*propertyValue{
		"MatchStartEpoch": {int64Val: 1729066236}, // 2024-10-16 08:10:36 UTC
		"TotalSecondsPlayed": {floatVal: 300.0},
		"PlayerStats": {
			subProps: []map[string]*propertyValue{
				{
					"Name":  {strVal: "Player1"},
					"Score": {intVal: 500},
					"Demos": {intVal: 3},
				},
				{
					"Name":        {strVal: "Player2"},
					"Score":       {intVal: 400},
					"Demolitions": {intVal: 5},
				},
				{
					"Name":       {strVal: "Player3"},
					"Score":      {intVal: 300},
					"Demolition": {intVal: 2},
				},
				{
					"Name":  {strVal: "Player4"},
					"Score": {intVal: 200},
					"demos": {intVal: 1},
				},
				{
					"Name":  {strVal: "Player5"},
					"Score": {intVal: 100},
				},
			},
		},
	}

	populateMetadata(meta, props)

	// Verify MatchStartEpoch fallback
	if meta.Date != "2024-10-16T08:10:36Z" {
		t.Errorf("expected Date fallback from MatchStartEpoch to be '2024-10-16T08:10:36Z', got %q", meta.Date)
	}

	// Verify player demolitions
	if len(meta.Players) != 5 {
		t.Fatalf("expected 5 players, got %d", len(meta.Players))
	}
	expectedDemos := []int{3, 5, 2, 1, 0}
	for i, exp := range expectedDemos {
		if meta.Players[i].Demos != exp {
			t.Errorf("player %d (%s) Demos = %d, expected %d",
				i, meta.Players[i].Name, meta.Players[i].Demos, exp)
		}
	}
}

func TestParseEmptyReader(t *testing.T) {
	r := bytes.NewReader([]byte{})
	_, err := ParseReplay(r)
	if err == nil {
		t.Errorf("expected error for empty reader, got nil")
	}
}

func TestParseCorruptedAndAdversarialReplay(t *testing.T) {
	// 1. Dummy mock replay with "TAGAME\x00\x00\x00\x00" magic
	dummy := []byte("TAGAME\x00\x00\x00\x00stress-match-001")
	_, err := ParseReplay(bytes.NewReader(dummy))
	if err == nil {
		t.Fatalf("expected error for dummy replay bytes, got nil")
	}

	// 2. Huge headerLen (e.g. 1GB)
	hugeHeader := []byte{0x00, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00, 0x00}
	_, err = ParseReplay(bytes.NewReader(hugeHeader))
	if err == nil {
		t.Fatalf("expected error for huge headerLen, got nil")
	}

	// 3. Truncated header
	truncated := []byte{0x10, 0x00, 0x00, 0x00}
	_, err = ParseReplay(bytes.NewReader(truncated))
	if err == nil {
		t.Fatalf("expected error for truncated header, got nil")
	}
}
