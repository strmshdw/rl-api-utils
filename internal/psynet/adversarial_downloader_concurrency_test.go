package psynet

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

func TestAdversarial_Downloader_ConcurrencyAndContention(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader(WithTimeout(10 * time.Second))

	t.Run("ConcurrentDistinctGUIDs_30Workers", func(t *testing.T) {
		const numWorkers = 30
		var wg sync.WaitGroup
		errCh := make(chan error, numWorkers)

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				guid := fmt.Sprintf("distinct-concur-%03d", idx)
				path, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
				if err != nil {
					errCh <- fmt.Errorf("worker %d failed: %w", idx, err)
					return
				}
				if !strings.HasSuffix(path, ".replay") {
					errCh <- fmt.Errorf("worker %d unexpected path: %s", idx, path)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatalf("concurrent download error: %v", err)
		}

		entries, err := os.ReadDir(destDir)
		if err != nil || len(entries) != numWorkers {
			t.Fatalf("expected %d files on disk, found %d (err=%v)", numWorkers, len(entries), err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), TempFilePrefix) {
				t.Fatalf("stray temp file found: %s", e.Name())
			}
		}
	})

	t.Run("ConcurrentSameGUID_ControlledContention", func(t *testing.T) {
		controlledDir := t.TempDir()
		sameGUID := "controlled-identical-guid"
		controlledDownloader := NewDownloader(WithRenameRetries(15))
		const numWorkers = 5

		var wg sync.WaitGroup
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = controlledDownloader.DownloadReplay(context.Background(), sameGUID, cdn.ReplayURL(sameGUID), controlledDir)
			}()
		}
		wg.Wait()

		finalPath := filepath.Join(controlledDir, fmt.Sprintf("%s.replay", sameGUID))
		if _, err := os.Stat(finalPath); err != nil {
			t.Fatalf("expected final file %s to exist: %v", finalPath, err)
		}
		entries, _ := os.ReadDir(controlledDir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), TempFilePrefix) {
				t.Fatalf("orphaned temp file found after controlled contention: %s", e.Name())
			}
		}
	})

	t.Run("ConcurrentSameGUID_StampedeInvariants", func(t *testing.T) {
		stampedeDir := t.TempDir()
		sameGUID := "stampede-invariants-guid"
		const numStampede = 20

		var wg sync.WaitGroup
		var successCount atomic.Int32
		for i := 0; i < numStampede; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := downloader.DownloadReplay(context.Background(), sameGUID, cdn.ReplayURL(sameGUID), stampedeDir); err == nil {
					successCount.Add(1)
				}
			}()
		}
		wg.Wait()

		if successCount.Load() == 0 {
			t.Fatal("expected at least one worker to succeed during stampede")
		}

		finalPath := filepath.Join(stampedeDir, fmt.Sprintf("%s.replay", sameGUID))
		data, err := os.ReadFile(finalPath)
		if err != nil || !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
			t.Fatalf("final stampede file missing or corrupted: err=%v", err)
		}

		entries, err := os.ReadDir(stampedeDir)
		if err != nil {
			t.Fatalf("failed reading stampedeDir: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), TempFilePrefix) {
				t.Fatalf("orphaned temp file leaked by stampede worker: %s", e.Name())
			}
		}
		if len(entries) != 1 {
			t.Fatalf("expected exactly 1 final file in stampedeDir, found %d", len(entries))
		}
	})
}

func TestDownloader_MaxRetries_ImmediateFail404And410_Retries5xx(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(fmt.Sprintf("ImmediateFail_%d", status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			defer srv.Close()

			dl := NewDownloader(WithMaxRetries(3))
			_, err := dl.DownloadReplay(context.Background(), "guid-404", srv.URL+"/r.replay", t.TempDir())
			var httpErr *HTTPStatusError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != status {
				t.Fatalf("expected HTTPStatusError(%d), got %v", status, err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("expected HTTP %d to fail immediately on 1 attempt, got %d", status, got)
			}
		})
	}

	t.Run("RetriesTransient503ThenSucceeds", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(testutil.GenerateValidReplay("retry-ok", 2048))
		}))
		defer srv.Close()

		dl := NewDownloader(WithMaxRetries(3))
		path, err := dl.DownloadReplay(context.Background(), "retry-ok", srv.URL+"/r.replay", t.TempDir())
		if err != nil {
			t.Fatalf("expected retry recovery on 503, got %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("downloaded file missing: %v", err)
		}
		if got := calls.Load(); got != 3 {
			t.Fatalf("expected 3 attempts, got %d", got)
		}
	})
}

func TestDownloader_ValidateReplay_ParseReplayHeaderCheck(t *testing.T) {
	destDir := t.TempDir()
	validPayload := buildMinimalUE3Replay(2048)
	corruptPayload := bytes.Repeat([]byte("NOT_A_REPLAY"), 200)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "corrupt") {
			_, _ = w.Write(corruptPayload)
			return
		}
		_, _ = w.Write(validPayload)
	}))
	defer srv.Close()

	dl := NewDownloader(WithValidateReplay(true))

	// Corrupt payload fails replay.ParseReplay and leaves zero temp files
	_, err := dl.DownloadReplay(context.Background(), "corrupt-guid", srv.URL+"/corrupt.replay", destDir)
	if err == nil || !strings.Contains(err.Error(), "replay validation failed") {
		t.Fatalf("expected replay validation error, got: %v", err)
	}
	if entries, _ := os.ReadDir(destDir); len(entries) != 0 {
		t.Fatalf("expected zero temp/final files after invalid replay, found %d", len(entries))
	}

	// Valid UE3 replay payload succeeds
	path, err := dl.DownloadReplay(context.Background(), "valid-ue3-guid", srv.URL+"/valid.replay", destDir)
	if err != nil {
		t.Fatalf("expected valid UE3 replay to pass validation, got: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected valid replay on disk: %v", err)
	}
}

func buildMinimalUE3Replay(minBytes int) []byte {
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, int32(867)) // EngineVersion
	_ = binary.Write(&body, binary.LittleEndian, int32(18))  // LicenseeVersion
	writeFStr(&body, "TAGame.Replay_Soccar_TA")
	writeFStr(&body, "None")

	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, int32(body.Len()))
	_ = binary.Write(&out, binary.LittleEndian, int32(0)) // CRC
	out.Write(body.Bytes())
	for out.Len() < minBytes {
		out.WriteByte(0)
	}
	return out.Bytes()
}

func writeFStr(buf *bytes.Buffer, s string) {
	_ = binary.Write(buf, binary.LittleEndian, int32(len(s)+1))
	buf.WriteString(s)
	buf.WriteByte(0)
}
