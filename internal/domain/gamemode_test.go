package domain

import "testing"

func TestGameModeCategory(t *testing.T) {
	cases := []struct {
		name       string
		playlistID int
		teamSize   int
		want       string
	}{
		{"casual 1v1", 1, 1, GameMode1v1},
		{"ranked 1v1", 10, 1, GameMode1v1},
		{"casual 2v2", 2, 2, GameMode2v2},
		{"ranked 2v2", 11, 2, GameMode2v2},
		{"casual 3v3", 3, 3, GameMode3v3},
		{"solo standard 3v3", 12, 3, GameMode3v3},
		{"ranked 3v3", 13, 3, GameMode3v3},
		{"hoops", 27, 2, GameModeExtra},
		{"rumble", 28, 3, GameModeExtra},
		{"dropshot", 29, 3, GameModeExtra},
		{"snow day", 30, 3, GameModeExtra},
		{"chaos 4v4", 4, 4, GameModeExtra},
		{"tournament", 34, 3, GameModeExtra},
		{"casual hoops", 15, 2, GameModeExtra},
		{"heatseeker", 38, 3, GameModeExtra},
		{"unknown playlist teamSize 1", 0, 1, GameMode1v1},
		{"unknown playlist teamSize 2", 0, 2, GameMode2v2},
		{"unknown playlist teamSize 3", 0, 3, GameMode3v3},
		{"unknown playlist teamSize 4", 0, 4, GameModeExtra},
		{"unknown playlist teamSize 0", 0, 0, GameModeExtra},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GameModeCategory(tc.playlistID, tc.teamSize); got != tc.want {
				t.Fatalf("GameModeCategory(%d, %d) = %q, want %q", tc.playlistID, tc.teamSize, got, tc.want)
			}
		})
	}
}

func TestFormatReplayFileNameAndGroupName(t *testing.T) {
	if got := FormatReplayFileName("2026_11_21", 1, "2v2", "victory"); got != "2026_11_21_001_2v2_WIN.replay" {
		t.Fatalf("got %q, want 2026_11_21_001_2v2_WIN.replay", got)
	}
	if got := FormatReplayFileName("2026_11_21", 42, "Extra", "defeat"); got != "2026_11_21_042_Extra_LOSS.replay" {
		t.Fatalf("got %q, want 2026_11_21_042_Extra_LOSS.replay", got)
	}
	if got := FormatReplayFileName("2026_11_21", 1005, "3v3", "WIN"); got != "2026_11_21_1005_3v3_WIN.replay" {
		t.Fatalf("got %q, want 2026_11_21_1005_3v3_WIN.replay", got)
	}
	if got := FormatBallchasingGroupName("2026_11_21", 1, "2v2"); got != "2026_11_21_1_2v2" {
		t.Fatalf("got %q, want 2026_11_21_1_2v2", got)
	}
	if got := FormatBallchasingGroupName("2026_11_21", 3, "extra"); got != "2026_11_21_3_Extra" {
		t.Fatalf("got %q, want 2026_11_21_3_Extra", got)
	}
}
