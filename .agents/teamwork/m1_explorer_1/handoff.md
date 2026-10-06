# Handoff Report: Milestone M1 (Requirement R2: Persistent Player State on Disconnect)

**Agent**: `m1_explorer_1`  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1`  
**Date**: 2026-10-06T08:55:00Z  
**Type**: Hard Handoff  
**Target Milestone**: M1 (Persistent Player State on Mid-Game Disconnect)  

---

## 1. Observation

### 1.1 Ingestion Dynamics of `MatchStatsExporter_TA`
Rocket League's local Stats API exporter (`MatchStatsExporter_TA`) communicates over WebSocket/TCP and emits two primary match lifecycle events received by `internal/statsapi/listener.go`:
1. `UpdateState` (`internal/statsapi/listener.go:292-329`):
   ```go
   292: 	case "UpdateState":
   293: 		guid := msg.Data.MatchGUID
   ...
   302: 		players := msg.Data.Players
   ...
   328: 		if err := h.OnUpdateState(ctx, guid, playlistID, players); err != nil {
   ```
   **Critical Behavioral Fact**: In `UpdateState`, `msg.Data.Players` (`[]statsapi.StatsPlayer`) contains **only the players currently connected to the server in that frame**. When a player quits to main menu, leaves the lobby, or loses connection, Rocket League does not emit a dedicated "PlayerDisconnected" event; instead, that player is silently absent from `msg.Data.Players` in all subsequent frames.

2. `MatchEnded` (`internal/statsapi/listener.go:331-355`):
   ```go
   331: 	case "MatchEnded":
   ...
   339: 		winner := msg.Data.WinnerTeamNum
   340: 		if err := h.OnMatchEnded(ctx, guid, winner); err != nil {
   ```
   **Critical Behavioral Fact**: The `MatchEnded` event carries **only** `WinnerTeamNum` (0 for Blue, 1 for Orange). It contains **no player rosters or box score statistics**. `Tracker.OnMatchEnded` depends 100% on the cached state stored in `t.currentMatch` compiled during preceding `UpdateState` events.

---

### 1.2 The Root Cause of Active Player & Stats Loss in `Tracker.OnUpdateState`
In `internal/playertrack/tracker.go:368-483`:
```go
368: 	var teammates []LobbyPlayer
369: 	var opponents []LobbyPlayer
370: 	var spectators []LobbyPlayer
371: 	var localPlayerLobby *LobbyPlayer
...
448: 		lp := LobbyPlayer{
449: 			PlayerID: p.PrimaryId,
450: 			Platform: platform,
451: 			Name:     p.Name,
452: 			TeamNum:  p.TeamNum,
453: 			IsLocal:  isLocal,
454: 			IsBot:    isBot,
455: 			Stats: PlayerStatsSummary{
456: 				Score:   p.Score,
457: 				Goals:   p.Goals,
458: 				Assists: p.Assists,
459: 				Saves:   p.Saves,
460: 				Shots:   p.Shots,
461: 				Demos:   p.Demos,
462: 			},
463: 			CurrentRank:   currentRank,
464: 			Ranks:         ranksSnapshot,
465: 			MatchupRecord: matchupRecord,
466: 		}
...
479: 	t.currentMatch.LocalPlayer = localPlayerLobby
480: 	t.currentMatch.Teammates = teammates
481: 	t.currentMatch.Opponents = opponents
482: 	t.currentMatch.Spectators = spectators
```
- **Direct Observation**: At line 368, `teammates`, `opponents`, and `spectators` are allocated as empty slices. The loop at lines 379-478 iterates strictly over the incoming `players` slice.
- At lines 479-482, `t.currentMatch.Teammates`, `t.currentMatch.Opponents`, and `t.currentMatch.Spectators` are overwritten directly by these newly populated slices.
- If a player was present in frame $N$ with 3 goals and 450 score, but leaves before frame $N+1$, frame $N+1$ allocates fresh empty slices, processes only the remaining players, and completely discards the departing player and all accumulated box score stats.
- Downstream propagation: `t.listener.OnActiveMatchUpdated(matchClone)` informs `SessionTracker.RecordActiveMatch` (`internal/session/session.go:142`), which clones the purged roster and broadcasts an SSE `EventMatchUpdate`. Web clients and `GET /current-match` instantly lose the player.

---

### 1.3 Missing Disconnect Flag in Models
In `internal/playertrack/tracker.go:49-60`:
```go
49: type LobbyPlayer struct {
50: 	PlayerID      string                 `json:"player_id"`                // PrimaryId (e.g. "Steam|76561198...|0")
51: 	Platform      string                 `json:"platform"`                 // "Steam", "Epic", "Unknown"
52: 	Name          string                 `json:"name"`                     // In-game display name
53: 	TeamNum       int                    `json:"team_num"`                 // 0 = Blue, 1 = Orange, 255 = Spectator
54: 	IsLocal       bool                   `json:"is_local"`                 // True if this is the authenticated user
55: 	IsBot         bool                   `json:"is_bot"`                   // True if AI bot ("Unknown|0|0")
56: 	Stats         PlayerStatsSummary     `json:"stats"`                    // Real-time in-game box score
57: 	CurrentRank   *PlayerPlaylistRank    `json:"current_rank,omitempty"`   // Rank for active match playlist
58: 	Ranks         PlayerRanksSnapshot    `json:"ranks,omitempty"`          // Full snapshot across all playlists
59: 	MatchupRecord *storage.PlayerMatchup `json:"matchup_record,omitempty"` // H2H record in this playlist
60: }
```
- **Direct Observation**: `LobbyPlayer` currently lacks any boolean indicator to signal whether a participant is actively connected or disconnected.
- Similarly, `internal/session/models.go:72-85` (`SessionMatchPlayer`) and `web/src/types/api.ts:82-93` lack `is_disconnected`.

---

### 1.4 Cascading Failure on Local Player Disconnect
In `internal/playertrack/tracker.go:321, 361-366, 608-619`:
```go
321: 	resolvedLocal, myTeamNum := t.resolveLocalPlayer(ctx, players)
...
361: 	if myTeamNum == 0 || myTeamNum == 1 {
362: 		val := myTeamNum
363: 		t.currentMatch.LocalTeam = &val
364: 	} else {
365: 		t.currentMatch.LocalTeam = nil
366: 	}
```
And in `OnMatchEnded`:
```go
608: 	if localPlayer == nil || localTeam == nil || (*localTeam != 0 && *localTeam != 1) {
609: 		matchClone := matchState.DeepClone()
610: 		listener := t.listener
611: 		t.mu.Unlock()
612: 		t.logger.Warn("match ended but local player not playing on a valid team; skipping matchup outcomes",
613: 			slog.String("match_guid", trimmedGUID),
614: 		)
...
618: 		return nil
619: 	}
```
- If the local player disconnects mid-game (or is omitted from a frame):
  1. `t.resolveLocalPlayer` returns `nil, -1`.
  2. Line 365 sets `t.currentMatch.LocalTeam = nil`.
  3. Lines 470-476 categorize ALL remaining players into `spectators` because `(myTeamNum == 0 || myTeamNum == 1)` evaluates to `false`!
  4. Line 479 sets `t.currentMatch.LocalPlayer = nil`.
  5. When `OnMatchEnded` fires, lines 608-619 hit `localPlayer == nil` and completely abort matchup outcome compilation for all players.

---

### 1.5 Verification of Existing Test Suite
- Executed `go test ./internal/playertrack/...`: 100% pass rate.
- Executed `go test ./...`: 100% pass rate across all 14 project packages.
- Grep search confirms zero existing unit tests simulate a mid-game player disconnect.

---

## 2. Logic Chain

1. **Premise**: `MatchStatsExporter_TA` sends only connected players in each `UpdateState` event. Disconnecting players are simply omitted (Observation 1.1).
2. **Root Cause 1**: `Tracker.OnUpdateState` operates as a stateless overwrite, wiping previous participant slices without retaining participants from earlier frames of the same match (Observation 1.2).
3. **Root Cause 2**: `resolveLocalPlayer` failure wipes `LocalTeam` and categorizes all remaining players as spectators, then aborts match outcome recording on match end (Observation 1.4).
4. **Differential Retention Formulation**:
   - Within the active `matchGUID`, `t.currentMatch` holds the accumulated roster state from preceding frames.
   - For an ongoing match (`t.currentMatch != nil && t.currentMatch.MatchGUID == trimmedGUID`), all previously recorded participants must be preserved across frames.
   - Any player absent from the incoming `players` slice must NOT be deleted. Instead, their previous record must be retained, flagged with `IsDisconnected = true`, and their accumulated box scores (`Score`, `Goals`, `Assists`, `Saves`, `Shots`, `Demos`) preserved at their last known values.
   - When a disconnected player reconnects, they reappear in `players`. The algorithm processes them as active (`IsDisconnected = false`), updates their stats to the latest values, and marks them in `seenThisFrame`. When inspecting the previous snapshot, the matching key in `seenThisFrame` prevents appending a duplicate row.
   - For the local player: if absent in a subsequent frame, `myTeamNum` and `LocalPlayer` must fall back to the previously resolved team and player (`retainedLocal.IsDisconnected = true`). This prevents the whole-match outcome compilation abortion (Observation 1.4).
   - Match GUID boundary: When a new match GUID arrives (`t.currentMatch.MatchGUID != trimmedGUID`), differential retention is bypassed and a fresh match state is initialized. Previous match participants are never retained into a new match.
   - AI bot exclusion: AI bots (`Unknown|0|0`) are temporary backfills in casual matches. To prevent bot churn from cluttering the roster with phantom disconnected bots, departed bots (`oldP.IsBot`) are excluded from retention.

---

## 3. Caveats

1. **Casual Match Bot Churn**: In casual matches, human players who disconnect are often replaced by AI bots (`Unknown|0|0`). When a bot leaves (e.g. because another human connects), retaining the departed bot would clutter the scoreboard with useless rows. Therefore, only human participants (`!oldP.IsBot`) are retained upon disconnect. Active bots currently in `players` are shown normally (`IsBot = true`, `IsDisconnected = false`).
2. **Casual Match Player Replacement / Team Size Expansion**: In casual playlists, new human players can join after a player leaves. The active roster may temporarily have more than the playlist's standard team limit (e.g., 3 players on a 2v2 team: 1 disconnected + 2 active). This is expected and mirrors official Rocket League replay files and ballchasing.com match records. The UI layout in Milestone M3 will format these cleanly.
3. **Splitscreen Player Identity**: Splitscreen players share account IDs but have distinct composite identifiers (e.g., `Steam|76561198000000001|0` vs `Steam|76561198000000001|1`). Case-normalized full `PrimaryId` must be used as the map key so splitscreen players are tracked and retained independently without collision.
4. **Mid-Game Team Swapping**: In private/casual matches, players may switch teams. The differential retention algorithm registers the player in their current frame's team first (`seenThisFrame[normID] = true`), causing the previous frame's opposing team check to skip them, preventing duplicate rows across teams.

---

## 4. Conclusion & Concrete Implementation Design

### 4.1 Data Model Updates

#### 1. `internal/playertrack/tracker.go:49-60`
Add `IsDisconnected bool json:"is_disconnected,omitempty"` to `LobbyPlayer`:
```go
// LobbyPlayer encapsulates identity, real-time match stats, skill ratings, and historical matchup records.
type LobbyPlayer struct {
	PlayerID       string                 `json:"player_id"`                // PrimaryId (e.g. "Steam|76561198...|0")
	Platform       string                 `json:"platform"`                 // "Steam", "Epic", "Unknown"
	Name           string                 `json:"name"`                     // In-game display name
	TeamNum        int                    `json:"team_num"`                 // 0 = Blue, 1 = Orange, 255 = Spectator
	IsLocal        bool                   `json:"is_local"`                 // True if this is the authenticated user
	IsBot          bool                   `json:"is_bot"`                   // True if AI bot ("Unknown|0|0")
	IsDisconnected bool                   `json:"is_disconnected,omitempty"`// True if player left the active game early
	Stats          PlayerStatsSummary     `json:"stats"`                    // Real-time in-game box score
	CurrentRank    *PlayerPlaylistRank    `json:"current_rank,omitempty"`   // Rank for active match playlist
	Ranks          PlayerRanksSnapshot    `json:"ranks,omitempty"`          // Full snapshot across all playlists
	MatchupRecord  *storage.PlayerMatchup `json:"matchup_record,omitempty"` // H2H record in this playlist
}
```

#### 2. `internal/session/models.go:72-85`
Add `IsDisconnected bool json:"is_disconnected,omitempty"` to `SessionMatchPlayer`:
```go
// SessionMatchPlayer captures an individual player's performance and credentials in a completed match.
type SessionMatchPlayer struct {
	PlayerID       string                         `json:"player_id"`
	Platform       string                         `json:"platform"`
	Name           string                         `json:"name"`
	TeamNum        int                            `json:"team_num"` // 0 = Blue, 1 = Orange, 255 = Spectator
	IsLocal        bool                           `json:"is_local"`
	IsBot          bool                           `json:"is_bot"`
	IsDisconnected bool                           `json:"is_disconnected,omitempty"`
	Stats          playertrack.PlayerStatsSummary `json:"stats"`
	RankName       string                         `json:"rank_name"`
	Tier           int                            `json:"tier"`
	Division       int                            `json:"division"`
	MMR            float64                        `json:"mmr"`
	MatchupRecord  *storage.PlayerMatchup         `json:"matchup_record,omitempty"`
}
```

#### 3. `internal/session/session.go:282-296`
In `ConcludeMatch`, map `lp.IsDisconnected` into `SessionMatchPlayer`:
```go
		matchPlayers = append(matchPlayers, SessionMatchPlayer{
			PlayerID:       lp.PlayerID,
			Platform:       lp.Platform,
			Name:           lp.Name,
			TeamNum:        lp.TeamNum,
			IsLocal:        lp.IsLocal,
			IsBot:          lp.IsBot,
			IsDisconnected: lp.IsDisconnected,
			Stats:          lp.Stats,
			RankName:       rankName,
			Tier:           tier,
			Division:       div,
			MMR:            mmr,
			MatchupRecord:  lp.MatchupRecord,
		})
```

#### 4. `web/src/types/api.ts`
Add `is_disconnected?: boolean;` to both `LobbyPlayer` (line 88) and `SessionMatchPlayer` (line 44):
```typescript
export interface LobbyPlayer {
  player_id: string;
  platform: string;
  name: string;
  team_num: number;
  is_local: boolean;
  is_bot: boolean;
  is_disconnected?: boolean;
  stats: PlayerStatsSummary;
  current_rank?: PlayerPlaylistRank;
  ranks?: PlayerRanksSnapshot;
  matchup_record?: PlayerMatchup;
}

export interface SessionMatchPlayer {
  player_id: string;
  platform: string;
  name: string;
  team_num: number; // 0 = Blue, 1 = Orange, 255 = Spectator
  is_local: boolean;
  is_bot: boolean;
  is_disconnected?: boolean;
  stats: PlayerStatsSummary;
  rank_name: string;
  tier: number;
  division: number;
  mmr: number;
  matchup_record?: PlayerMatchup;
}
```

---

### 4.2 Exact Differential Retention Algorithm for `Tracker.OnUpdateState`
In `internal/playertrack/tracker.go`, replace lines 324-483 with the following drop-in block:

```go
	// 2. Prepare state update under lock
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("tracker is closed")
	}

	now := time.Now().UTC()

	isSameMatch := (t.currentMatch != nil && t.currentMatch.MatchGUID == trimmedGUID)

	// Capture previous participant snapshot if within the same match
	var prevLocalPlayer *LobbyPlayer
	var prevTeammates []LobbyPlayer
	var prevOpponents []LobbyPlayer
	var prevSpectators []LobbyPlayer

	if !isSameMatch {
		// New match detected: initialize new snapshot
		t.currentMatch = &CurrentMatchResponse{
			ActiveMatch:  true,
			MatchEnded:   false,
			MatchGUID:    trimmedGUID,
			PlaylistID:   playlistID,
			PlaylistName: FormatPlaylist(playlistID),
			Teammates:    make([]LobbyPlayer, 0),
			Opponents:    make([]LobbyPlayer, 0),
			Spectators:   make([]LobbyPlayer, 0),
			UpdatedAt:    now,
		}
		// Reset frame upsert cache and in-flight matchup queries for new match
		t.upsertedProfiles = make(map[string]string)
		t.matchupInFlight = make(map[string]time.Time)
	} else {
		// Existing match: capture previous rosters for differential retention
		prevLocalPlayer = t.currentMatch.LocalPlayer
		prevTeammates = t.currentMatch.Teammates
		prevOpponents = t.currentMatch.Opponents
		prevSpectators = t.currentMatch.Spectators

		// Fallback: if local player left/omitted in this frame, retain previously resolved local team
		if resolvedLocal == nil && t.currentMatch.LocalTeam != nil && (*t.currentMatch.LocalTeam == 0 || *t.currentMatch.LocalTeam == 1) {
			myTeamNum = *t.currentMatch.LocalTeam
		}

		// Update existing match metadata
		if playlistID != 0 {
			t.currentMatch.PlaylistID = playlistID
			t.currentMatch.PlaylistName = FormatPlaylist(playlistID)
		}
		t.currentMatch.UpdatedAt = now
		// If match was already marked ended, keep ActiveMatch=false and MatchEnded=true
		if !t.currentMatch.MatchEnded {
			t.currentMatch.ActiveMatch = true
		}
	}

	if myTeamNum == 0 || myTeamNum == 1 {
		val := myTeamNum
		t.currentMatch.LocalTeam = &val
	} else if !isSameMatch {
		t.currentMatch.LocalTeam = nil
	}

	var teammates []LobbyPlayer
	var opponents []LobbyPlayer
	var spectators []LobbyPlayer
	var localPlayerLobby *LobbyPlayer

	// Track observed player IDs in current frame to detect departures and deduplicate
	seenThisFrame := make(map[string]bool, len(players))

	var playersToUpsert []statsapi.StatsPlayer
	var playersToFetchRank []rlapi.PlayerID
	var playersToQueryMatchup []statsapi.StatsPlayer

	activePlaylistID := t.currentMatch.PlaylistID

	for _, p := range players {
		isBot := p.IsBot()
		normID := strings.ToLower(strings.TrimSpace(p.PrimaryId))

		// Deduplicate human players within the same frame
		if !isBot && normID != "" {
			if seenThisFrame[normID] {
				continue
			}
			seenThisFrame[normID] = true
		}

		isLocal := resolvedLocal != nil && strings.EqualFold(strings.TrimSpace(p.PrimaryId), strings.TrimSpace(resolvedLocal.PrimaryID))

		// Profile upsert check for non-bot human players
		if !isBot && strings.TrimSpace(p.PrimaryId) != "" {
			if lastSeenName, exists := t.upsertedProfiles[p.PrimaryId]; !exists || lastSeenName != p.Name {
				playersToUpsert = append(playersToUpsert, p)
				t.upsertedProfiles[p.PrimaryId] = p.Name
			}
		}

		// Asynchronous rank query check
		if !isBot && strings.TrimSpace(p.PrimaryId) != "" && t.cfg.AutoFetchRanks && t.rankClient != nil && t.rankClient.IsEnabled() {
			pid := p.PrimaryId
			shouldFetch := true

			// Tier 1: Cache check
			if entry, ok := t.rankCache[pid]; ok {
				if time.Since(entry.fetchedAt) < t.rankCacheTTL {
					shouldFetch = false
				}
			}
			// Tier 2: In-flight check
			if _, inFlight := t.inFlight[pid]; inFlight {
				shouldFetch = false
			}
			// Tier 3: Failure backoff check
			if failTime, failed := t.failBackoff[pid]; failed {
				if time.Since(failTime) < t.backoffTTL {
					shouldFetch = false
				}
			}

			if shouldFetch {
				t.inFlight[pid] = now
				playersToFetchRank = append(playersToFetchRank, rlapi.PlayerID(pid))
			}
		}

		// Parse platform
		parsed, _ := p.ParseID()
		platform := parsed.Platform

		// Extract cached ranks if available
		var ranksSnapshot PlayerRanksSnapshot
		var currentRank *PlayerPlaylistRank
		if entry, ok := t.rankCache[p.PrimaryId]; ok {
			ranksSnapshot = entry.snapshot
			if r, found := ranksSnapshot.GetRank(activePlaylistID); found {
				currentRank = &r
			}
		}

		// Extract cached matchup record if available
		var matchupRecord *storage.PlayerMatchup
		if activePlaylistID != 0 && !isBot {
			mKey := fmt.Sprintf("%s:%d", p.PrimaryId, activePlaylistID)
			if m, ok := t.matchupCache[mKey]; ok {
				matchupRecord = m
			} else if !isLocal {
				// Queue for matchup history lookup if not already in-flight
				if _, inFlight := t.matchupInFlight[mKey]; !inFlight {
					t.matchupInFlight[mKey] = now
					playersToQueryMatchup = append(playersToQueryMatchup, p)
				}
			}
		}

		lp := LobbyPlayer{
			PlayerID:       p.PrimaryId,
			Platform:       platform,
			Name:           p.Name,
			TeamNum:        p.TeamNum,
			IsLocal:        isLocal,
			IsBot:          isBot,
			IsDisconnected: false,
			Stats: PlayerStatsSummary{
				Score:   p.Score,
				Goals:   p.Goals,
				Assists: p.Assists,
				Saves:   p.Saves,
				Shots:   p.Shots,
				Demos:   p.Demos,
			},
			CurrentRank:   currentRank,
			Ranks:         ranksSnapshot,
			MatchupRecord: matchupRecord,
		}

		if isLocal {
			localPlayerLobby = &lp
		} else if (myTeamNum == 0 || myTeamNum == 1) && p.TeamNum == myTeamNum {
			teammates = append(teammates, lp)
		} else if (myTeamNum == 0 || myTeamNum == 1) && (p.TeamNum == 0 || p.TeamNum == 1) {
			opponents = append(opponents, lp)
		} else {
			spectators = append(spectators, lp)
		}
	}

	// Differential Retention: Retain human participants from previous frame who departed mid-game
	if isSameMatch {
		// Retain disconnected local player
		if localPlayerLobby == nil && prevLocalPlayer != nil {
			retainedLocal := prevLocalPlayer.DeepClone()
			retainedLocal.IsDisconnected = true
			localPlayerLobby = &retainedLocal
			if normID := strings.ToLower(strings.TrimSpace(retainedLocal.PlayerID)); normID != "" {
				seenThisFrame[normID] = true
			}
		}

		// Retain disconnected teammates
		for _, oldP := range prevTeammates {
			if oldP.IsBot {
				continue // Do not retain departed AI bots
			}
			normID := strings.ToLower(strings.TrimSpace(oldP.PlayerID))
			if normID != "" && !seenThisFrame[normID] {
				retained := oldP.DeepClone()
				retained.IsDisconnected = true
				teammates = append(teammates, retained)
				seenThisFrame[normID] = true
			}
		}

		// Retain disconnected opponents
		for _, oldP := range prevOpponents {
			if oldP.IsBot {
				continue // Do not retain departed AI bots
			}
			normID := strings.ToLower(strings.TrimSpace(oldP.PlayerID))
			if normID != "" && !seenThisFrame[normID] {
				retained := oldP.DeepClone()
				retained.IsDisconnected = true
				opponents = append(opponents, retained)
				seenThisFrame[normID] = true
			}
		}

		// Retain disconnected spectators
		for _, oldP := range prevSpectators {
			if oldP.IsBot {
				continue
			}
			normID := strings.ToLower(strings.TrimSpace(oldP.PlayerID))
			if normID != "" && !seenThisFrame[normID] {
				retained := oldP.DeepClone()
				retained.IsDisconnected = true
				spectators = append(spectators, retained)
				seenThisFrame[normID] = true
			}
		}
	}

	t.currentMatch.LocalPlayer = localPlayerLobby
	t.currentMatch.Teammates = teammates
	t.currentMatch.Opponents = opponents
	t.currentMatch.Spectators = spectators
```

---

### 4.3 Programmatic Unit Tests Design for Implementer

Add a dedicated test suite `TestTracker_MidGameDisconnect` in `internal/playertrack/tracker_test.go`:

```go
func TestTracker_MidGameDisconnect(t *testing.T) {
	ctx := context.Background()

	t.Run("Teammate Disconnect Retains Stats and Logs Win", func(t *testing.T) {
		store := newTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		guid := "match-dc-teammate"
		playlistID := 11

		// Frame 1: All participants connected
		playersF1 := []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: 100, Goals: 1},
			{Name: "Teammate1", PrimaryId: "Steam|tm1|0", TeamNum: 0, Score: 250, Goals: 2, Assists: 1, Saves: 1, Shots: 4, Demos: 1},
			{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1, Score: 120, Goals: 0},
		}
		if err := tracker.OnUpdateState(ctx, guid, playlistID, playersF1); err != nil {
			t.Fatalf("Frame 1 OnUpdateState failed: %v", err)
		}

		// Assert Frame 1
		m1 := tracker.GetCurrentMatch()
		if len(m1.Teammates) != 1 || m1.Teammates[0].IsDisconnected {
			t.Fatalf("Frame 1: expected 1 active teammate, got %+v", m1.Teammates)
		}

		// Frame 2: Teammate1 leaves mid-game (omitted from players)
		playersF2 := []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: 150, Goals: 1},
			{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1, Score: 180, Goals: 1},
		}
		if err := tracker.OnUpdateState(ctx, guid, playlistID, playersF2); err != nil {
			t.Fatalf("Frame 2 OnUpdateState failed: %v", err)
		}

		// Assert Frame 2: Teammate1 retained with stats preserved and IsDisconnected=true
		m2 := tracker.GetCurrentMatch()
		if len(m2.Teammates) != 1 {
			t.Fatalf("Frame 2: expected 1 retained teammate, got %d", len(m2.Teammates))
		}
		tm := m2.Teammates[0]
		if tm.PlayerID != "Steam|tm1|0" {
			t.Errorf("expected retained player Steam|tm1|0, got %s", tm.PlayerID)
		}
		if !tm.IsDisconnected {
			t.Errorf("expected IsDisconnected=true for departed teammate")
		}
		if tm.Stats.Score != 250 || tm.Stats.Goals != 2 || tm.Stats.Assists != 1 || tm.Stats.Saves != 1 || tm.Stats.Shots != 4 || tm.Stats.Demos != 1 {
			t.Errorf("Stats not preserved: got %+v", tm.Stats)
		}

		// Conclude match: Local team won (WinnerTeamNum = 0)
		win := 0
		if err := tracker.OnMatchEnded(ctx, guid, &win); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		// Verify store records WinAsTeammate for disconnected teammate
		matchup, err := store.GetPlayerMatchup(ctx, "Steam|tm1|0", playlistID)
		if err != nil {
			t.Fatalf("GetPlayerMatchup failed: %v", err)
		}
		if matchup.WinsAsTeammate != 1 || matchup.LossesAsTeammate != 0 || matchup.TotalMatches != 1 {
			t.Errorf("Matchup outcome incorrect for disconnected teammate: %+v", matchup)
		}
	})

	t.Run("Reconnection Restores Active Status Without Duplicates", func(t *testing.T) {
		store := newTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		guid := "match-dc-reconnect"
		playlistID := 11

		// Frame 1: Connected
		tracker.OnUpdateState(ctx, guid, playlistID, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: 50},
			{Name: "Teammate1", PrimaryId: "Steam|tm1|0", TeamNum: 0, Score: 100, Goals: 1},
		})

		// Frame 2: Disconnected
		tracker.OnUpdateState(ctx, guid, playlistID, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: 80},
		})
		m2 := tracker.GetCurrentMatch()
		if len(m2.Teammates) != 1 || !m2.Teammates[0].IsDisconnected {
			t.Fatalf("Frame 2: teammate should be retained as disconnected")
		}

		// Frame 3: Reconnected with increased stats
		tracker.OnUpdateState(ctx, guid, playlistID, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: 120},
			{Name: "Teammate1", PrimaryId: "Steam|tm1|0", TeamNum: 0, Score: 300, Goals: 2},
		})

		m3 := tracker.GetCurrentMatch()
		if len(m3.Teammates) != 1 {
			t.Fatalf("Frame 3: expected exactly 1 teammate (no duplicate rows), got %d", len(m3.Teammates))
		}
		if m3.Teammates[0].IsDisconnected {
			t.Errorf("Frame 3: reconnected teammate should have IsDisconnected=false")
		}
		if m3.Teammates[0].Stats.Score != 300 || m3.Teammates[0].Stats.Goals != 2 {
			t.Errorf("Frame 3: reconnected stats not updated: %+v", m3.Teammates[0].Stats)
		}
	})

	t.Run("Local Player Disconnect Fallback Preserves Outcomes", func(t *testing.T) {
		store := newTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		guid := "match-dc-local"
		playlistID := 11

		// Frame 1: Local player on Team 0
		tracker.OnUpdateState(ctx, guid, playlistID, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: 100},
			{Name: "Teammate1", PrimaryId: "Steam|tm1|0", TeamNum: 0, Score: 100},
			{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1, Score: 100},
		})

		// Frame 2: Local player disconnects (omitted from players)
		tracker.OnUpdateState(ctx, guid, playlistID, []statsapi.StatsPlayer{
			{Name: "Teammate1", PrimaryId: "Steam|tm1|0", TeamNum: 0, Score: 150},
			{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1, Score: 150},
		})

		m2 := tracker.GetCurrentMatch()
		if m2.LocalPlayer == nil || !m2.LocalPlayer.IsDisconnected {
			t.Fatalf("Local player not retained as disconnected: %+v", m2.LocalPlayer)
		}
		if m2.LocalTeam == nil || *m2.LocalTeam != 0 {
			t.Fatalf("LocalTeam not preserved: got %+v", m2.LocalTeam)
		}
		if len(m2.Teammates) != 1 || m2.Teammates[0].PlayerID != "Steam|tm1|0" {
			t.Errorf("Teammate shifted out of teammates: %+v", m2.Teammates)
		}
		if len(m2.Opponents) != 1 || m2.Opponents[0].PlayerID != "Epic|opp1|0" {
			t.Errorf("Opponent shifted out of opponents: %+v", m2.Opponents)
		}

		// Conclude match: Local team wins
		win := 0
		if err := tracker.OnMatchEnded(ctx, guid, &win); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		// Matchup outcomes should still be successfully recorded!
		tmMatchup, err := store.GetPlayerMatchup(ctx, "Steam|tm1|0", playlistID)
		if err != nil || tmMatchup.WinsAsTeammate != 1 {
			t.Errorf("Teammate outcome failed after local disconnect: err=%v, matchup=%+v", err, tmMatchup)
		}
		oppMatchup, err := store.GetPlayerMatchup(ctx, "Epic|opp1|0", playlistID)
		if err != nil || oppMatchup.WinsAsOpponent != 1 {
			t.Errorf("Opponent outcome failed after local disconnect: err=%v, matchup=%+v", err, oppMatchup)
		}
	})

	t.Run("New Match GUID Resets Retention", func(t *testing.T) {
		store := newTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		// Match 1
		tracker.OnUpdateState(ctx, "match-1", 11, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0},
			{Name: "OldTeammate", PrimaryId: "Steam|old_tm|0", TeamNum: 0, Score: 200},
		})
		// OldTeammate disconnects in Match 1
		tracker.OnUpdateState(ctx, "match-1", 11, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0},
		})
		if len(tracker.GetCurrentMatch().Teammates) != 1 {
			t.Fatalf("Expected OldTeammate retained in match-1")
		}

		// Match 2 starts with new GUID
		tracker.OnUpdateState(ctx, "match-2", 11, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0},
			{Name: "NewTeammate", PrimaryId: "Steam|new_tm|0", TeamNum: 0, Score: 50},
		})

		m2 := tracker.GetCurrentMatch()
		if len(m2.Teammates) != 1 || m2.Teammates[0].PlayerID != "Steam|new_tm|0" {
			t.Fatalf("Match 2 leaked old teammate: %+v", m2.Teammates)
		}
	})

	t.Run("Casual Departed Bot Excluded From Retention", func(t *testing.T) {
		store := newTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		guid := "match-casual-bots"
		playlistID := 1 // Casual Duel/Doubles

		// Frame 1: Human teammate + Bot
		tracker.OnUpdateState(ctx, guid, playlistID, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0},
			{Name: "HumanPlayer", PrimaryId: "Steam|human|0", TeamNum: 0, Score: 100},
			{Name: "BotTex", PrimaryId: "Unknown|0|0", TeamNum: 0, Score: 20},
		})

		// Frame 2: Bot leaves (replaced by human newcomer), original human leaves
		tracker.OnUpdateState(ctx, guid, playlistID, []statsapi.StatsPlayer{
			{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0},
			{Name: "NewHuman", PrimaryId: "Steam|new_human|0", TeamNum: 0, Score: 0},
		})

		m2 := tracker.GetCurrentMatch()
		// Teammates should contain: NewHuman (active) and HumanPlayer (retained). BotTex must NOT be retained.
		if len(m2.Teammates) != 2 {
			t.Fatalf("Expected exactly 2 teammates (NewHuman + retained HumanPlayer), got %d: %+v", len(m2.Teammates), m2.Teammates)
		}
		for _, tm := range m2.Teammates {
			if tm.IsBot {
				t.Errorf("Departed bot was erroneously retained in active state: %+v", tm)
			}
		}
	})
}
```

---

## 5. Verification Method

### 5.1 Independent Verification Commands
1. Run target unit tests in `internal/playertrack`:
   ```powershell
   go test -v -run TestTracker_MidGameDisconnect ./internal/playertrack/...
   ```
2. Run session integration tests:
   ```powershell
   go test -v -run TestSessionTracker ./internal/session/...
   ```
3. Run complete Go test suite across all 14 packages:
   ```powershell
   go test ./...
   ```
4. Build the standalone executable:
   ```powershell
   go build ./cmd/rl-sync
   ```

### 5.2 Files to Inspect
- `internal/playertrack/tracker.go`:
  - Lines 48-60 (`LobbyPlayer` struct definition, `IsDisconnected` field)
  - Lines 62-80 (`LobbyPlayer.DeepClone()`)
  - Lines 324-489 (`OnUpdateState` differential retention logic)
- `internal/session/models.go`:
  - Lines 72-85 (`SessionMatchPlayer` struct definition, `IsDisconnected` field)
- `internal/session/session.go`:
  - Lines 282-296 (`ConcludeMatch` mapping `lp.IsDisconnected`)
- `web/src/types/api.ts`:
  - Lines 40-50 (`SessionMatchPlayer`), Lines 82-93 (`LobbyPlayer`)
- `internal/playertrack/tracker_test.go`:
  - `TestTracker_MidGameDisconnect` suite verifying active stats preservation, disconnection flag, reconnection, local player fallback, and match outcomes.

### 5.3 Invalidation Conditions
- If `t.currentMatch` is reset or cleared by another goroutine during active gameplay without transitioning `matchGUID`, participant retention between frames will fail.
- If Rocket League's `MatchStatsExporter_TA` begins reusing `PrimaryId` across distinct human players within the same match, key collisions could occur. (Protected against by standard platform-prefixed account ID format).
