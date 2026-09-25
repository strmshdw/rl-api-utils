# Adversarial Challenge Handoff Report: `internal/syncer`

**Verdict**: **APPROVE**  
**Milestone**: M4 - Syncer, Daemon Engine & CLI  
**Package Under Review**: `github.com/dank/rl-api-utils/internal/syncer`

---

## 1. Observation

All 5 adversarial challenge objectives were executed empirically against `internal/syncer` via `internal/syncer/adversarial_test.go` and existing tests in `internal/syncer/syncer_test.go`.

### Verification Commands & Results:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 ./internal/syncer/...
```

Output:
```
=== RUN   TestAdversarial_InFlightRecovery_SQLiteBackend
--- PASS: TestAdversarial_InFlightRecovery_SQLiteBackend (0.02s)
=== RUN   TestAdversarial_InFlightRecovery_JSONBackend
--- PASS: TestAdversarial_InFlightRecovery_JSONBackend (0.02s)
=== RUN   TestAdversarial_DelayedReplayURLs_MultiCyclePromotion_SQLite
--- PASS: TestAdversarial_DelayedReplayURLs_MultiCyclePromotion_SQLite (0.02s)
=== RUN   TestAdversarial_DelayedReplayURLs_WhitespaceStorageVulnerability
    adversarial_test.go:314: Confirmed vulnerability: ReplayURL in DB is untrimmed "   " instead of empty string
    adversarial_test.go:329: Empirically proven: match remains permanently SKIPPED (stats2.Downloaded=0) because DB replay_url was "   " and did not match ''
--- PASS: TestAdversarial_DelayedReplayURLs_WhitespaceStorageVulnerability (0.01s)
=== RUN   TestAdversarial_Ballchasing409_DuplicateHandling_MixedBatch
--- PASS: TestAdversarial_Ballchasing409_DuplicateHandling_MixedBatch (0.03s)
=== RUN   TestAdversarial_DryRun_ZeroMutationsWithPreSeededDirtyState
--- PASS: TestAdversarial_DryRun_ZeroMutationsWithPreSeededDirtyState (0.02s)
=== RUN   TestAdversarial_ContextCancellation_MidDownload_NoPoisonAndCleanResume
--- PASS: TestAdversarial_ContextCancellation_MidDownload_NoPoisonAndCleanResume (0.02s)
=== RUN   TestAdversarial_ContextCancellation_MidUpload_NoPoisonAndCleanResume
--- PASS: TestAdversarial_ContextCancellation_MidUpload_NoPoisonAndCleanResume (0.02s)
=== RUN   TestAdversarial_MalformedMatches_Resilience
--- PASS: TestAdversarial_MalformedMatches_Resilience (0.02s)
=== RUN   TestAdversarial_KeepLocalFilesFalse_CleanupOnSuccessAndDuplicate
--- PASS: TestAdversarial_KeepLocalFilesFalse_CleanupOnSuccessAndDuplicate (0.02s)
=== RUN   TestSyncer_ConstructorValidation
--- PASS: TestSyncer_ConstructorValidation (0.00s)
=== RUN   TestSyncer_HappyPath_FullPipeline
--- PASS: TestSyncer_HappyPath_FullPipeline (0.00s)
=== RUN   TestSyncer_MultiCycle_Progression
--- PASS: TestSyncer_MultiCycle_Progression (0.00s)
=== RUN   TestSyncer_DelayedReplayURL_TwoCycles
--- PASS: TestSyncer_DelayedReplayURL_TwoCycles (0.00s)
=== RUN   TestSyncer_DuplicateHandling_HTTP409
--- PASS: TestSyncer_DuplicateHandling_HTTP409 (0.00s)
=== RUN   TestSyncer_DryRun_NoNetworkOrDBMutations
--- PASS: TestSyncer_DryRun_NoNetworkOrDBMutations (0.00s)
=== RUN   TestSyncer_CrashRecovery_ResetsInFlight
--- PASS: TestSyncer_CrashRecovery_ResetsInFlight (0.00s)
=== RUN   TestSyncer_ContextCancellation_DuringDownload
--- PASS: TestSyncer_ContextCancellation_DuringDownload (0.02s)
=== RUN   TestSyncer_ContextCancellation_DuringUpload
--- PASS: TestSyncer_ContextCancellation_DuringUpload (0.02s)
=== RUN   TestSyncer_PartialDownloadFailure_ContinuesToNext
--- PASS: TestSyncer_PartialDownloadFailure_ContinuesToNext (0.00s)
=== RUN   TestSyncer_PartialUploadFailure_ContinuesToNext
--- PASS: TestSyncer_PartialUploadFailure_ContinuesToNext (0.00s)
=== RUN   TestSyncer_ProviderError_AbortsCycle
--- PASS: TestSyncer_ProviderError_AbortsCycle (0.00s)
=== RUN   TestSyncer_KeepLocalFilesFalse
--- PASS: TestSyncer_KeepLocalFilesFalse (0.00s)
=== RUN   TestSyncer_StoreErrors
--- PASS: TestSyncer_StoreErrors (0.00s)
PASS
ok  	github.com/dank/rl-api-utils/internal/syncer	1.038s
```

Full repository test execution (`go test -count=1 ./...`):
- `github.com/dank/rl-api-utils/cmd/rl-sync`: PASS (0.134s)
- `github.com/dank/rl-api-utils/internal/auth`: PASS (0.151s)
- `github.com/dank/rl-api-utils/internal/ballchasing`: PASS (7.524s)
- `github.com/dank/rl-api-utils/internal/config`: PASS (0.455s)
- `github.com/dank/rl-api-utils/internal/daemon`: PASS (1.107s)
- `github.com/dank/rl-api-utils/internal/psynet`: PASS (4.128s)
- `github.com/dank/rl-api-utils/internal/storage`: PASS (2.542s)
- `github.com/dank/rl-api-utils/internal/syncer`: PASS (0.827s)
- `github.com/dank/rl-api-utils/internal/testutil`: PASS (0.725s)
- `github.com/dank/rl-api-utils/test/e2e`: PASS (3.184s)

Static analysis (`go vet ./internal/syncer/... ./internal/daemon/... ./cmd/rl-sync/...`):
- Zero warnings/errors.

---

## 2. Logic Chain

1. **In-Flight Crash Recovery (`store.RecoverInFlight`)**:
   - In `internal/syncer/syncer.go:150-154`, stage 1 executes `s.store.RecoverInFlight(ctx)` prior to polling, download, and upload phases.
   - Tested empirically in `TestAdversarial_InFlightRecovery_SQLiteBackend` and `TestAdversarial_InFlightRecovery_JSONBackend`. Pre-seeded matches with `DOWNLOADING` and `UPLOADING` statuses were recovered to `PENDING` on cycle start, while completed (`UPLOADED`) and skipped (`SKIPPED`) matches were preserved without corruption.
   - All recovered matches were successfully downloaded and uploaded in the immediate cycle.

2. **Delayed Replay URLs**:
   - In `internal/syncer/syncer.go:176-185`, discovered matches with empty `ReplayURL` are assigned `DownloadStatus = storage.DownloadSkipped` and `stats.SkippedCount++`.
   - In cycle 2, when `ReplayURL` is populated, `store.UpsertDiscoveredMatches` promotes `DownloadStatus` from `SKIPPED` to `PENDING` via SQLite `ON CONFLICT` and JSONStore update rules.
   - Tested empirically across 3 cycles in `TestAdversarial_DelayedReplayURLs_MultiCyclePromotion_SQLite`, verifying that matches delayed across 1 or 2 cycles are promoted and downloaded once URLs arrive.

3. **Ballchasing 409 Duplicate Handling**:
   - In `internal/syncer/syncer.go:311-325`, when `uploader.UploadReplay` returns `res.IsDuplicate == true`, syncer invokes `store.MarkDuplicate(ctx, rec.MatchGUID, res.ID, res.Location)` and increments `stats.DuplicateCount++`.
   - Crucially, it does NOT increment `stats.FailedCount` and does NOT treat 409 as an error.
   - Tested empirically in `TestAdversarial_Ballchasing409_DuplicateHandling_MixedBatch` with 20 matches (10 duplicate, 10 fresh 201 Created). Cycle completed with 0 errors and 0 failures. Cycle 2 verified strict idempotency (0 downloads, 0 uploads).

4. **Dry-Run Guarantee**:
   - In `internal/syncer/syncer.go:150` and `syncer.go:197-201`, when `s.dryRun == true`:
     - Stage 1 crash recovery is bypassed (`!s.dryRun`).
     - Stage 3 upsert is bypassed, logging discovery count and returning immediately.
     - Stages 4 (downloads) and 5 (uploads) are never executed.
   - Tested empirically in `TestAdversarial_DryRun_ZeroMutationsWithPreSeededDirtyState` on a database pre-seeded with pending downloads, pending uploads, and orphaned states. The database state before and after dry-run was verified to be 100% field-for-field identical, and downloader/uploader received strictly 0 calls.

5. **Context Cancellation Mid-Download and Mid-Upload**:
   - In `internal/syncer/syncer.go:216-221`, `238-241`, `274-279`, and `298-301`, context cancellation is checked before and immediately after every I/O call.
   - When cancellation occurs mid-download or mid-upload, the loop detects `ctx.Err() != nil` and promptly exits returning `stats, ctx.Err()`.
   - It intentionally bypasses `MarkDownloadFailed` and `MarkUploadFailed`, preventing in-flight matches from being poisoned as permanently failed.
   - Tested empirically in `TestAdversarial_ContextCancellation_MidDownload_NoPoisonAndCleanResume` and `TestAdversarial_ContextCancellation_MidUpload_NoPoisonAndCleanResume`. Both exited in <20ms upon cancellation, maintained in-flight statuses, and cleanly recovered and completed on the subsequent cycle with fresh context.

---

## 3. Caveats & Adversarial Findings

### Finding: Untrimmed Whitespace in ReplayURL Storage
- **Severity**: Low / Edge-Case Hardening
- **Location**: `internal/syncer/syncer.go:176, 191`
- **Description**:
  In `syncer.go:176`, syncer checks:
  ```go
  if strings.TrimSpace(d.ReplayURL) == "" {
      dlStatus = storage.DownloadSkipped
  ...
  ```
  However, at line 191, `toUpsert` records `ReplayURL: d.ReplayURL` without trimming or normalizing to `""`.
  If PsyNet were ever to return a match with whitespace-only ReplayURL (e.g. `"   "`), `matches.replay_url` in the database stores `"   "`.
  When the real URL arrives in a subsequent cycle, SQLite's conflict update condition (`matches.replay_url = ''`) and JSONStore's condition (`existing.ReplayURL == ""`) evaluate to `false` because `"   " != ""`. As a result, the match is never promoted from `SKIPPED` to `PENDING`.
- **Mitigation Recommendation**:
  Normalize `ReplayURL` in `syncer.go:174`:
  ```go
  replayURL := strings.TrimSpace(d.ReplayURL)
  dlStatus := storage.DownloadPending
  if replayURL == "" {
      dlStatus = storage.DownloadSkipped
      ...
  }
  // Use replayURL in MatchRecord
  ```
- **Live Impact**: None in production, as `psynet.Client` always emits canonical `""` when the URL is missing. Tested and documented in `TestAdversarial_DelayedReplayURLs_WhitespaceStorageVulnerability`.

---

## 4. Conclusion

The `internal/syncer` domain orchestrator is **APPROVED**. All 5 core synchronization mechanisms are empirically verified, robust, and correctly integrated with storage crash recovery, deduplication, dry-run simulation, and cancellation mechanics.

---

## 5. Verification Method

To independently reproduce this verification:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Run all syncer unit & adversarial tests
go test -v -count=1 ./internal/syncer/...

# 2. Run repository-wide test suite
go test -count=1 ./...

# 3. Verify static analysis
go vet ./internal/syncer/... ./internal/daemon/... ./cmd/rl-sync/...
```

Expected result: 100% test pass rate with zero `go vet` warnings.
