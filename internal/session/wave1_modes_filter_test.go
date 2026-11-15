package session

import (
	"testing"
	"time"
)

func TestFilterSessionSummary_CasualPrivateRankedAndCSV(t *testing.T) {
	baseTime := time.Date(2026, 4, 10, 18, 0, 0, 0, time.UTC)
	localTeam := 0
	winTeam0 := 0
	winTeam1 := 1

	summary := &SessionResponse{
		SessionID:      "sess-modes",
		StartedAt:      baseTime,
		TotalMatches:   6,
		TotalWins:      4,
		TotalLosses:    2,
		WinRate:        66.7,
		LocalPlayerIDs: []string{"Steam|local|0"},
		Playlists: map[string]*PlaylistSessionStats{
			"11": {
				PlaylistID:    11,
				PlaylistName:  "Ranked Doubles",
				MatchesPlayed: 1,
				Wins:          1,
				Losses:        0,
				WinRate:       100.0,
				InitialMMR:    1400,
				CurrentMMR:    1409,
				MMRDelta:      9,
			},
			"27": {
				PlaylistID:    27,
				PlaylistName:  "Ranked Hoops",
				MatchesPlayed: 1,
				Wins:          1,
				Losses:        0,
				WinRate:       100.0,
				InitialMMR:    1174,
				CurrentMMR:    1183,
				MMRDelta:      9,
			},
		},
		Matches: []*SessionMatchDetail{
			{
				MatchGUID: "m-cas-2", PlaylistID: 2, PlaylistName: "Casual Doubles",
				EndedAt: baseTime.Add(5 * time.Minute), Result: "victory",
				LocalTeam: &localTeam, WinnerTeam: &winTeam0, BlueScore: 3, OrangeScore: 1,
			},
			{
				MatchGUID: "m-cas-15", PlaylistID: 15, PlaylistName: "Casual Snow Day",
				EndedAt: baseTime.Add(10 * time.Minute), Result: "defeat",
				LocalTeam: &localTeam, WinnerTeam: &winTeam1, BlueScore: 1, OrangeScore: 2,
			},
			{
				MatchGUID: "m-priv-6", PlaylistID: 6, PlaylistName: "Private Match",
				EndedAt: baseTime.Add(15 * time.Minute), Result: "victory",
				LocalTeam: &localTeam, WinnerTeam: &winTeam0, BlueScore: 4, OrangeScore: 0,
			},
			{
				MatchGUID: "m-rk-11", PlaylistID: 11, PlaylistName: "Ranked Doubles",
				EndedAt: baseTime.Add(20 * time.Minute), Result: "victory",
				LocalTeam: &localTeam, WinnerTeam: &winTeam0, BlueScore: 2, OrangeScore: 1,
				StartingMMR: 1400, EndingMMR: 1409, MMRChange: 9,
			},
			{
				MatchGUID: "m-hoops-27", PlaylistID: 27, PlaylistName: "Ranked Hoops",
				EndedAt: baseTime.Add(25 * time.Minute), Result: "victory",
				LocalTeam: &localTeam, WinnerTeam: &winTeam0, BlueScore: 5, OrangeScore: 3,
			},
			{
				MatchGUID: "m-rumble-28", PlaylistID: 28, PlaylistName: "Ranked Rumble",
				EndedAt: baseTime.Add(30 * time.Minute), Result: "defeat",
				LocalTeam: &localTeam, WinnerTeam: &winTeam1, BlueScore: 2, OrangeScore: 4,
			},
		},
	}

	// 1. Casual filter -> matches Playlist 2 and 15.
	cas := FilterSessionSummary(summary, "casual", 0, 0, nil)
	if cas.TotalMatches != 2 || cas.TotalWins != 1 || cas.TotalLosses != 1 {
		t.Fatalf("casual summary = (%d, %d-%d), want (2, 1-1)", cas.TotalMatches, cas.TotalWins, cas.TotalLosses)
	}

	// 2. Private filter -> matches Playlist 6.
	priv := FilterSessionSummary(summary, "private", 0, 0, nil)
	if priv.TotalMatches != 1 || priv.TotalWins != 1 || len(priv.Matches) != 1 || priv.Matches[0].PlaylistID != 6 {
		t.Fatalf("private summary = %+v, want 1 match in playlist 6", priv)
	}

	// 3. Ranked 2v2 filter -> matches Playlist 11 only (not Casual 2v2 Playlist 2!), preserving InitialMMR/CurrentMMR.
	rk2 := FilterSessionSummary(summary, "ranked_2v2", 0, 0, nil)
	if rk2.TotalMatches != 1 || rk2.Matches[0].PlaylistID != 11 {
		t.Fatalf("ranked_2v2 summary = %+v, want 1 match in playlist 11", rk2)
	}
	ps11 := rk2.Playlists["11"]
	if ps11 == nil || ps11.InitialMMR != 1400 || ps11.CurrentMMR != 1409 || ps11.MMRDelta != 9 {
		t.Fatalf("ranked_2v2 playlist 11 stats = %+v, want InitialMMR=1400 CurrentMMR=1409 MMRDelta=9", ps11)
	}

	// 4. Extra mode filter ("27" Hoops) -> preserves InitialMMR/CurrentMMR/MMRDelta from unfiltered Playlists["27"] even when match StartingMMR/EndingMMR are 0.
	hoops := FilterSessionSummary(summary, "27", 0, 0, nil)
	ps27 := hoops.Playlists["27"]
	if ps27 == nil || ps27.InitialMMR != 1174 || ps27.CurrentMMR != 1183 || ps27.MMRDelta != 9 {
		t.Fatalf("hoops playlist 27 stats = %+v, want InitialMMR=1174 CurrentMMR=1183 MMRDelta=9", ps27)
	}

	// 5. Date-bounded filter computes InitialMMR from earliest matching game's StartingMMR.
	rk2Window := FilterSessionSummary(summary, "11", baseTime.Add(18*time.Minute).Unix(), baseTime.Add(22*time.Minute).Unix(), nil)
	if ps11Win := rk2Window.Playlists["11"]; ps11Win == nil || ps11Win.InitialMMR != 1400 || ps11Win.CurrentMMR != 1409 {
		t.Fatalf("windowed ranked_2v2 playlist 11 stats = %+v, want InitialMMR=1400 CurrentMMR=1409", ps11Win)
	}

	// 6. Comma-separated playlist IDs -> "27,28" matches Hoops + Rumble.
	csv := FilterSessionSummary(summary, "27,28", 0, 0, nil)
	if csv.TotalMatches != 2 || csv.TotalWins != 1 || csv.TotalLosses != 1 {
		t.Fatalf("csv 27,28 summary = (%d, %d-%d), want (2, 1-1)", csv.TotalMatches, csv.TotalWins, csv.TotalLosses)
	}
}
