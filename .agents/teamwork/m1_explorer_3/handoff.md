# Handoff Report: Milestone M1 (Requirement R2) Programmatic Mid-Game Player Disconnect Tests

**Agent**: `m1_explorer_3`  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_3`  
**Date**: 2026-10-06T08:54:00Z  
**Type**: Hard Handoff  

---

## 1. Observation

### 1.1 Existing Test Patterns & Infrastructure

1. **`internal/playertrack/tracker_test.go` Architecture**:
   - Uses external test package `package playertrack_test` (lines 1-20).
   - Test fixture runner `forEachStore(t, func(t *testing.T, store storage.StateStore) { ... })` (lines 176-190) executes each test against both `modernc.org/sqlite` (`newSQLiteTestStore`) and structured JSON (`newJSONTestStore`), guaranteeing store engine parity.
   - Test double `newTestSkillFetcher()` (lines 38-117) provides controllable PsyNet rank queries.
   - Helper `makePlayer(name, id string, team int)` (lines 192-204) creates basic `statsapi.StatsPlayer` structs with fixed stats:
     ```go
     192: func makePlayer(name, id string, team int) statsapi.StatsPlayer {
     193: 	return statsapi.StatsPlayer{
     194: 		Name:      name,
     195: 		PrimaryId: id,
     196: 		TeamNum:   team,
     197: 		Score:     100,
     198: 		Goals:     1,
     199: 		Assists:   0,
     200: 		Saves:     1,
     201: 		Shots:     2,
     202: 		Demos:     0,
     203: 	}
     204: }
     ```
   - Prior test suites cover: Local Player Resolution Tiers 1–4 (lines 210-600), Classification & Bot Exclusion (lines 603-698), Splitscreen (lines 700-746), Lifecycle Win/Loss (lines 748-816), Playlist Isolation (lines 818-860), Debounce/RPC caching (lines 862-1008), Idempotency & Edge Cases (lines 1010-1108), Fault Injection (lines 1110-1147), and Concurrency Stress (lines 1149-1289).
   - **Observation**: Zero existing tests currently simulate mid-game player disconnects or verify retention of omitted players across consecutive `OnUpdateState` frames.

2. **`internal/session/session_test.go` Architecture**:
   - Uses internal test package `package session` (lines 1-25).
   - Tests `SessionTracker` behavior: Initial state (line 90), Single match recording (line 126), Win rate math (line 158), MMR deltas (line 215), Idempotency (line 310), Reset during active match (line 444), Observer integration (line 485), Concurrency stress (line 563).
   - In `TestSessionTracker_ObserverIntegration` (lines 485-561):
     ```go
     507: 		playertrack.WithMatchStateListener(s),
     ...
     533: 	if err := pt.OnUpdateState(ctx, "obs-guid-1", 11, players); err != nil {
     534: 		t.Fatalf("OnUpdateState failed: %v", err)
     535: 	}
     536: 
     537: 	// Verify session tracker received active match
     538: 	summary := s.GetSessionSummary()
     539: 	if summary.ActiveMatch == nil {
     540: 		t.Fatal("expected SessionTracker to receive active match from observer notification")
     541: 	}
     ```
   - In `session.go` (lines 88-91, 204-209):
     ```go
     88: func (s *SessionTracker) Subscribe() (<-chan SessionEvent, func()) {
     89: 	return s.broadcaster.Subscribe()
     90: }
     ...
     204: 	matchClone := s.currentMatch.DeepClone()
     205: 	s.mu.Unlock()
     206: 
     207: 	if s.broadcaster != nil && matchClone != nil {
     208: 		s.broadcaster.Broadcast(SessionEvent{Event: EventMatchUpdate, Data: matchClone})
     209: 	}
     ```
   - **Observation**: `SessionTracker` directly mirrors `playertrack.CurrentMatchResponse` into `s.currentMatch` and broadcasts `SessionEvent{Event: EventMatchUpdate, Data: matchClone}`. When `playertrack.Tracker` retains disconnected players, `SessionTracker` and SSE subscribers receive that retention automatically.

3. **Current Missing Fields in Production Models**:
   - In `internal/playertrack/tracker.go:49-60`: `LobbyPlayer` lacks `IsDisconnected bool json:"is_disconnected,omitempty"`.
   - In `internal/session/models.go:72-85`: `SessionMatchPlayer` lacks `IsDisconnected bool json:"is_disconnected,omitempty"`.
   - In `internal/playertrack/tracker.go:368-483`: `Tracker.OnUpdateState` reallocates empty slices (`teammates`, `opponents`, `spectators`) on each frame, purging absent players.
   - In `internal/playertrack/tracker.go:361-366`: `t.currentMatch.LocalTeam` is set to `nil` when the local player is absent from `players`.
   - In `internal/playertrack/tracker.go:608-619`: `OnMatchEnded` aborts if `localTeam == nil`, causing win/loss outcome recording to be skipped entirely for all participants.

4. **Baseline Test Execution**:
   - Command: `go test -count=1 ./internal/playertrack/... ./internal/session/...`
     Output: `ok github.com/dank/rl-api-utils/internal/playertrack 4.113s`, `ok github.com/dank/rl-api-utils/internal/session 5.159s` (100% pass).
   - Command: `go test ./...`
     Output: All 14 packages pass cleanly with 0 failures.

---

## 2. Logic Chain

1. **Stats API Semantics**: Rocket League's `MatchStatsExporter_TA` sends discrete `UpdateState` frames where `msg.Data.Players` contains only the players currently present in the lobby. When a player disconnects mid-game, their entry is simply omitted in subsequent frames (Observation 1.3).
2. **Failure Mode 1 (Active Match Purge)**: Because `OnUpdateState` currently builds `teammates`, `opponents`, and `spectators` from scratch without referencing prior frame participants, any omitted player is purged immediately. Their accumulated box score stats (`Score`, `Goals`, `Assists`, `Saves`, `Shots`, `Demos`) are permanently lost from `GetCurrentMatch()` and `SessionTracker.GetSessionSummary().ActiveMatch` (Observation 1.2, 1.3).
3. **Failure Mode 2 (Local Player Cascading Abort)**: If the local player leaves early, `resolveLocalPlayer` returns `nil, -1`, resetting `LocalTeam` to `nil` and `LocalPlayer` to `nil`. When the match finishes, `OnMatchEnded` encounters `localTeam == nil` and returns early at line 618, skipping `store.RecordMatchResults` for all remaining participants (Observation 1.3).
4. **Failure Mode 3 (Completed Match Goal Loss)**: In `SessionTracker.ConcludeMatch` (lines 245-270), team scores (`BlueScore`, `OrangeScore`) are summed from player goals. Purged disconnected players contribute 0 goals to the final score, distorting match history (Observation 1.2, 1.3).
5. **Requirements Derivation**:
   - Requirement R2 mandates persistent player state in the active game state when a player leaves early.
   - Dispatch mandates designing end-to-end programmatic tests covering:
     - **Frame 1**: Full lobby accumulating stats.
     - **Frame 2**: One or more players leave early -> retained in active state with preserved stats and `IsDisconnected = true`.
     - **Frame 3**: Reconnection -> stats updated, `IsDisconnected = false`, no duplicate rows.
     - **Frame 4**: Local player leaves early -> local player and team preserved.
6. **Test Design Strategy**:
   - Provide a new dedicated test suite `Test Suite 4: Mid-Game Disconnect & Player State Retention` in `internal/playertrack/tracker_test.go` with 5 targeted tests.
   - Provide a new test suite in `internal/session/session_test.go` with 3 integration and snapshot tests.
   - Define helper `makePlayerWithStats(name, id string, team int, score, goals, assists, saves, shots, demos int)` to cleanly parameterize stat counters.
   - Test across both SQLite and JSONStore engines via `forEachStore`.
   - Assert `len(Teammates) == 1` upon reconnection to explicitly fail if an implementation produces duplicate rows.
   - Assert `LocalTeam != nil && *LocalTeam == 0` when the local player disconnects, followed by concluding the match to ensure outcomes are successfully recorded in storage.

---

## 3. Caveats

1. **Compilation Dependency**: These designed tests reference the new field `IsDisconnected bool` on `playertrack.LobbyPlayer` and `session.SessionMatchPlayer`. Therefore, the test code is ready to be pasted by the worker once `IsDisconnected` has been added to `internal/playertrack/tracker.go` and `internal/session/models.go`.
2. **Zero CGO Environment**: The test suite runs in pure Go (`modernc.org/sqlite`, `CGO_ENABLED=0`). Tests must not rely on `-race` flags requiring CGO compilers, but all code must maintain strict internal lock synchronization (`t.mu.RLock()` / `t.mu.Lock()`).
3. **Casual Bot Backfill vs Human Retention**: In casual matches, an AI bot (`Unknown|0|0`) may backfill a disconnected human. Our test suite includes `TestTracker_MidGameDisconnect_BotReplacement` asserting that the human player remains retained as a disconnected participant, while the AI bot is added as an active participant (`len(Teammates) == 2`).
4. **Splitscreen Multi-Player Handling**: Splitscreen players share account IDs (e.g. `Steam|12345|0` and `Steam|12345|1`). Primary ID comparisons must always compare the full pipe-delimited string so splitscreen teammates are not conflated.

---

## 4. Conclusion & Complete Ready-to-Run Go Test Code

### 4.1 Recommended Test File Placement

1. **`internal/playertrack/tracker_test.go`**:
   - Add helper function `makePlayerWithStats` right after `makePlayer` (around line 205).
   - Add `Test Suite 4: Mid-Game Disconnect & Player State Retention` at the end of `tracker_test.go`.
2. **`internal/session/session_test.go`**:
   - Add `Test Suite: Mid-Game Disconnect Integration & Snapshots` at the end of `session_test.go`.

---

### 4.2 Test Suite Code for `internal/playertrack/tracker_test.go`

```go
// ============================================================================
// Helper Function for Custom Stats
// ============================================================================

func makePlayerWithStats(name, id string, team int, score, goals, assists, saves, shots, demos int) statsapi.StatsPlayer {
	return statsapi.StatsPlayer{
		Name:      name,
		PrimaryId: id,
		TeamNum:   team,
		Score:     score,
		Goals:     goals,
		Assists:   assists,
		Saves:     saves,
		Shots:     shots,
		Demos:     demos,
	}
}

// ============================================================================
// Test Suite 4: Mid-Game Disconnect & Player State Retention (Requirement R2)
// ============================================================================

// TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal covers the primary 4-frame lifecycle:
// Frame 1: Full lobby with active players accumulating stats.
// Frame 2: Teammate leaves early -> retained in active state with stats preserved and IsDisconnected=true.
// Frame 3: Disconnected teammate reconnects -> stats updated, IsDisconnected=false, NO duplicate entries.
// Frame 4: Local player leaves early -> local player and local team preserved, stats retained.
// Conclude: Match ends -> outcomes recorded in store because local player and team were preserved.
func TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "m-disconnect-lifecycle-1"
		playlistID := 11 // Ranked 2v2 Doubles

		// --------------------------------------------------------------------
		// Frame 1: Full Lobby, Active Players Accumulating Stats
		// --------------------------------------------------------------------
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 250, 1, 1, 2, 3, 1),
			makePlayerWithStats("Teammate1", "Steam|teammate_1|0", 0, 300, 2, 0, 1, 4, 2),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 180, 1, 1, 0, 2, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 120, 0, 1, 2, 1, 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1); err != nil {
			t.Fatalf("Frame 1 OnUpdateState failed: %v", err)
		}

		snapF1 := tracker.GetCurrentMatch()
		if snapF1 == nil {
			t.Fatal("Frame 1: expected active match snapshot, got nil")
		}
		if !snapF1.ActiveMatch || snapF1.MatchEnded {
			t.Errorf("Frame 1: expected ActiveMatch=true, MatchEnded=false; got ActiveMatch=%v, MatchEnded=%v",
				snapF1.ActiveMatch, snapF1.MatchEnded)
		}
		if snapF1.LocalPlayer == nil || snapF1.LocalPlayer.PlayerID != "Steam|local_user|0" {
			t.Fatalf("Frame 1: expected LocalPlayer 'Steam|local_user|0', got %+v", snapF1.LocalPlayer)
		}
		if snapF1.LocalPlayer.IsDisconnected {
			t.Errorf("Frame 1: local player should not be disconnected")
		}
		if snapF1.LocalTeam == nil || *snapF1.LocalTeam != 0 {
			t.Errorf("Frame 1: expected LocalTeam=0, got %v", snapF1.LocalTeam)
		}
		if len(snapF1.Teammates) != 1 {
			t.Fatalf("Frame 1: expected 1 teammate, got %d", len(snapF1.Teammates))
		}
		tmF1 := snapF1.Teammates[0]
		if tmF1.PlayerID != "Steam|teammate_1|0" || tmF1.IsDisconnected {
			t.Errorf("Frame 1: unexpected teammate state: %+v", tmF1)
		}
		if tmF1.Stats.Score != 300 || tmF1.Stats.Goals != 2 || tmF1.Stats.Demos != 2 {
			t.Errorf("Frame 1: unexpected teammate stats: %+v", tmF1.Stats)
		}
		if len(snapF1.Opponents) != 2 {
			t.Fatalf("Frame 1: expected 2 opponents, got %d", len(snapF1.Opponents))
		}

		// --------------------------------------------------------------------
		// Frame 2: Teammate Leaves Early (Omitted from UpdateState)
		// --------------------------------------------------------------------
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 270, 1, 1, 2, 4, 1),
			// Teammate1 omitted!
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 200, 1, 1, 1, 3, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 140, 0, 1, 2, 1, 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2); err != nil {
			t.Fatalf("Frame 2 OnUpdateState failed: %v", err)
		}

		snapF2 := tracker.GetCurrentMatch()
		if snapF2 == nil {
			t.Fatal("Frame 2: expected active match snapshot, got nil")
		}
		if !snapF2.ActiveMatch || snapF2.MatchEnded {
			t.Errorf("Frame 2: expected match to remain active")
		}

		// ASSERTION: Disconnected teammate MUST be retained in active match state
		if len(snapF2.Teammates) != 1 {
			t.Fatalf("Frame 2: expected disconnected teammate to be retained (len=1), got %d", len(snapF2.Teammates))
		}
		tmF2 := snapF2.Teammates[0]
		if tmF2.PlayerID != "Steam|teammate_1|0" {
			t.Errorf("Frame 2: expected retained teammate ID 'Steam|teammate_1|0', got %q", tmF2.PlayerID)
		}
		if !tmF2.IsDisconnected {
			t.Errorf("Frame 2: expected IsDisconnected=true for omitted teammate")
		}
		// ASSERTION: Accumulated stats MUST be preserved exactly
		if tmF2.Stats.Score != 300 || tmF2.Stats.Goals != 2 || tmF2.Stats.Assists != 0 ||
			tmF2.Stats.Saves != 1 || tmF2.Stats.Shots != 4 || tmF2.Stats.Demos != 2 {
			t.Errorf("Frame 2: disconnected teammate stats were modified or lost: %+v", tmF2.Stats)
		}
		// Active players in Frame 2 updated normally
		if snapF2.LocalPlayer == nil || snapF2.LocalPlayer.IsDisconnected {
			t.Errorf("Frame 2: local player should remain active and connected")
		}
		if snapF2.LocalPlayer.Stats.Score != 270 {
			t.Errorf("Frame 2: local player score not updated: got %d", snapF2.LocalPlayer.Stats.Score)
		}

		// --------------------------------------------------------------------
		// Frame 3: Disconnected Teammate Reconnects
		// --------------------------------------------------------------------
		playersF3 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 270, 1, 1, 2, 4, 1),
			// Teammate1 returns with updated score/shots
			makePlayerWithStats("Teammate1", "Steam|teammate_1|0", 0, 450, 3, 1, 2, 6, 2),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 220, 1, 1, 1, 3, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 160, 0, 1, 2, 2, 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF3); err != nil {
			t.Fatalf("Frame 3 OnUpdateState failed: %v", err)
		}

		snapF3 := tracker.GetCurrentMatch()
		// ASSERTION: No duplicate entries on reconnection
		if len(snapF3.Teammates) != 1 {
			t.Fatalf("Frame 3: expected exactly 1 teammate entry after reconnect (no duplicates), got %d", len(snapF3.Teammates))
		}
		tmF3 := snapF3.Teammates[0]
		if tmF3.PlayerID != "Steam|teammate_1|0" {
			t.Errorf("Frame 3: expected teammate ID 'Steam|teammate_1|0', got %q", tmF3.PlayerID)
		}
		if tmF3.IsDisconnected {
			t.Errorf("Frame 3: expected IsDisconnected=false after reconnection")
		}
		if tmF3.Stats.Score != 450 || tmF3.Stats.Goals != 3 || tmF3.Stats.Shots != 6 {
			t.Errorf("Frame 3: teammate stats not updated after reconnect: %+v", tmF3.Stats)
		}

		// --------------------------------------------------------------------
		// Frame 4: Local Player Leaves Early (Omitted from UpdateState)
		// --------------------------------------------------------------------
		playersF4 := []statsapi.StatsPlayer{
			// LocalUser omitted!
			makePlayerWithStats("Teammate1", "Steam|teammate_1|0", 0, 450, 3, 1, 2, 6, 2),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 220, 1, 1, 1, 3, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 160, 0, 1, 2, 2, 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF4); err != nil {
			t.Fatalf("Frame 4 OnUpdateState failed: %v", err)
		}

		snapF4 := tracker.GetCurrentMatch()
		// ASSERTION: Local player and local team preserved on early disconnect
		if snapF4.LocalPlayer == nil {
			t.Fatal("Frame 4: expected LocalPlayer to be retained when omitted, got nil")
		}
		if snapF4.LocalPlayer.PlayerID != "Steam|local_user|0" {
			t.Errorf("Frame 4: expected LocalPlayer ID 'Steam|local_user|0', got %q", snapF4.LocalPlayer.PlayerID)
		}
		if !snapF4.LocalPlayer.IsDisconnected {
			t.Errorf("Frame 4: expected LocalPlayer.IsDisconnected=true")
		}
		if snapF4.LocalPlayer.Stats.Score != 270 {
			t.Errorf("Frame 4: expected LocalPlayer stats preserved (Score=270), got %d", snapF4.LocalPlayer.Stats.Score)
		}
		if snapF4.LocalTeam == nil || *snapF4.LocalTeam != 0 {
			t.Errorf("Frame 4: expected LocalTeam preserved as 0, got %v", snapF4.LocalTeam)
		}

		// --------------------------------------------------------------------
		// Match Conclusion: Win/Loss Outcomes Recorded Despite Disconnects
		// --------------------------------------------------------------------
		winnerBlue := 0
		if err := tracker.OnMatchEnded(ctx, matchGUID, &winnerBlue); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		snapEnded := tracker.GetCurrentMatch()
		if snapEnded.ActiveMatch || !snapEnded.MatchEnded {
			t.Errorf("expected ActiveMatch=false, MatchEnded=true post-conclusion")
		}
		if snapEnded.Result != "victory" {
			t.Errorf("expected Result='victory', got %q", snapEnded.Result)
		}

		// Verify store recorded outcomes because local player/team was preserved
		tmRecord, err := store.GetPlayerMatchup(ctx, "Steam|teammate_1|0", playlistID)
		if err != nil {
			t.Fatalf("GetPlayerMatchup failed: %v", err)
		}
		if tmRecord.WinsAsTeammate != 1 || tmRecord.TotalMatches != 1 {
			t.Errorf("expected teammate recorded 1 win, got %+v", tmRecord)
		}

		rivalRecord, err := store.GetPlayerMatchup(ctx, "Steam|rival_1|0", playlistID)
		if err != nil {
			t.Fatalf("GetPlayerMatchup for rival failed: %v", err)
		}
		if rivalRecord.LossesAsOpponent != 1 || rivalRecord.TotalMatches != 1 {
			t.Errorf("expected rival recorded 1 loss, got %+v", rivalRecord)
		}
	})
}

// TestTracker_MidGameDisconnect_OpponentDisconnectAndReconnect asserts that
// opposing players who disconnect mid-game are preserved in the Opponents slice
// with stats intact, marked IsDisconnected=true, and restored cleanly upon return.
func TestTracker_MidGameDisconnect_OpponentDisconnectAndReconnect(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "m-opp-disconnect-1"
		playlistID := 11

		// Frame 1: Full lobby
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100, 0, 0, 1, 1, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 200, 1, 0, 1, 2, 0),
			makePlayerWithStats("Opponent2", "Steam|opp_2|0", 1, 250, 1, 1, 0, 3, 1),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1)

		// Frame 2: Opponent2 disconnects
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 120, 0, 0, 1, 2, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 220, 1, 0, 2, 2, 0),
			// Opponent2 omitted
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2)

		snapF2 := tracker.GetCurrentMatch()
		if len(snapF2.Opponents) != 2 {
			t.Fatalf("expected 2 opponents retained, got %d", len(snapF2.Opponents))
		}

		var opp1, opp2 *playertrack.LobbyPlayer
		for i := range snapF2.Opponents {
			if snapF2.Opponents[i].PlayerID == "Steam|opp_1|0" {
				opp1 = &snapF2.Opponents[i]
			} else if snapF2.Opponents[i].PlayerID == "Steam|opp_2|0" {
				opp2 = &snapF2.Opponents[i]
			}
		}
		if opp1 == nil || opp2 == nil {
			t.Fatalf("expected both opponents present in snapshot")
		}
		if opp1.IsDisconnected {
			t.Errorf("opp1 should not be disconnected")
		}
		if !opp2.IsDisconnected {
			t.Errorf("opp2 must be marked IsDisconnected=true")
		}
		if opp2.Stats.Score != 250 || opp2.Stats.Goals != 1 || opp2.Stats.Demos != 1 {
			t.Errorf("opp2 stats were lost: %+v", opp2.Stats)
		}

		// Frame 3: Opponent2 reconnects with updated stats
		playersF3 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 150, 0, 0, 2, 2, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 250, 1, 0, 2, 3, 0),
			makePlayerWithStats("Opponent2", "Steam|opp_2|0", 1, 380, 2, 1, 1, 5, 2),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF3)

		snapF3 := tracker.GetCurrentMatch()
		if len(snapF3.Opponents) != 2 {
			t.Fatalf("expected 2 opponents after reconnect (no duplicates), got %d", len(snapF3.Opponents))
		}
		for _, o := range snapF3.Opponents {
			if o.PlayerID == "Steam|opp_2|0" {
				if o.IsDisconnected {
					t.Errorf("opp2 should have IsDisconnected=false after reconnect")
				}
				if o.Stats.Score != 380 || o.Stats.Goals != 2 || o.Stats.Demos != 2 {
					t.Errorf("opp2 stats not updated: %+v", o.Stats)
				}
			}
		}
	})
}

// TestTracker_MidGameDisconnect_MultipleSimultaneous verifies that simultaneous
// drops across both teams retain all disconnected players correctly.
func TestTracker_MidGameDisconnect_MultipleSimultaneous(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "m-multi-drop-1"
		playlistID := 13 // 3v3 Standard

		// Frame 1: 3v3 full lobby (6 players)
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 200, 1, 0, 1, 2, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 150, 0, 1, 1, 1, 0),
			makePlayerWithStats("Tm2", "Steam|tm_2|0", 0, 180, 1, 0, 0, 2, 1),
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 220, 1, 0, 1, 3, 0),
			makePlayerWithStats("Opp2", "Steam|opp_2|0", 1, 140, 0, 1, 1, 1, 0),
			makePlayerWithStats("Opp3", "Steam|opp_3|0", 1, 160, 0, 0, 2, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1)

		// Frame 2: Both Tm2 and Opp3 disconnect simultaneously
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 220, 1, 0, 1, 3, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 160, 0, 1, 1, 2, 0),
			// Tm2 omitted!
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 240, 1, 0, 1, 3, 0),
			makePlayerWithStats("Opp2", "Steam|opp_2|0", 1, 150, 0, 1, 1, 1, 0),
			// Opp3 omitted!
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2)

		snapF2 := tracker.GetCurrentMatch()
		if len(snapF2.Teammates) != 2 {
			t.Fatalf("expected 2 teammates retained, got %d", len(snapF2.Teammates))
		}
		if len(snapF2.Opponents) != 3 {
			t.Fatalf("expected 3 opponents retained, got %d", len(snapF2.Opponents))
		}

		for _, tm := range snapF2.Teammates {
			if tm.PlayerID == "Steam|tm_2|0" {
				if !tm.IsDisconnected {
					t.Errorf("Tm2 must be marked IsDisconnected=true")
				}
				if tm.Stats.Score != 180 || tm.Stats.Goals != 1 {
					t.Errorf("Tm2 stats lost: %+v", tm.Stats)
				}
			}
		}

		for _, opp := range snapF2.Opponents {
			if opp.PlayerID == "Steam|opp_3|0" {
				if !opp.IsDisconnected {
					t.Errorf("Opp3 must be marked IsDisconnected=true")
				}
				if opp.Stats.Score != 160 || opp.Stats.Saves != 2 {
					t.Errorf("Opp3 stats lost: %+v", opp.Stats)
				}
			}
		}
	})
}

// TestTracker_MidGameDisconnect_MatchTransition_ResetsOldDisconnected ensures that
// disconnected players from match N do NOT leak into match N+1.
func TestTracker_MidGameDisconnect_MatchTransition_ResetsOldDisconnected(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		// Match 1: Player drops and is retained
		playersM1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100, 1, 0, 0, 1, 0),
			makePlayerWithStats("OldTeammate", "Steam|old_tm|0", 0, 200, 1, 0, 1, 2, 0),
			makePlayerWithStats("Rival", "Steam|rival|0", 1, 150, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, "guid-match-1", 11, playersM1)

		playersM1Drop := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 120, 1, 0, 0, 2, 0),
			makePlayerWithStats("Rival", "Steam|rival|0", 1, 150, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, "guid-match-1", 11, playersM1Drop)

		snapM1 := tracker.GetCurrentMatch()
		if len(snapM1.Teammates) != 1 || !snapM1.Teammates[0].IsDisconnected {
			t.Fatalf("expected OldTeammate retained as disconnected in match 1")
		}

		// Match 2: Completely new match GUID with new players
		playersM2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 50, 0, 0, 0, 1, 0),
			makePlayerWithStats("NewTeammate", "Steam|new_tm|0", 0, 80, 0, 1, 0, 1, 0),
			makePlayerWithStats("NewRival", "Steam|new_rival|0", 1, 90, 1, 0, 0, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, "guid-match-2", 11, playersM2)

		snapM2 := tracker.GetCurrentMatch()
		if snapM2.MatchGUID != "guid-match-2" {
			t.Errorf("expected MatchGUID 'guid-match-2', got %q", snapM2.MatchGUID)
		}
		if len(snapM2.Teammates) != 1 {
			t.Fatalf("expected exactly 1 teammate in match 2, got %d", len(snapM2.Teammates))
		}
		if snapM2.Teammates[0].PlayerID != "Steam|new_tm|0" {
			t.Errorf("expected NewTeammate in match 2, got %q", snapM2.Teammates[0].PlayerID)
		}
		// Confirm OldTeammate is nowhere in match 2
		for _, tm := range snapM2.Teammates {
			if tm.PlayerID == "Steam|old_tm|0" {
				t.Errorf("old disconnected teammate leaked into match 2!")
			}
		}
	})
}

// TestTracker_MidGameDisconnect_BotReplacement verifies casual match bot backfill:
// When a human player disconnects, they are retained as disconnected, while an AI
// bot joining in their place is added as an active participant.
func TestTracker_MidGameDisconnect_BotReplacement(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "m-bot-replace-1"
		playlistID := 1 // Casual 3v3

		// Frame 1: Human teammate active
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100, 1, 0, 0, 1, 0),
			makePlayerWithStats("HumanTm", "Steam|human_tm|0", 0, 180, 1, 1, 0, 2, 1),
			makePlayerWithStats("Opponent", "Steam|opp|0", 1, 150, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1)

		// Frame 2: HumanTm disconnects, AI bot "Tex" replaces them on Team 0
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 120, 1, 0, 1, 2, 0),
			makePlayerWithStats("Tex", "Unknown|0|0", 0, 20, 0, 0, 0, 1, 0), // AI Bot
			makePlayerWithStats("Opponent", "Steam|opp|0", 1, 160, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2)

		snapF2 := tracker.GetCurrentMatch()
		if len(snapF2.Teammates) != 2 {
			t.Fatalf("expected 2 teammates (retained human + bot), got %d", len(snapF2.Teammates))
		}

		var humanFound, botFound bool
		for _, tm := range snapF2.Teammates {
			if tm.PlayerID == "Steam|human_tm|0" {
				humanFound = true
				if !tm.IsDisconnected {
					t.Errorf("expected human teammate to be marked IsDisconnected=true")
				}
				if tm.Stats.Score != 180 || tm.Stats.Goals != 1 {
					t.Errorf("human teammate stats were modified: %+v", tm.Stats)
				}
			}
			if tm.IsBot {
				botFound = true
				if tm.IsDisconnected {
					t.Errorf("AI bot should be active, not disconnected")
				}
				if tm.Name != "Tex" {
					t.Errorf("expected bot name 'Tex', got %q", tm.Name)
				}
			}
		}
		if !humanFound {
			t.Errorf("retained human teammate not found in teammates slice")
		}
		if !botFound {
			t.Errorf("AI bot teammate not found in teammates slice")
		}
	})
}
```

---

### 4.3 Test Suite Code for `internal/session/session_test.go`

```go
// ============================================================================
// Test Suite: Mid-Game Disconnect Integration & Snapshots (Requirement R2)
// ============================================================================

// TestSessionTracker_MidGameDisconnect_ActiveMatchObserverPropagation asserts that
// when a player disconnects in playertrack.Tracker, the active match state retained
// in SessionTracker.GetSessionSummary().ActiveMatch preserves the player, their stats,
// and sets IsDisconnected=true across all 4 lifecycle frames.
func TestSessionTracker_MidGameDisconnect_ActiveMatchObserverPropagation(t *testing.T) {
	s := NewSessionTracker()

	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(tempDir + "/test_session_disc.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	cfg := config.PlayerTrackingConfig{
		Enabled:       true,
		LocalPlayerID: "Steam|local_session_user|0",
	}

	pt, err := playertrack.NewTracker(
		store,
		playertrack.NewNoOpRankClient(),
		cfg,
		config.AuthConfig{},
		playertrack.WithMatchStateListener(s),
	)
	if err != nil {
		t.Fatalf("failed to create playertrack.Tracker: %v", err)
	}
	defer pt.Close()

	ctx := context.Background()
	matchGUID := "session-disc-guid-1"
	playlistID := 11

	// Frame 1: Full lobby with active players accumulating stats
	playersF1 := []statsapi.StatsPlayer{
		{
			Name:      "LocalUser",
			PrimaryId: "Steam|local_session_user|0",
			TeamNum:   0,
			Score:     200,
			Goals:     1,
			Assists:   0,
			Saves:     1,
			Shots:     3,
			Demos:     0,
		},
		{
			Name:      "Teammate1",
			PrimaryId: "Steam|session_tm_1|0",
			TeamNum:   0,
			Score:     350,
			Goals:     2,
			Assists:   1,
			Saves:     2,
			Shots:     4,
			Demos:     1,
		},
		{
			Name:      "Opponent1",
			PrimaryId: "Steam|session_opp_1|0",
			TeamNum:   1,
			Score:     180,
			Goals:     1,
			Assists:   0,
			Saves:     1,
			Shots:     2,
			Demos:     0,
		},
	}

	if err := pt.OnUpdateState(ctx, matchGUID, playlistID, playersF1); err != nil {
		t.Fatalf("Frame 1 OnUpdateState failed: %v", err)
	}

	// Verify SessionTracker ActiveMatch in Frame 1
	summaryF1 := s.GetSessionSummary()
	if summaryF1.ActiveMatch == nil {
		t.Fatal("Frame 1: expected SessionTracker to hold ActiveMatch")
	}
	if len(summaryF1.ActiveMatch.Teammates) != 1 {
		t.Fatalf("Frame 1: expected 1 teammate in session active match, got %d", len(summaryF1.ActiveMatch.Teammates))
	}
	if summaryF1.ActiveMatch.Teammates[0].IsDisconnected {
		t.Errorf("Frame 1: teammate should not be disconnected")
	}

	// Frame 2: Teammate leaves early (omitted from UpdateState)
	playersF2 := []statsapi.StatsPlayer{
		{
			Name:      "LocalUser",
			PrimaryId: "Steam|local_session_user|0",
			TeamNum:   0,
			Score:     240,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     4,
			Demos:     0,
		},
		// Teammate1 omitted!
		{
			Name:      "Opponent1",
			PrimaryId: "Steam|session_opp_1|0",
			TeamNum:   1,
			Score:     200,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     3,
			Demos:     0,
		},
	}

	if err := pt.OnUpdateState(ctx, matchGUID, playlistID, playersF2); err != nil {
		t.Fatalf("Frame 2 OnUpdateState failed: %v", err)
	}

	// ASSERTION: SessionTracker.GetSessionSummary().ActiveMatch retains disconnected player and stats
	summaryF2 := s.GetSessionSummary()
	if summaryF2.ActiveMatch == nil {
		t.Fatal("Frame 2: expected SessionTracker to retain ActiveMatch")
	}
	if len(summaryF2.ActiveMatch.Teammates) != 1 {
		t.Fatalf("Frame 2: expected 1 retained teammate in session active match, got %d", len(summaryF2.ActiveMatch.Teammates))
	}
	tmF2 := summaryF2.ActiveMatch.Teammates[0]
	if tmF2.PlayerID != "Steam|session_tm_1|0" {
		t.Errorf("Frame 2: expected teammate ID 'Steam|session_tm_1|0', got %q", tmF2.PlayerID)
	}
	if !tmF2.IsDisconnected {
		t.Errorf("Frame 2: expected IsDisconnected=true in session active match for omitted teammate")
	}
	if tmF2.Stats.Score != 350 || tmF2.Stats.Goals != 2 || tmF2.Stats.Assists != 1 || tmF2.Stats.Demos != 1 {
		t.Errorf("Frame 2: session active match teammate stats were not preserved: %+v", tmF2.Stats)
	}

	// Frame 3: Disconnected teammate reconnects -> stats update, no duplicates
	playersF3 := []statsapi.StatsPlayer{
		{
			Name:      "LocalUser",
			PrimaryId: "Steam|local_session_user|0",
			TeamNum:   0,
			Score:     240,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     4,
			Demos:     0,
		},
		{
			Name:      "Teammate1",
			PrimaryId: "Steam|session_tm_1|0",
			TeamNum:   0,
			Score:     480,
			Goals:     3,
			Assists:   1,
			Saves:     3,
			Shots:     6,
			Demos:     1,
		},
		{
			Name:      "Opponent1",
			PrimaryId: "Steam|session_opp_1|0",
			TeamNum:   1,
			Score:     210,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     3,
			Demos:     0,
		},
	}

	if err := pt.OnUpdateState(ctx, matchGUID, playlistID, playersF3); err != nil {
		t.Fatalf("Frame 3 OnUpdateState failed: %v", err)
	}

	summaryF3 := s.GetSessionSummary()
	if len(summaryF3.ActiveMatch.Teammates) != 1 {
		t.Fatalf("Frame 3: expected exactly 1 teammate in session active match after reconnect, got %d", len(summaryF3.ActiveMatch.Teammates))
	}
	tmF3 := summaryF3.ActiveMatch.Teammates[0]
	if tmF3.IsDisconnected {
		t.Errorf("Frame 3: expected IsDisconnected=false after reconnection")
	}
	if tmF3.Stats.Score != 480 || tmF3.Stats.Goals != 3 {
		t.Errorf("Frame 3: teammate stats not updated after reconnect: %+v", tmF3.Stats)
	}

	// Frame 4: Local player leaves early -> local player and local team preserved
	playersF4 := []statsapi.StatsPlayer{
		// LocalUser omitted!
		{
			Name:      "Teammate1",
			PrimaryId: "Steam|session_tm_1|0",
			TeamNum:   0,
			Score:     480,
			Goals:     3,
			Assists:   1,
			Saves:     3,
			Shots:     6,
			Demos:     1,
		},
		{
			Name:      "Opponent1",
			PrimaryId: "Steam|session_opp_1|0",
			TeamNum:   1,
			Score:     210,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     3,
			Demos:     0,
		},
	}

	if err := pt.OnUpdateState(ctx, matchGUID, playlistID, playersF4); err != nil {
		t.Fatalf("Frame 4 OnUpdateState failed: %v", err)
	}

	summaryF4 := s.GetSessionSummary()
	if summaryF4.ActiveMatch.LocalPlayer == nil {
		t.Fatal("Frame 4: expected LocalPlayer to be retained in session active match, got nil")
	}
	if summaryF4.ActiveMatch.LocalPlayer.PlayerID != "Steam|local_session_user|0" {
		t.Errorf("Frame 4: expected LocalPlayer 'Steam|local_session_user|0', got %q", summaryF4.ActiveMatch.LocalPlayer.PlayerID)
	}
	if !summaryF4.ActiveMatch.LocalPlayer.IsDisconnected {
		t.Errorf("Frame 4: expected LocalPlayer.IsDisconnected=true in session active match")
	}
	if summaryF4.ActiveMatch.LocalTeam == nil || *summaryF4.ActiveMatch.LocalTeam != 0 {
		t.Errorf("Frame 4: expected LocalTeam preserved as 0, got %v", summaryF4.ActiveMatch.LocalTeam)
	}
}

// TestSessionTracker_MidGameDisconnect_SSEBroadcast verifies that the Server-Sent
// Events broadcaster pushes an EventMatchUpdate carrying the retained disconnected player.
func TestSessionTracker_MidGameDisconnect_SSEBroadcast(t *testing.T) {
	s := NewSessionTracker()

	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(tempDir + "/test_session_sse.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	cfg := config.PlayerTrackingConfig{
		Enabled:       true,
		LocalPlayerID: "Steam|local_user|0",
	}

	pt, err := playertrack.NewTracker(
		store,
		playertrack.NewNoOpRankClient(),
		cfg,
		config.AuthConfig{},
		playertrack.WithMatchStateListener(s),
	)
	if err != nil {
		t.Fatalf("failed to create playertrack.Tracker: %v", err)
	}
	defer pt.Close()

	ch, unsubscribe := s.Subscribe()
	defer unsubscribe()

	ctx := context.Background()
	matchGUID := "session-sse-guid-1"

	// Frame 1: Full lobby
	playersF1 := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: "Steam|local_user|0", TeamNum: 0, Score: 100, Goals: 1},
		{Name: "Teammate1", PrimaryId: "Steam|tm_1|0", TeamNum: 0, Score: 200, Goals: 1},
	}
	_ = pt.OnUpdateState(ctx, matchGUID, 11, playersF1)

	// Consume Frame 1 SSE event
	select {
	case event := <-ch:
		if event.Event != EventMatchUpdate {
			t.Errorf("expected EventMatchUpdate, got %s", event.Event)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Frame 1 SSE event")
	}

	// Frame 2: Teammate disconnects
	playersF2 := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: "Steam|local_user|0", TeamNum: 0, Score: 120, Goals: 1},
	}
	_ = pt.OnUpdateState(ctx, matchGUID, 11, playersF2)

	// Consume Frame 2 SSE event and verify payload
	select {
	case event := <-ch:
		if event.Event != EventMatchUpdate {
			t.Errorf("expected EventMatchUpdate, got %s", event.Event)
		}
		matchPayload, ok := event.Data.(*playertrack.CurrentMatchResponse)
		if !ok {
			t.Fatalf("expected event data to be *playertrack.CurrentMatchResponse, got %T", event.Data)
		}
		if len(matchPayload.Teammates) != 1 {
			t.Fatalf("expected 1 teammate in SSE payload, got %d", len(matchPayload.Teammates))
		}
		if !matchPayload.Teammates[0].IsDisconnected {
			t.Errorf("expected SSE payload teammate to have IsDisconnected=true")
		}
		if matchPayload.Teammates[0].Stats.Score != 200 || matchPayload.Teammates[0].Stats.Goals != 1 {
			t.Errorf("expected SSE payload teammate stats preserved, got %+v", matchPayload.Teammates[0].Stats)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Frame 2 SSE event")
	}
}

// TestSessionTracker_MidGameDisconnect_ConcludeMatchSnapshotPreservation verifies that
// when a match finishes with a disconnected participant:
// 1. ConcludeMatch preserves the disconnected player in SessionMatchDetail.Players.
// 2. The player's IsDisconnected flag and accumulated stats are preserved.
// 3. Team goal tallies (BlueScore/OrangeScore) aggregate goals from the disconnected player.
func TestSessionTracker_MidGameDisconnect_ConcludeMatchSnapshotPreservation(t *testing.T) {
	s := NewSessionTracker()

	localTeam := 0
	winnerTeam := 0

	// Mock match snapshot where teammate disconnected after scoring 2 goals
	match := &playertrack.CurrentMatchResponse{
		ActiveMatch:  false,
		MatchEnded:   true,
		MatchGUID:    "conclude-disc-1",
		PlaylistID:   11,
		PlaylistName: "Ranked Doubles",
		LocalTeam:    &localTeam,
		WinnerTeam:   &winnerTeam,
		Result:       "victory",
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID: "Steam|local_hero|0",
			Name:     "LocalHero",
			TeamNum:  0,
			IsLocal:  true,
			Stats: playertrack.PlayerStatsSummary{
				Score: 150,
				Goals: 1, // 1 goal by local
			},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|leaver_tm|0",
				Name:           "LeaverTm",
				TeamNum:        0,
				IsLocal:        false,
				IsDisconnected: true, // Left early
				Stats: playertrack.PlayerStatsSummary{
					Score: 250,
					Goals: 2, // 2 goals scored before leaving
				},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|rival|0",
				Name:     "Rival",
				TeamNum:  1,
				Stats: playertrack.PlayerStatsSummary{
					Score: 100,
					Goals: 1,
				},
			},
		},
	}

	// First set match as active so ConcludeMatch can transition cleanly
	activeMatch := match.DeepClone()
	activeMatch.ActiveMatch = true
	activeMatch.MatchEnded = false
	s.RecordActiveMatch(activeMatch)

	// Conclude match
	s.ConcludeMatch(match)

	summary := s.GetSessionSummary()
	if summary.TotalMatches != 1 || summary.TotalWins != 1 {
		t.Fatalf("expected 1 match and 1 win, got %d matches, %d wins", summary.TotalMatches, summary.TotalWins)
	}
	if len(summary.Matches) != 1 {
		t.Fatalf("expected 1 match detail in history, got %d", len(summary.Matches))
	}

	detail := summary.Matches[0]
	// Blue team scored 1 (Local) + 2 (LeaverTm) = 3 total goals
	if detail.BlueScore != 3 {
		t.Errorf("expected BlueScore=3 (including disconnected player's 2 goals), got %d", detail.BlueScore)
	}
	if detail.OrangeScore != 1 {
		t.Errorf("expected OrangeScore=1, got %d", detail.OrangeScore)
	}

	// Verify players roster contains disconnected teammate
	var leaverFound bool
	for _, p := range detail.Players {
		if p.PlayerID == "Steam|leaver_tm|0" {
			leaverFound = true
			if !p.IsDisconnected {
				t.Errorf("expected SessionMatchPlayer.IsDisconnected=true for early leaver")
			}
			if p.Stats.Goals != 2 || p.Stats.Score != 250 {
				t.Errorf("expected leaver stats preserved in match detail, got %+v", p.Stats)
			}
		}
	}
	if !leaverFound {
		t.Errorf("disconnected teammate not found in completed match detail players")
	}
}
```

---

## 5. Verification Method

### 5.1 Independent Verification Commands

1. **Verify Existing Tests Pass (Zero Regressions Baseline)**:
   ```powershell
   go test -v ./internal/playertrack/... ./internal/session/...
   ```
2. **Execute Mid-Game Disconnect Tests (after worker adds `IsDisconnected` and retention logic)**:
   ```powershell
   go test -v -run "TestTracker_MidGameDisconnect" ./internal/playertrack/...
   go test -v -run "TestSessionTracker_MidGameDisconnect" ./internal/session/...
   ```
3. **Verify Full Project Test Suite**:
   ```powershell
   go test ./...
   ```
4. **Verify Single Standalone Executable Build**:
   ```powershell
   go build ./cmd/rl-sync
   ```

### 5.2 Files to Inspect

- `internal/playertrack/tracker.go`: `LobbyPlayer` struct (lines 49-60), `OnUpdateState` retention (lines 314-490), `LocalTeam` fallback (lines 361-366), `OnMatchEnded` outcome compiling (lines 631-667).
- `internal/playertrack/tracker_test.go`: `Test Suite 4: Mid-Game Disconnect & Player State Retention`.
- `internal/session/models.go`: `SessionMatchPlayer` (lines 72-85).
- `internal/session/session.go`: `RecordActiveMatch` (lines 128-210), `ConcludeMatch` (lines 245-296).
- `internal/session/session_test.go`: `Test Suite: Mid-Game Disconnect Integration & Snapshots`.

### 5.3 Invalidation Conditions

- If Rocket League's local Stats API exporter introduces an explicit JSON event type for player disconnection instead of passive omission in `msg.Data.Players`, this passive differential retention logic would still correctly handle omissions, but could be augmented with explicit event handlers.
- If future modifications introduce in-place mutation of `t.currentMatch` without acquiring `t.mu.Lock()`, race detector or concurrent tests will fail.
