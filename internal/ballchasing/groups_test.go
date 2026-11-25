package ballchasing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

func TestClient_UploadReplayWithOptions_FilenameAndGroup(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:      srv.URL(),
		APIKey:       defaultTestToken,
		Visibility:   "unlisted",
		Group:        "default-parent-group",
		MaxRetries:   2,
		BaseBackoff:  5 * time.Millisecond,
		StreamUpload: true,
		RateLimiter:  NewRateLimiter(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	guid := "raw-guid-1234"
	filePath := helperCreateReplayFile(t, "2026_11_21_001_2v2_WIN", 2048)

	res, err := client.UploadReplayWithOptions(context.Background(), guid, filePath, UploadOptions{
		Filename: "2026_11_21_001_2v2_WIN.replay",
		GroupID:  "grp-session-1-2v2",
	})
	if err != nil {
		t.Fatalf("UploadReplayWithOptions: %v", err)
	}
	if res == nil || res.ID == "" || res.IsDuplicate {
		t.Fatalf("unexpected upload result: %+v", res)
	}

	uploads := srv.GetUploads()
	if len(uploads) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(uploads))
	}
	if uploads[0].FileName != "2026_11_21_001_2v2_WIN.replay" {
		t.Fatalf("expected multipart filename 2026_11_21_001_2v2_WIN.replay, got %q", uploads[0].FileName)
	}
	if uploads[0].Group != "grp-session-1-2v2" {
		t.Fatalf("expected group query param grp-session-1-2v2, got %q", uploads[0].Group)
	}
	if uploads[0].Visibility != "unlisted" {
		t.Fatalf("expected visibility unlisted, got %q", uploads[0].Visibility)
	}
}

func TestClient_CreateGroup_And_PatchReplay(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		Group:       "root-parent",
		MaxRetries:  2,
		BaseBackoff: 5 * time.Millisecond,
		RateLimiter: NewRateLimiter(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.DefaultGroup() != "root-parent" {
		t.Fatalf("expected DefaultGroup root-parent, got %q", client.DefaultGroup())
	}

	// Simulate one transient 429 rate limit to verify CreateGroup retries cleanly.
	srv.SimulateRateLimit(1, 0)

	grp, err := client.CreateGroup(context.Background(), CreateGroupRequest{
		Name:   "2026_11_21_1_2v2",
		Parent: client.DefaultGroup(),
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if grp == nil || grp.ID == "" || grp.Location == "" {
		t.Fatalf("unexpected CreateGroupResponse: %+v", grp)
	}

	groups := srv.GetGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 created group, got %d", len(groups))
	}
	if groups[0].Name != "2026_11_21_1_2v2" || groups[0].Parent != "root-parent" {
		t.Fatalf("unexpected group payload: %+v", groups[0])
	}
	if groups[0].PlayerIdentification != "by-id" || groups[0].TeamIdentification != "by-distinct-players" {
		t.Fatalf("unexpected group identification defaults: %+v", groups[0])
	}

	// PatchReplay should set both title and group and succeed on HTTP 204.
	if err := client.PatchReplay(context.Background(), "replay-uuid-42", PatchReplayRequest{
		Title: "2026_11_21_001_2v2_WIN",
		Group: grp.ID,
	}); err != nil {
		t.Fatalf("PatchReplay: %v", err)
	}
	patch := srv.GetReplayPatch("replay-uuid-42")
	if patch == nil || patch.Title != "2026_11_21_001_2v2_WIN" || patch.Group != grp.ID {
		t.Fatalf("unexpected recorded patch: %+v", patch)
	}

	// Validation errors
	if _, err := client.CreateGroup(context.Background(), CreateGroupRequest{Name: "   "}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("expected ErrBadRequest for empty group name, got %v", err)
	}
	if err := client.PatchReplay(context.Background(), "", PatchReplayRequest{Title: "x"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("expected ErrBadRequest for empty replay ID, got %v", err)
	}
}
