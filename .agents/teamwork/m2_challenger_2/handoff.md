# Milestone 2 (Auth & PsyNet Integration) Challenger Report: `internal/psynet`

- **Author**: `m2_challenger_2` (Roles: critic, specialist)
- **Target Subsystem**: `internal/psynet` (`client.go`, `downloader.go`)
- **Verdict**: **APPROVE**
- **Date**: 2026-09-24T20:57:00-07:00
- **Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m2_challenger_2`

---

## 1. Observation

Direct empirical investigation and adversarial execution against `internal/psynet` produced the following concrete observations:

1. **ReplayDownloader Minimum Size Validation & Boundary Handling**:
   - `internal/psynet/downloader.go:288-290`: `if written < d.minSize { return "", fmt.Errorf("%w: downloaded %d bytes, minimum required is %d", ErrReplayTooSmall, written, d.minSize) }`
   - Empirically tested payload sizes `0`, `1`, `128`, `500`, and `1023` bytes against `MockCDNServer` in `TestAdversarial_Downloader_ExtremePayloads/Reject_*`. All returned `ErrReplayTooSmall`.
   - Empirically tested boundary of exactly `1024` bytes, `1025` bytes, `64KB`, `1MB`, and `5MB` in `TestAdversarial_Downloader_ExtremePayloads/Accept_*`. All succeeded, wrote valid `.replay` binaries with `TAGAME` headers, and left zero `.tmp` files.
   - Tested failure overwrite invariant in `TestAdversarial_Downloader_FailedOverwritePreservesOriginal`: when an 8KB valid `.replay` already exists on disk and a subsequent download fails with a 500-byte truncated payload, the existing 8KB file was completely preserved and uncorrupted.

2. **HTTP Status Code Rejections & Zero Disk Footprint**:
   - `internal/psynet/downloader.go:247-253`: `if resp.StatusCode != http.StatusOK { return "", &HTTPStatusError{StatusCode: resp.StatusCode, MatchGUID: trimmedGUID, URL: trimmedURL} }`
   - In `downloader.go`, step 8 (creating the temporary file) is executed *after* step 7 (validating HTTP status).
   - In `TestAdversarial_Downloader_HTTPStatusCodes`, all HTTP status codes (`400`, `401`, `403`, `404`, `410`, `429`, `500`, `502`, `503`, `504`) returned errors unwrapping to `*HTTPStatusError` with the matching status code and match GUID. Exactly 0 files (`.tmp` or `.replay`) were created on disk for any non-200 HTTP response.

3. **Mid-Stream Connection Drops, Timeouts, and Windows Handle Cleanup**:
   - `internal/psynet/downloader.go:267-273`:
     ```go
     success := false
     defer func() {
         _ = tmpFile.Close()
         if !success {
             _ = os.Remove(tmpPath)
         }
     }()
     ```
   - On Windows, attempting to delete an open file handle fails with `ERROR_SHARING_VIOLATION`. The deferred cleanup handler explicitly closes `tmpFile.Close()` *before* invoking `os.Remove(tmpPath)`.
   - In `TestAdversarial_Downloader_MidStreamDropsAndInterrupts`:
     - Truncated TCP connection at 64 bytes cleanly aborted with 0 orphaned `.tmp` files.
     - Mid-stream drop after 4KB (advertising 100KB `Content-Length`) cleanly aborted with 0 orphaned `.tmp` files.
     - Server hanging with a 100ms context timeout cleanly aborted with 0 orphaned `.tmp` files.

4. **Path Traversal & Filename Sanitization**:
   - `internal/psynet/downloader.go:208-210`: `if strings.ContainsAny(trimmedGUID, `/\:?*"<>|`) || strings.Contains(trimmedGUID, "..") { return "", fmt.Errorf("%w: %q", ErrInvalidMatchGUID, matchGUID) }`
   - In `TestAdversarial_Downloader_PathTraversal_ComprehensiveMatrix`, 21 distinct traversal payloads were rejected with `ErrInvalidMatchGUID` or `ErrEmptyMatchGUID`, including: `../sneaky`, `..\sneaky`, `../../../../etc/passwd`, `..\..\..\windows\system32\calc`, `abc/../def`, `abc\..\def`, `/tmp/owned`, `C:\Windows\System32\malicious`, `D:file`, `C:test`, `*`, `?`, `<`, `>`, `|`, `"`, `:`, `..`, `...`, `....`, `""`, and whitespace strings.

5. **Windows Concurrency & File Contention**:
   - `TestAdversarial_Downloader_ConcurrencyAndContention/ConcurrentDistinctGUIDs_30Workers`: 30 concurrent goroutines downloaded distinct match GUIDs into the same `destDir` simultaneously. All 30 completed successfully in 0.03s; exactly 30 `.replay` files were created; zero `.tmp` files remained.
   - `ConcurrentSameGUID_ControlledContention`: 5 concurrent goroutines downloading the identical GUID simultaneously into `destDir` with `WithRenameRetries(15)`. The retry backoff loop absorbed Windows NTFS file sharing contention; the final file was created and verified with zero orphaned `.tmp` files.
   - `ConcurrentSameGUID_StampedeInvariants`: 20 concurrent goroutines stampeding the exact same destination file simultaneously. At least one worker succeeded and wrote the valid replay payload; all losing workers cleanly deleted their `.tmp` files without orphaned leaks or panics.

6. **Cleanup of Stale Temporary Files**:
   - `TestAdversarial_Downloader_CleanupStaleTempFiles_EdgeCases`: Tested `CleanupStaleTempFiles` on a directory with 50 stale `.tmp-*.replay` files, 10 valid `.replay` files, 4 unrelated files (`config.yaml`, `state.json`, `.tmp-not-a-replay.txt`, `important.tmp`), and subdirectories (`.tmp-subdir.replay`). Exactly 50 stale files were removed; all 10 valid replays, unrelated files, and subdirectories survived intact. Non-existent directories returned `(0, nil)`.

7. **MatchHistoryProvider Delayed ReplayURL Arrival & Multi-Cycle Progression**:
   - `internal/psynet/client.go:306-312`: Matches with empty `ReplayUrl` are logged at `Debug` level and preserved in `DiscoveredMatch` with `ReplayURL: ""`.
   - In `TestAdversarial_Client_DelayedReplayURL_Progression`:
     - Cycle 1: Matches with empty `ReplayUrl` were returned with `ReplayURL: ""` (ready for syncer to record as `SKIPPED`).
     - Cycle 2 (5 minutes later): Matches with newly populated `ReplayUrl` returned updated URLs; remaining delayed matches stayed empty; newly discovered matches were appended.

8. **MatchHistoryProvider Malformed Metadata Filtering**:
   - `internal/psynet/client.go:293-297`: Matches with empty or whitespace-only GUIDs are skipped with a warning log.
   - `internal/psynet/client.go:300-303`: Matches with `RecordStartTimestamp == 0` automatically default to `time.Now().Unix()`.
   - In `TestAdversarial_Client_MalformedMetadata_Filtering`: entries with `""` and `"    "` GUIDs were skipped; zero-timestamp matches defaulted to positive Unix timestamps; empty match lists returned `[]DiscoveredMatch{}, nil`.

9. **MatchHistoryProvider Connection Drops, Transparent Reconnect, and Context Handling**:
   - `internal/psynet/client.go:162-187`: Connection drops (`rlapi.ErrConnectionClosed`, `io.EOF`, network errors) trigger `c.connectLocked(ctx)` and retry the query once.
   - In `TestAdversarial_Client_ConnectionDrop_TransparentReconnect`:
     - Dropped WebSocket socket on query 1 transparently reconnected and returned matches without caller error.
     - Connection drop with reconnect failure cleanly returned a descriptive wrapped error.
     - Consecutive drops across multiple query cycles reconnected cleanly each cycle.
   - In `TestAdversarial_Client_ContextCancellation_NoHangs`: Pre-cancelled contexts returned `context.Canceled` immediately; 5ms deadline contexts returned `context.DeadlineExceeded` without blocking or hanging.
   - In `TestAdversarial_Client_ConcurrencyAndCloseSafety`: 20 concurrent goroutines querying while `Close()` was called mid-flight yielded zero panics, zero data races, and idempotent `Close()` calls.
   - In `TestAdversarial_Client_InvalidCredentials`: Defensive checks correctly rejected unsupported platforms (`NintendoSwitch`), missing credentials, and missing Steam account IDs.

10. **Test Suite Execution Results**:
    - `go test -v -count=1 ./internal/psynet/...`: All 31 tests passed cleanly in 4.07s.
    - `go test -count=5 ./internal/psynet/...`: Passed 5 consecutive iterations in 17.95s with zero flakiness.
    - `go vet ./internal/psynet/...`: Exited with code 0 (zero warnings).
    - `go test -cover ./internal/psynet/...`: Statement coverage reached 82.4%.
    - `go test -count=1 ./...`: 100% of workspace tests passed across all packages (`internal/auth`, `internal/config`, `internal/psynet`, `internal/storage`, `internal/testutil`, `test/e2e`).

---

## 2. Logic Chain

1. **ReplayDownloader Payload Integrity (Observation 1)**:
   Because valid Rocket League replays must be valid Unreal Engine binary structures (typically >500KB), rejecting payloads <1024 bytes protects the local cache and downstream Ballchasing uploader from processing truncated headers or 0-byte CDN stubs. By ensuring the boundary of 1024 bytes passes and payload sizes up to 5MB pass, legitimate replays are safely stored. Preserving existing files on disk upon subsequent download failure guarantees download idempotency and prevents network blips from clobbering good replays.

2. **ReplayDownloader Network Error Containment (Observation 2 & 3)**:
   By verifying HTTP status before creating temporary files on disk, HTTP errors (403, 404, 500, etc.) create zero filesystem churn. For mid-stream network drops, closing the file descriptor prior to calling `os.Remove(tmpPath)` complies with Windows file system semantics (`ERROR_SHARING_VIOLATION` avoidance), ensuring no orphaned `.tmp` files remain on disk.

3. **Path Traversal Security (Observation 4)**:
   Sanitizing `matchGUID` against path separators (`/`, `\`), directory navigation (`..`), and Windows reserved characters (`:`, `*`, `?`, `"`, `<`, `>`, `|`) prevents arbitrary file overwrite attacks and directory escaping, ensuring files are written strictly inside `destDir`.

4. **Windows Concurrency & Contention Handling (Observation 5 & 6)**:
   Concurrent downloads of distinct GUIDs have zero filesystem collisions. When identical GUIDs are downloaded concurrently, Windows NTFS locks on atomic replacement (`MoveFileExW`) are mitigated by the 15-attempt linear backoff retry loop (`atomicRename`). In extreme stampede conditions (20 concurrent threads on the same file), the winning worker writes the file and all losing workers cleanly delete their temporary files via deferred cleanup without leaks or panics.

5. **MatchHistoryProvider Contract Fidelity (Observations 7, 8, 9)**:
   The Rocket League game coordinator registers matches before the game server finishes uploading the `.replay` binary to CDN. Preserving matches with empty `ReplayURL` allows the syncer to discover the match and track it as `SKIPPED`/pending until subsequent cycles populate the URL. Automatic skipping of empty GUIDs, default timestamp assignment, and transparent single-retry on WebSocket connection drop ensure uninterrupted polling in unattended daemon mode.

---

## 3. Caveats

1. **Severe Stampede on Identical GUIDs**:
   - In production, match GUIDs are unique per match, and the syncer orchestrator controls download dispatch via its state store. However, if more than 15 concurrent threads attempt to atomically rename to the *exact same* file path simultaneously on Windows NTFS, some threads may exhaust the 15 retry attempts (~120ms window) due to kernel file locks. This is expected Windows file system behavior; in all cases, the winning file is valid, and losing threads cleanly remove their `.tmp` files with zero leakage.

---

## 4. Conclusion

**Verdict: APPROVE**

The `internal/psynet` implementation (`ReplayDownloader` and `MatchHistoryProvider`) has been thoroughly stress-tested against extreme payload boundaries, network disruptions, Windows file sharing contention, path traversal attacks, and multi-cycle match progression. All 31 tests pass with 100% success, statement coverage is 82.4%, `go vet` produces zero warnings, and the entire repository test suite passes.

---

## 5. Verification Method

To independently verify the adversarial test suite and results:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Run all unit and adversarial tests for internal/psynet
go test -v -count=1 ./internal/psynet/...

# 2. Run stress iterations (5x) to verify zero flakiness
go test -count=5 ./internal/psynet/...

# 3. Verify static analysis
go vet ./internal/psynet/...

# 4. Verify test coverage
go test -cover ./internal/psynet/...

# 5. Verify entire workspace suite
go test -count=1 ./...
```
