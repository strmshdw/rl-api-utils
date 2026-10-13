package replay

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestParseRealReplays(t *testing.T) {
	files, err := filepath.Glob("../../replays/*.replay")
	if err != nil || len(files) == 0 {
		t.Skip("no replay files found")
	}

	successCount := 0
	for _, f := range files {
		meta, err := ParseReplayFile(f)
		if err != nil {
			t.Logf("Failed parsing %s: %v", filepath.Base(f), err)
			continue
		}
		if meta.MatchGUID == "" {
			t.Errorf("Replay %s had empty MatchGUID", filepath.Base(f))
		}
		successCount++
	}

	t.Logf("Successfully parsed %d/%d real replay files", successCount, len(files))
	if successCount == 0 {
		t.Fatalf("Expected to parse real replays, but 0 succeeded")
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

