package domain_test

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
)

func TestRankAndPlaylistFormatting(t *testing.T) {
	if got := domain.FormatRank(19, 1); got != "Grand Champion I Division II" {
		t.Fatalf("FormatRank(19,1) = %q", got)
	}
	if got := domain.FormatRank(22, 3); got != "Supersonic Legend" {
		t.Fatalf("FormatRank(22,3) = %q", got)
	}
	if got := domain.FormatRank(0, 2); got != "Unranked" {
		t.Fatalf("FormatRank(0,2) = %q", got)
	}
	if got := domain.FormatRank(99, 0); got != "Unknown" {
		t.Fatalf("FormatRank(99,0) = %q", got)
	}
	if got := domain.FormatShortRank(7, 0); got != "GLD I · DIV I" {
		t.Fatalf("FormatShortRank(7,0) = %q", got)
	}
	if got := domain.FormatStorageShortRank(7, 1); got != "GOLD I · DIV I" {
		t.Fatalf("FormatStorageShortRank(7,1) = %q", got)
	}
	if got := domain.FormatStorageRank(16, 4); got != "Champion I Div IV" {
		t.Fatalf("FormatStorageRank(16,4) = %q", got)
	}
	if got := domain.MapPlaylistSlugToID("ranked-doubles"); got != 11 {
		t.Fatalf("MapPlaylistSlugToID(ranked-doubles) = %d", got)
	}
	if !domain.IsRankedPlaylist(11) || domain.IsExtraMode(11) || !domain.IsExtraMode(27) {
		t.Fatal("playlist predicates returned unexpected values")
	}
}

func TestIdentityAndRoleStatsMath(t *testing.T) {
	parsed, err := domain.ParsePrimaryID("Steam|76561198000000001|0")
	if err != nil || parsed.IsBot || parsed.Platform != "Steam" {
		t.Fatalf("unexpected ParsePrimaryID: %+v, err=%v", parsed, err)
	}
	if !domain.IsBotID("Unknown|0|0") {
		t.Fatal("expected Unknown|0|0 to be classified as bot")
	}

	summary := domain.BuildRoleStatsSummary([]domain.MatchPlayerRow{
		{MatchGUID: "m1", Role: "teammate", Outcome: "victory", Score: 500, Goals: 2, Shots: 4},
		{MatchGUID: "m1", Role: "teammate", Outcome: "victory", Score: 500, Goals: 2, Shots: 4}, // duplicate ignored
		{MatchGUID: "m2", Role: "opponent", Outcome: "defeat", Score: 300, Goals: 1, Shots: 2},
	})
	if summary.Teammate.Games != 1 || summary.Teammate.Wins != 1 || summary.Teammate.ShotPct != 50.0 {
		t.Fatalf("unexpected teammate summary: %+v", summary.Teammate)
	}
	if summary.Opponent.Games != 1 || summary.Opponent.Losses != 1 {
		t.Fatalf("unexpected opponent summary: %+v", summary.Opponent)
	}
	if cut := domain.DateRangeCutoff("7d", time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)); cut.Day() != 1 {
		t.Fatalf("unexpected 7d cutoff: %v", cut)
	}
}

func TestErrorsAndSanitization(t *testing.T) {
	if !domain.IsDisconnectError(context.Background(), syscall.EPIPE) {
		t.Fatal("expected EPIPE to be disconnect error")
	}
	if !domain.IsDisconnectError(context.Background(), errors.New("wsasend: An established connection was aborted by the software in your host machine")) {
		t.Fatal("expected WinSock 10053 string to be disconnect error")
	}
	if _, err := domain.SanitizePathSegment("../secret"); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if got, err := domain.SanitizePathSegment("Steam|123|0"); err != nil || got != "Steam|123|0" {
		t.Fatalf("unexpected sanitize result: %q, %v", got, err)
	}
}
