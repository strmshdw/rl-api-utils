package e2e

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// ============================================================================
// Feature 6: Match History Polling
// ============================================================================

func TestTier1_F6_Polling_MatchesRetrieved(t *testing.T) {
	h := SetupE2EHarness(t)
	m1 := testutil.NewMockMatchEntry("m1", h.CDN.ReplayURL("m1"), "Wasteland_P", 2)
	h.PsyNet.AddMatch(m1)

	adapter := NewMockProviderAdapter(h.PsyNet)
	matches, err := adapter.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("failed to get matches: %v", err)
	}
	if len(matches) != 1 || matches[0].MatchGUID != "m1" {
		t.Fatalf("expected m1, got %v", matches)
	}
}

func TestTier1_F6_Polling_MetadataExtraction(t *testing.T) {
	h := SetupE2EHarness(t)
	m1 := testutil.NewMockMatchEntry("m1-meta", h.CDN.ReplayURL("m1-meta"), "Beckwith_P", 3)
	h.PsyNet.AddMatch(m1)

	adapter := NewMockProviderAdapter(h.PsyNet)
	matches, _ := adapter.GetRecentMatches(context.Background())
	if matches[0].MapName != "Beckwith_P" || matches[0].Playlist != 3 {
		t.Fatalf("unexpected metadata: %+v", matches[0])
	}
}

func TestTier1_F6_Polling_EmptyHistoryHandling(t *testing.T) {
	h := SetupE2EHarness(t)
	adapter := NewMockProviderAdapter(h.PsyNet)
	matches, err := adapter.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("polling failed on empty history: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestTier1_F6_Polling_FilterValidVsEmptyReplayUrl(t *testing.T) {
	h := SetupE2EHarness(t)
	m1 := testutil.NewMockMatchEntry("m1", h.CDN.ReplayURL("m1"), "Map1", 2)
	m2 := testutil.NewMockMatchEntry("m2", "", "Map2", 2) // No replay
	h.PsyNet.SetMatches([]testutil.MockMatchEntry{m1, m2})

	adapter := NewMockProviderAdapter(h.PsyNet)
	matches, _ := adapter.GetRecentMatches(context.Background())
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestTier1_F6_Polling_DynamicMatchesUpdate(t *testing.T) {
	h := SetupE2EHarness(t)
	adapter := NewMockProviderAdapter(h.PsyNet)

	matches1, _ := adapter.GetRecentMatches(context.Background())
	if len(matches1) != 0 {
		t.Fatalf("expected 0 matches")
	}

	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m-dyn", h.CDN.ReplayURL("m-dyn"), "Map", 2))
	matches2, _ := adapter.GetRecentMatches(context.Background())
	if len(matches2) != 1 {
		t.Fatalf("expected 1 match after dynamic addition, got %d", len(matches2))
	}
}

// ============================================================================
// Feature 7: Atomic Replay Downloader
// ============================================================================

func TestTier1_F7_Downloader_SuccessWithTAGAME(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	path, err := downloader.DownloadReplay(context.Background(), "guid-dl-1", h.CDN.ReplayURL("guid-dl-1"), h.ReplayDir)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
		t.Fatal("downloaded replay does not have TAGAME magic bytes")
	}
}

func TestTier1_F7_Downloader_AtomicTmpRename(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	path, err := downloader.DownloadReplay(context.Background(), "guid-dl-2", h.CDN.ReplayURL("guid-dl-2"), h.ReplayDir)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	expectedPath := filepath.Join(h.ReplayDir, "guid-dl-2.replay")
	if path != expectedPath {
		t.Fatalf("expected %s, got %s", expectedPath, path)
	}

	// Verify no temporary files remain
	files, _ := os.ReadDir(h.ReplayDir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".tmp") {
			t.Fatalf("orphaned tmp file found: %s", f.Name())
		}
	}
}

func TestTier1_F7_Downloader_RejectUnder1KB(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	// Set tiny 100 byte payload
	h.CDN.SetReplayPayload("guid-tiny", []byte("tiny-replay-data"))

	_, err := downloader.DownloadReplay(context.Background(), "guid-tiny", h.CDN.ReplayURL("guid-tiny"), h.ReplayDir)
	if err == nil {
		t.Fatal("expected error downloading < 1KB file")
	}
}

func TestTier1_F7_Downloader_CleanupTmpOnFailure(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	h.CDN.SetStatusCode("guid-err", http.StatusForbidden)
	_, err := downloader.DownloadReplay(context.Background(), "guid-err", h.CDN.ReplayURL("guid-err"), h.ReplayDir)
	if err == nil {
		t.Fatal("expected download failure on 403")
	}

	files, _ := os.ReadDir(h.ReplayDir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".tmp") {
			t.Fatalf("tmp file not cleaned up after error: %s", f.Name())
		}
	}
}

func TestTier1_F7_Downloader_DestinationDirCreation(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	nestedDir := filepath.Join(h.ReplayDir, "nested", "replays", "dir")
	path, err := downloader.DownloadReplay(context.Background(), "guid-nested", h.CDN.ReplayURL("guid-nested"), nestedDir)
	if err != nil {
		t.Fatalf("download with auto dir creation failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not found in auto-created dir: %v", err)
	}
}

// ============================================================================
// Feature 8: Ballchasing Multipart Upload
// ============================================================================

func TestTier1_F8_Upload_MultipartFormDataStructure(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)

	filePath := filepath.Join(h.ReplayDir, "test.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("guid-up-1", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "guid-up-1", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if res.ID == "" {
		t.Fatal("expected non-empty replay ID")
	}
}

func TestTier1_F8_Upload_VisibilityPublic(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)

	filePath := filepath.Join(h.ReplayDir, "vis-pub.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("vis-pub", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "vis-pub", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	record := h.BC.GetUpload("vis-pub")
	if record == nil || record.Visibility != "public" {
		t.Fatalf("expected visibility=public, got %v", record)
	}
}

func TestTier1_F8_Upload_VisibilityUnlisted(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "unlisted", "", 3)

	filePath := filepath.Join(h.ReplayDir, "vis-unlisted.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("vis-unlisted", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "vis-unlisted", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	record := h.BC.GetUpload("vis-unlisted")
	if record == nil || record.Visibility != "unlisted" {
		t.Fatalf("expected visibility=unlisted, got %v", record)
	}
}

func TestTier1_F8_Upload_VisibilityPrivate(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "private", "", 3)

	filePath := filepath.Join(h.ReplayDir, "vis-priv.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("vis-priv", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "vis-priv", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	record := h.BC.GetUpload("vis-priv")
	if record == nil || record.Visibility != "private" {
		t.Fatalf("expected visibility=private, got %v", record)
	}
}

func TestTier1_F8_Upload_WithGroupID(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "tournament-group-1", 3)

	filePath := filepath.Join(h.ReplayDir, "grp.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("grp", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "grp", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	record := h.BC.GetUpload("grp")
	if record == nil || record.Group != "tournament-group-1" {
		t.Fatalf("expected group=tournament-group-1, got %v", record)
	}
}

// ============================================================================
// Feature 9: Ballchasing Raw Auth Header
// ============================================================================

func TestTier1_F9_AuthHeader_RawTokenAccepted(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)

	filePath := filepath.Join(h.ReplayDir, "raw.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("raw", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "raw", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
}

func TestTier1_F9_AuthHeader_BearerPrefixRejected401(t *testing.T) {
	h := SetupE2EHarness(t)
	// Passing "Bearer <key>" must trigger 401
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "Bearer test-ballchasing-token", "public", "", 1)

	filePath := filepath.Join(h.ReplayDir, "bearer.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("bearer", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "bearer", filePath)
	if err == nil {
		t.Fatal("expected 401 unauthorized when Bearer prefix is used")
	}
}

func TestTier1_F9_AuthHeader_MissingTokenRejected401(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "", "public", "", 1)

	filePath := filepath.Join(h.ReplayDir, "missing.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("missing", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "missing", filePath)
	if err == nil {
		t.Fatal("expected error with missing token")
	}
}

func TestTier1_F9_AuthHeader_WrongTokenRejected401(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "wrong-token-abc", "public", "", 1)

	filePath := filepath.Join(h.ReplayDir, "wrong.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("wrong", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "wrong", filePath)
	if err == nil {
		t.Fatal("expected error with wrong token")
	}
}

func TestTier1_F9_AuthHeader_SpecialCharacterToken(t *testing.T) {
	h := SetupE2EHarness(t)
	specialToken := "token_with-special!@#$"
	h.BC.SetExpectedToken(specialToken)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), specialToken, "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "special.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("special", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "special", filePath)
	if err != nil {
		t.Fatalf("upload with special character token failed: %v", err)
	}
}

// ============================================================================
// Feature 10: Ballchasing HTTP 201 Handling
// ============================================================================

func TestTier1_F10_201_ParseIDAndLocation(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	filePath := filepath.Join(h.ReplayDir, "f10.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f10", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "f10", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if res.ID == "" || !strings.Contains(res.Location, res.ID) {
		t.Fatalf("location does not match ID: id=%s, loc=%s", res.ID, res.Location)
	}
}

func TestTier1_F10_201_TransitionToUploaded(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m201"}})
	_ = store.MarkUploaded(ctx, "m201", "bc-201", "http://bc/201")

	rec, _ := store.GetMatch(ctx, "m201")
	if rec.UploadStatus != UploadUploaded {
		t.Fatalf("expected UPLOADED, got %s", rec.UploadStatus)
	}
}

func TestTier1_F10_201_RecordBallchasingID(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-id"}})
	_ = store.MarkUploaded(ctx, "m-id", "expected-id-xyz", "http://bc/id")

	rec, _ := store.GetMatch(ctx, "m-id")
	if rec.BallchasingID != "expected-id-xyz" {
		t.Fatalf("expected expected-id-xyz, got %s", rec.BallchasingID)
	}
}

func TestTier1_F10_201_RecordBallchasingURL(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-url"}})
	_ = store.MarkUploaded(ctx, "m-url", "id", "https://ballchasing.com/replay/12345")

	rec, _ := store.GetMatch(ctx, "m-url")
	if rec.BallchasingURL != "https://ballchasing.com/replay/12345" {
		t.Fatalf("unexpected BallchasingURL: %s", rec.BallchasingURL)
	}
}

func TestTier1_F10_201_UpdateUploadedAtTimestamp(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-ts"}})
	_ = store.MarkUploaded(ctx, "m-ts", "id", "http://bc/id")

	rec, _ := store.GetMatch(ctx, "m-ts")
	if rec.UploadedAt == nil || rec.UploadedAt.IsZero() {
		t.Fatal("expected non-nil UploadedAt timestamp")
	}
}
