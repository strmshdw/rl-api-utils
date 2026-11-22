package syncer

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/testutil"
)

// buildTestUE3Replay constructs a valid Rocket League UE3 replay payload parsed by replay.ParseReplayFile.
func buildTestUE3Replay(matchGUID string, team0Score, team1Score, teamSize, localTeam int, localName, dateStr string) []byte {
	var body bytes.Buffer
	writeI32 := func(b *bytes.Buffer, v int32) { _ = binary.Write(b, binary.LittleEndian, v) }
	writeI64 := func(b *bytes.Buffer, v int64) { _ = binary.Write(b, binary.LittleEndian, v) }
	writeFStr := func(b *bytes.Buffer, s string) {
		if s == "" {
			writeI32(b, 0)
			return
		}
		writeI32(b, int32(len(s)+1))
		b.WriteString(s)
		b.WriteByte(0)
	}
	writeIntProp := func(b *bytes.Buffer, name string, val int) {
		writeFStr(b, name)
		writeFStr(b, "IntProperty")
		writeI64(b, 4)
		writeI32(b, int32(val))
	}
	writeStrProp := func(b *bytes.Buffer, name, val string) {
		writeFStr(b, name)
		writeFStr(b, "StrProperty")
		var vbuf bytes.Buffer
		writeFStr(&vbuf, val)
		writeI64(b, int64(vbuf.Len()))
		b.Write(vbuf.Bytes())
	}

	writeI32(&body, 868) // engVer
	writeI32(&body, 26)  // licVer
	writeI32(&body, 10)  // netVer
	writeFStr(&body, "TAGame.Replay_Soccar_TA")
	writeStrProp(&body, "Id", matchGUID)
	writeStrProp(&body, "MapName", "Stadium_P")
	writeIntProp(&body, "Team0Score", team0Score)
	writeIntProp(&body, "Team1Score", team1Score)
	writeIntProp(&body, "TeamSize", teamSize)
	writeIntProp(&body, "PrimaryPlayerTeam", localTeam)
	writeStrProp(&body, "PlayerName", localName)
	writeStrProp(&body, "Date", dateStr)

	// PlayerStats array with local player + opponent
	var arrBuf bytes.Buffer
	writeI32(&arrBuf, 2)
	writeStrProp(&arrBuf, "Name", localName)
	writeIntProp(&arrBuf, "Team", localTeam)
	writeIntProp(&arrBuf, "Score", 420)
	writeIntProp(&arrBuf, "Goals", 2)
	writeFStr(&arrBuf, "None")

	oppTeam := 1 - localTeam
	writeStrProp(&arrBuf, "Name", "OpponentOne")
	writeIntProp(&arrBuf, "Team", oppTeam)
	writeIntProp(&arrBuf, "Score", 210)
	writeIntProp(&arrBuf, "Goals", 1)
	writeFStr(&arrBuf, "None")

	writeFStr(&body, "PlayerStats")
	writeFStr(&body, "ArrayProperty")
	writeI64(&body, int64(arrBuf.Len()))
	body.Write(arrBuf.Bytes())

	writeFStr(&body, "None")

	var out bytes.Buffer
	writeI32(&out, int32(body.Len()))
	writeI32(&out, 0) // crc
	out.Write(body.Bytes())
	// Pad to >= 1024 bytes so replay passes min size thresholds.
	if out.Len() < 1500 {
		out.Write(make([]byte, 1500-out.Len()))
	}
	return out.Bytes()
}

type realFileDownloader struct {
	mu       sync.Mutex
	payloads map[string][]byte
}

func (d *realFileDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}
	d.mu.Lock()
	data := d.payloads[matchGUID]
	d.mu.Unlock()
	if len(data) == 0 {
		data = testutil.GenerateValidReplay(matchGUID, 2048)
	}
	p := filepath.Join(destDir, matchGUID+".replay")
	if err := os.WriteFile(p, data, 0644); err != nil {
		return "", err
	}
	return p, nil
}

func TestSyncer_ReplayNamingAndBallchasingGroups_FullLifecycle(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sync-org.db")
	replayDir := filepath.Join(tmpDir, "replays")

	bcSrv := testutil.NewMockBallchasingServer()
	defer bcSrv.Close()

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	day1Sess1Start := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	day1Sess2Start := time.Date(2026, 11, 21, 18, 0, 0, 0, time.UTC)
	day2Sess1Start := time.Date(2026, 11, 22, 11, 0, 0, 0, time.UTC)

	// Persist two sessions on 2026-11-21 and one session on 2026-11-22.
	s1End := day1Sess1Start.Add(45 * time.Minute)
	if err := store.SaveSession(ctx, &storage.SessionRecord{
		SessionID:    "sess-day1-a",
		StartedAt:    day1Sess1Start,
		LastMatchAt:  &s1End,
		TotalMatches: 4,
		MatchesJSON: `[
			{"match_guid":"m-d1-1","playlist_id":11,"result":"victory"},
			{"match_guid":"m-d1-2","playlist_id":11,"result":"defeat"},
			{"match_guid":"m-d1-3","playlist_id":13,"result":"victory"},
			{"match_guid":"m-d1-4","playlist_id":27,"result":"defeat"}
		]`,
	}); err != nil {
		t.Fatalf("SaveSession s1: %v", err)
	}

	s2End := day1Sess2Start.Add(30 * time.Minute)
	if err := store.SaveSession(ctx, &storage.SessionRecord{
		SessionID:    "sess-day1-b",
		StartedAt:    day1Sess2Start,
		LastMatchAt:  &s2End,
		TotalMatches: 2,
		MatchesJSON: `[
			{"match_guid":"m-d1-5","playlist_id":11,"result":"victory"},
			{"match_guid":"m-d1-6","playlist_id":11,"result":"defeat"}
		]`,
	}); err != nil {
		t.Fatalf("SaveSession s2: %v", err)
	}

	s3End := day2Sess1Start.Add(15 * time.Minute)
	if err := store.SaveSession(ctx, &storage.SessionRecord{
		SessionID:    "sess-day2-a",
		StartedAt:    day2Sess1Start,
		LastMatchAt:  &s3End,
		TotalMatches: 1,
		MatchesJSON:  `[{"match_guid":"m-d2-1","playlist_id":10,"result":"victory"}]`,
	}); err != nil {
		t.Fatalf("SaveSession s3: %v", err)
	}

	dl := &realFileDownloader{
		payloads: map[string][]byte{
			"m-d1-1": buildTestUE3Replay("m-d1-1", 3, 1, 2, 0, "Hero", "2026-11-21 10-05-00"), // 2v2 WIN
			"m-d1-2": buildTestUE3Replay("m-d1-2", 1, 4, 2, 0, "Hero", "2026-11-21 10-15-00"), // 2v2 LOSS
			"m-d1-3": buildTestUE3Replay("m-d1-3", 2, 4, 3, 1, "Hero", "2026-11-21 10-25-00"), // 3v3 WIN (orange)
			"m-d1-4": buildTestUE3Replay("m-d1-4", 5, 2, 2, 1, "Hero", "2026-11-21 10-35-00"), // Extra (Hoops) LOSS
			"m-d1-5": buildTestUE3Replay("m-d1-5", 3, 0, 2, 0, "Hero", "2026-11-21 18-05-00"), // Session 2 2v2 WIN
			"m-d1-6": buildTestUE3Replay("m-d1-6", 0, 2, 2, 0, "Hero", "2026-11-21 18-15-00"), // Session 2 2v2 LOSS (after restart)
			"m-d2-1": buildTestUE3Replay("m-d2-1", 4, 2, 1, 0, "Hero", "2026-11-22 11-05-00"), // Day 2 1v1 WIN
		},
	}

	bcClient, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:     bcSrv.URL(),
		APIKey:      "test-ballchasing-token",
		Visibility:  "private",
		MaxRetries:  1,
		BaseBackoff: 5 * time.Millisecond,
		RateLimiter: ballchasing.NewRateLimiter(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-d1-1", RecordStartTimestamp: day1Sess1Start.Add(5 * time.Minute).Unix(), Playlist: 11, ReplayURL: "https://cdn/1.replay"},
		DiscoveredMatch{MatchGUID: "m-d1-2", RecordStartTimestamp: day1Sess1Start.Add(15 * time.Minute).Unix(), Playlist: 11, ReplayURL: "https://cdn/2.replay"},
		DiscoveredMatch{MatchGUID: "m-d1-3", RecordStartTimestamp: day1Sess1Start.Add(25 * time.Minute).Unix(), Playlist: 13, ReplayURL: "https://cdn/3.replay"},
		DiscoveredMatch{MatchGUID: "m-d1-4", RecordStartTimestamp: day1Sess1Start.Add(35 * time.Minute).Unix(), Playlist: 27, ReplayURL: "https://cdn/4.replay"},
		DiscoveredMatch{MatchGUID: "m-d1-5", RecordStartTimestamp: day1Sess2Start.Add(5 * time.Minute).Unix(), Playlist: 11, ReplayURL: "https://cdn/5.replay"},
		DiscoveredMatch{MatchGUID: "m-d2-1", RecordStartTimestamp: day2Sess1Start.Add(5 * time.Minute).Unix(), Playlist: 10, ReplayURL: "https://cdn/7.replay"},
	)

	s, err := New(store, provider, dl, bcClient,
		WithReplayDir(replayDir),
		WithKeepLocalFiles(true),
		WithLocalPlayerName("Hero"),
		WithLocation(time.UTC),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("syncer.New: %v", err)
	}

	stats, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle 1: %v", err)
	}
	if stats.DownloadedCount != 6 || stats.UploadedCount != 6 {
		t.Fatalf("expected 6 downloaded & 6 uploaded, got %+v", stats)
	}

	// Verify exact filenames on disk AND in Ballchasing uploads & patches!
	wantFiles := map[string]string{
		"m-d1-1": "2026_11_21_001_2v2_WIN.replay",
		"m-d1-2": "2026_11_21_002_2v2_LOSS.replay",
		"m-d1-3": "2026_11_21_003_3v3_WIN.replay",
		"m-d1-4": "2026_11_21_004_Extra_LOSS.replay",
		"m-d1-5": "2026_11_21_005_2v2_WIN.replay",
		"m-d2-1": "2026_11_22_001_1v1_WIN.replay",
	}
	for guid, wantName := range wantFiles {
		rec, err := store.GetMatch(ctx, guid)
		if err != nil {
			t.Fatalf("GetMatch %s: %v", guid, err)
		}
		if rec.ReplayName != wantName {
			t.Fatalf("match %s ReplayName = %q, want %q", guid, rec.ReplayName, wantName)
		}
		if filepath.Base(rec.LocalFilePath) != wantName {
			t.Fatalf("match %s LocalFilePath = %q, want basename %q", guid, rec.LocalFilePath, wantName)
		}
		if _, err := os.Stat(rec.LocalFilePath); err != nil {
			t.Fatalf("renamed file %q missing on disk: %v", rec.LocalFilePath, err)
		}
		// Original raw GUID filename should no longer exist on disk.
		if _, err := os.Stat(filepath.Join(replayDir, guid+".replay")); !os.IsNotExist(err) {
			t.Fatalf("old raw GUID file %s.replay still exists on disk", guid)
		}
	}

	// Verify exact Ballchasing groups created (5 distinct session+mode groups, NO duplicate for 2nd 2v2 match in sess-day1-a!)
	groups := bcSrv.GetGroups()
	if len(groups) != 5 {
		t.Fatalf("expected exactly 5 created Ballchasing groups, got %d: %+v", len(groups), groups)
	}
	groupByName := make(map[string]string, len(groups))
	for _, g := range groups {
		groupByName[g.Name] = g.ID
	}
	for _, expectedGroup := range []string{
		"2026_11_21_1_2v2",
		"2026_11_21_1_3v3",
		"2026_11_21_1_Extra",
		"2026_11_21_2_2v2",
		"2026_11_22_1_1v1",
	} {
		if groupByName[expectedGroup] == "" {
			t.Fatalf("missing expected Ballchasing group %q; created=%v", expectedGroup, groupByName)
		}
	}

	// Verify every uploaded replay used the exact renamed filename and its exact group ID
	uploadsByFile := make(map[string]testutil.UploadedReplay)
	for _, u := range bcSrv.GetUploads() {
		uploadsByFile[u.FileName] = u
	}
	assertUpload := func(fileName, expectedGroupName string) {
		t.Helper()
		u, ok := uploadsByFile[fileName]
		if !ok {
			t.Fatalf("expected Ballchasing upload with filename %q; got %v", fileName, uploadsByFile)
		}
		if u.Group != groupByName[expectedGroupName] {
			t.Fatalf("upload %q group=%q, want %q (%s)", fileName, u.Group, groupByName[expectedGroupName], expectedGroupName)
		}
	}
	assertUpload("2026_11_21_001_2v2_WIN.replay", "2026_11_21_1_2v2")
	assertUpload("2026_11_21_002_2v2_LOSS.replay", "2026_11_21_1_2v2")
	assertUpload("2026_11_21_003_3v3_WIN.replay", "2026_11_21_1_3v3")
	assertUpload("2026_11_21_004_Extra_LOSS.replay", "2026_11_21_1_Extra")
	assertUpload("2026_11_21_005_2v2_WIN.replay", "2026_11_21_2_2v2")
	assertUpload("2026_11_22_001_1v1_WIN.replay", "2026_11_22_1_1v1")

	// Simulate full process restart: close store, reopen SQLiteStore + new Syncer, sync m-d1-6 in sess-day1-b (2v2)
	_ = store.Close()

	store2, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen SQLiteStore: %v", err)
	}
	defer store2.Close()

	provider2 := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-d1-6", RecordStartTimestamp: day1Sess2Start.Add(15 * time.Minute).Unix(), Playlist: 11, ReplayURL: "https://cdn/6.replay"},
	)
	s2, err := New(store2, provider2, dl, bcClient,
		WithReplayDir(replayDir),
		WithKeepLocalFiles(true),
		WithLocalPlayerName("Hero"),
		WithLocation(time.UTC),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("syncer.New after restart: %v", err)
	}
	stats2, err := s2.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle after restart: %v", err)
	}
	if stats2.DownloadedCount != 1 || stats2.UploadedCount != 1 {
		t.Fatalf("expected 1 downloaded & 1 uploaded after restart, got %+v", stats2)
	}

	// Must allocate #006 (NOT reset to #001!) and MUST reuse existing group 2026_11_21_2_2v2 (still 5 total groups!)
	rec6, err := store2.GetMatch(ctx, "m-d1-6")
	if err != nil {
		t.Fatalf("GetMatch m-d1-6: %v", err)
	}
	if rec6.ReplayName != "2026_11_21_006_2v2_LOSS.replay" {
		t.Fatalf("after restart expected 2026_11_21_006_2v2_LOSS.replay, got %q", rec6.ReplayName)
	}
	if bcSrv.GetGroupCount() != 5 {
		t.Fatalf("expected zero new groups created after restart (reused 2026_11_21_2_2v2), got %d groups", bcSrv.GetGroupCount())
	}
	if rec6.BallchasingGroupID != groupByName["2026_11_21_2_2v2"] {
		t.Fatalf("m-d1-6 group=%q, want persisted %q", rec6.BallchasingGroupID, groupByName["2026_11_21_2_2v2"])
	}
}

func TestSyncer_ConcurrentDownloads_UniqueDailyAutonumbersAndRetryIdempotency(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tmpDir, "concurrent.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	bcSrv := testutil.NewMockBallchasingServer()
	defer bcSrv.Close()

	baseTS := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	dl := &realFileDownloader{payloads: make(map[string][]byte)}
	var matches []DiscoveredMatch
	for i := 1; i <= 4; i++ {
		guid := fmt.Sprintf("conc-m-%d", i)
		dl.payloads[guid] = buildTestUE3Replay(guid, 3, 1, 2, 0, "Hero", "2026-11-21 14-00-00")
		matches = append(matches, DiscoveredMatch{
			MatchGUID:            guid,
			RecordStartTimestamp: baseTS.Add(time.Duration(i) * time.Minute).Unix(),
			Playlist:             11,
			ReplayURL:            fmt.Sprintf("https://cdn/%s.replay", guid),
		})
	}

	bcClient, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:     bcSrv.URL(),
		APIKey:      "test-ballchasing-token",
		MaxRetries:  0,
		RateLimiter: ballchasing.NewRateLimiter(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Force upload failure on Cycle 1 so all 4 download & rename concurrently, then fail upload.
	bcSrv.SetForcedStatus(500, "temporary outage")

	s, err := New(store, newMockHistoryProvider(matches...), dl, bcClient,
		WithReplayDir(filepath.Join(tmpDir, "replays")),
		WithMaxConcurrentDownloads(4),
		WithLocalPlayerName("Hero"),
		WithLocation(time.UTC),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stats1, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle 1: %v", err)
	}
	if stats1.DownloadedCount != 4 || stats1.FailedCount != 4 {
		t.Fatalf("expected 4 downloaded & 4 upload failures, got %+v", stats1)
	}

	// Check 4 strictly unique sequential autonumbers (1..4) with zero collisions.
	var namesBefore []string
	for i := 1; i <= 4; i++ {
		rec, err := store.GetMatch(ctx, fmt.Sprintf("conc-m-%d", i))
		if err != nil {
			t.Fatalf("GetMatch: %v", err)
		}
		namesBefore = append(namesBefore, rec.ReplayName)
	}
	sort.Strings(namesBefore)
	wantSorted := []string{
		"2026_11_21_001_2v2_WIN.replay",
		"2026_11_21_002_2v2_WIN.replay",
		"2026_11_21_003_2v2_WIN.replay",
		"2026_11_21_004_2v2_WIN.replay",
	}
	for i := range wantSorted {
		if namesBefore[i] != wantSorted[i] {
			t.Fatalf("concurrent names = %v, want %v", namesBefore, wantSorted)
		}
	}

	// Recover Ballchasing, retry all 4 matches; autonumbers MUST NOT advance past 4, and only 1 daily fallback group created!
	bcSrv.SetForcedStatus(0, "")
	for i := 1; i <= 4; i++ {
		if err := s.RetryMatch(ctx, fmt.Sprintf("conc-m-%d", i)); err != nil {
			t.Fatalf("RetryMatch %d: %v", i, err)
		}
	}

	if counter, err := store.GetDailyCounter(ctx, storage.CounterTypeGame, "2026_11_21"); err != nil || counter != 4 {
		t.Fatalf("expected daily game counter still 4 after retries, got %d (err=%v)", counter, err)
	}
	if bcSrv.GetGroupCount() != 1 {
		t.Fatalf("expected exactly 1 fallback daily group for all 4 matches, got %d", bcSrv.GetGroupCount())
	}
	if gotGroupName := bcSrv.GetGroups()[0].Name; gotGroupName != "2026_11_21_1_2v2" {
		t.Fatalf("expected fallback daily group name 2026_11_21_1_2v2, got %q", gotGroupName)
	}
}

func TestSyncer_Duplicate409_PatchesReplayIntoGroup(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tmpDir, "dup.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	bcSrv := testutil.NewMockBallchasingServer()
	defer bcSrv.Close()
	bcSrv.SetDuplicateGUID("dup-match-1", "existing-bc-id-77")

	dl := &realFileDownloader{
		payloads: map[string][]byte{
			"dup-match-1": buildTestUE3Replay("dup-match-1", 2, 1, 2, 0, "Hero", "2026-11-21 16-00-00"),
		},
	}

	bcClient, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:     bcSrv.URL(),
		APIKey:      "test-ballchasing-token",
		MaxRetries:  1,
		RateLimiter: ballchasing.NewRateLimiter(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ts := time.Date(2026, 11, 21, 16, 0, 0, 0, time.UTC).Unix()
	s, err := New(store, newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "dup-match-1", RecordStartTimestamp: ts, Playlist: 11, ReplayURL: "https://cdn/dup.replay"},
	), dl, bcClient,
		WithReplayDir(filepath.Join(tmpDir, "replays")),
		WithLocalPlayerName("Hero"),
		WithLocation(time.UTC),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stats, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if stats.DuplicateCount != 1 {
		t.Fatalf("expected DuplicateCount=1, got %+v", stats)
	}
	if bcSrv.GetGroupCount() != 1 {
		t.Fatalf("expected 1 created group, got %d", bcSrv.GetGroupCount())
	}
	grpID := bcSrv.GetGroups()[0].ID
	patch := bcSrv.GetReplayPatch("existing-bc-id-77")
	if patch == nil || patch.Group != grpID || patch.Title != "2026_11_21_001_2v2_WIN" {
		t.Fatalf("expected 409 duplicate replay patched with group=%q title=2026_11_21_001_2v2_WIN, got %+v", grpID, patch)
	}
}

func TestSyncer_PostStatsSync_PatchesTitleWhenDifferent(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tmpDir, "title.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	bcSrv := testutil.NewMockBallchasingServer()
	defer bcSrv.Close()

	bcClient, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:     bcSrv.URL(),
		APIKey:      "test-ballchasing-token",
		MaxRetries:  1,
		RateLimiter: ballchasing.NewRateLimiter(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ts := time.Date(2026, 11, 21, 17, 0, 0, 0, time.UTC).Unix()
	if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
		{MatchGUID: "m-title-1", RecordStartTimestamp: ts, Playlist: 11, ReplayURL: "https://cdn/t.replay"},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := store.AllocateMatchReplayNaming(ctx, "m-title-1", "2026_11_21", "2v2", "WIN", 0); err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if err := store.MarkUploaded(ctx, "m-title-1", "bc-title-99", "https://ballchasing.com/replay/bc-title-99"); err != nil {
		t.Fatalf("MarkUploaded: %v", err)
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-title-99"] = &ballchasing.ReplayItem{
		ID:         "bc-title-99",
		Status:     "ok",
		Title:      "2026-11-21 17:00 Ranked Doubles",
		PlaylistID: "ranked-doubles",
		Blue:       ballchasing.ReplayTeam{Goals: 3},
		Orange:     ballchasing.ReplayTeam{Goals: 1},
	}

	s, err := New(store, newMockHistoryProvider(), newMockDownloader(), bcClient,
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.PollAndSyncPendingStats(ctx); err != nil {
		t.Fatalf("PollAndSyncPendingStats: %v", err)
	}

	patch := bcSrv.GetReplayPatch("bc-title-99")
	if patch == nil || patch.Title != "2026_11_21_001_2v2_WIN" {
		t.Fatalf("expected title patch to 2026_11_21_001_2v2_WIN, got %+v", patch)
	}
}
