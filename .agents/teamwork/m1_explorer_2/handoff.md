# Handoff Report: Milestone M1 (Requirement R2) — internal/session & Downstream Consumers

**Agent**: `m1_explorer_2`  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2`  
**Date**: 2026-10-06T08:58:00Z  
**Type**: Hard Handoff  
**Scope**: `internal/session`, `internal/daemon`, downstream REST/SSE exposure, models, and deep-clone propagation for Milestone M1 (Requirement R2: Persistent Player State on Mid-Game Disconnect).

---

## 1. Observation

### 1.1 Ingestion Flow in `SessionTracker` (`internal/session/session.go`)
- **Observer Wiring**:
  In `internal/daemon/daemon.go:156-158`:
  ```go
  156: 	if d.playerTracker != nil && d.sessionTracker != nil {
  157: 		d.playerTracker.SetMatchStateListener(d.sessionTracker)
  158: 	}
  ```
  `SessionTracker` implements `playertrack.MatchStateListener`.
- **`OnActiveMatchUpdated` Implementation**:
  In `internal/session/session.go:447-450`:
  ```go
  447: // OnActiveMatchUpdated implements playertrack.MatchStateListener.
  448: func (s *SessionTracker) OnActiveMatchUpdated(match *playertrack.CurrentMatchResponse) {
  449: 	s.RecordActiveMatch(match)
  450: }
  ```
- **`RecordActiveMatch` State & Concurrency Semantics**:
  In `internal/session/session.go:127-143, 204-209`:
  ```go
  127: func (s *SessionTracker) RecordActiveMatch(match *playertrack.CurrentMatchResponse) {
  128: 	if match == nil || strings.TrimSpace(match.MatchGUID) == "" || match.MatchEnded {
  129: 		return
  130: 	}
  131: 
  132: 	s.mu.Lock()
  ...
  142: 	s.currentMatch = match.DeepClone()
  ...
  204: 	matchClone := s.currentMatch.DeepClone()
  205: 	s.mu.Unlock()
  206: 
  207: 	if s.broadcaster != nil && matchClone != nil {
  208: 		s.broadcaster.Broadcast(SessionEvent{Event: EventMatchUpdate, Data: matchClone})
  209: 	}
  210: }
  ```
  - `s.currentMatch` is updated with `match.DeepClone()`.
  - An isolated clone `matchClone` is broadcast via `s.broadcaster.Broadcast(SessionEvent{Event: EventMatchUpdate, Data: matchClone})` outside the mutex lock.
  - If a player leaves mid-game and `playertrack.Tracker` retains them in `match.Teammates` or `match.Opponents` with `IsDisconnected = true`, `SessionTracker` clones this representation into `s.currentMatch` and broadcasts it to all SSE subscribers.

### 1.2 DeepClone Propagation of `IsDisconnected`
- In `internal/playertrack/tracker.go:62-80` (`LobbyPlayer.DeepClone`):
  ```go
  62: func (p LobbyPlayer) DeepClone() LobbyPlayer {
  63: 	clone := p
  64: 	if p.CurrentRank != nil {
  65: 		cr := *p.CurrentRank
  66: 		clone.CurrentRank = &cr
  67: 	}
  ...
  79: 	return clone
  80: }
  ```
  `clone := p` copies all struct fields by value. Because `IsDisconnected` is a boolean scalar field, adding `IsDisconnected bool` to `LobbyPlayer` causes `LobbyPlayer.DeepClone()` to copy it automatically.
- In `internal/playertrack/tracker.go:109-154` (`CurrentMatchResponse.DeepClone`):
  ```go
  133: 	if s.LocalPlayer != nil {
  134: 		lp := s.LocalPlayer.DeepClone()
  135: 		clone.LocalPlayer = &lp
  136: 	}
  137: 
  138: 	clone.Teammates = make([]LobbyPlayer, len(s.Teammates))
  139: 	for i := range s.Teammates {
  140: 		clone.Teammates[i] = s.Teammates[i].DeepClone()
  141: 	}
  142: 
  143: 	clone.Opponents = make([]LobbyPlayer, len(s.Opponents))
  144: 	for i := range s.Opponents {
  145: 		clone.Opponents[i] = s.Opponents[i].DeepClone()
  146: 	}
  ```
  `CurrentMatchResponse.DeepClone()` clones every `LobbyPlayer` in `LocalPlayer`, `Teammates`, `Opponents`, and `Spectators`.
  Therefore, calling `match.DeepClone()` in `SessionTracker.RecordActiveMatch` (line 142) and `s.currentMatch.DeepClone()` (line 204 & line 425) preserves `IsDisconnected` on all participants.
- In `internal/session/models.go:161-183` (`SessionResponse.DeepClone`):
  ```go
  179: 	if r.ActiveMatch != nil {
  180: 		clone.ActiveMatch = r.ActiveMatch.DeepClone()
  181: 	}
  ```
  Calling `s.GetSessionSummary()` returns a deep clone whose `ActiveMatch` maintains `IsDisconnected` on all players.

### 1.3 Disconnect Retention Gap in `SessionMatchPlayer` & `ConcludeMatch`
- In `internal/session/models.go:71-85`:
  ```go
  71: // SessionMatchPlayer captures an individual player's performance and credentials in a completed match.
  72: type SessionMatchPlayer struct {
  73: 	PlayerID      string                         `json:"player_id"`
  74: 	Platform      string                         `json:"platform"`
  75: 	Name          string                         `json:"name"`
  76: 	TeamNum       int                            `json:"team_num"` // 0 = Blue, 1 = Orange, 255 = Spectator
  77: 	IsLocal       bool                           `json:"is_local"`
  78: 	IsBot         bool                           `json:"is_bot"`
  79: 	Stats         playertrack.PlayerStatsSummary `json:"stats"`
  80: 	RankName      string                         `json:"rank_name"`
  81: 	Tier          int                            `json:"tier"`
  82: 	Division      int                            `json:"division"`
  83: 	MMR           float64                        `json:"mmr"`
  84: 	MatchupRecord *storage.PlayerMatchup         `json:"matchup_record,omitempty"`
  85: }
  ```
  `SessionMatchPlayer` is currently **missing** `IsDisconnected bool json:"is_disconnected,omitempty"` and `Won *bool json:"won,omitempty"`.
- In `internal/session/session.go:282-296` (`ConcludeMatch`):
  ```go
  282: 		matchPlayers = append(matchPlayers, SessionMatchPlayer{
  283: 			PlayerID:      lp.PlayerID,
  284: 			Platform:      lp.Platform,
  285: 			Name:          lp.Name,
  286: 			TeamNum:       lp.TeamNum,
  287: 			IsLocal:       lp.IsLocal,
  288: 			IsBot:         lp.IsBot,
  289: 			Stats:         lp.Stats,
  290: 			RankName:      rankName,
  291: 			Tier:          tier,
  292: 			Division:      div,
  293: 			MMR:           mmr,
  294: 			MatchupRecord: lp.MatchupRecord,
  295: 		})
  ```
  `ConcludeMatch` does not map `IsDisconnected: lp.IsDisconnected` or compute `Won`.
- Goal aggregation in `ConcludeMatch` (lines 246-270):
  ```go
  246: 	allPlayers := make([]playertrack.LobbyPlayer, 0, len(match.Teammates)+len(match.Opponents)+len(match.Spectators)+1)
  247: 	if match.LocalPlayer != nil {
  248: 		allPlayers = append(allPlayers, *match.LocalPlayer)
  249: 	}
  250: 	allPlayers = append(allPlayers, match.Teammates...)
  251: 	allPlayers = append(allPlayers, match.Opponents...)
  252: 	allPlayers = append(allPlayers, match.Spectators...)
  ...
  266: 		if lp.TeamNum == 0 {
  267: 			blueScore += lp.Stats.Goals
  268: 		} else if lp.TeamNum == 1 {
  269: 			orangeScore += lp.Stats.Goals
  270: 		}
  ```
  Because `allPlayers` includes `match.Teammates` and `match.Opponents`, retaining disconnected players in those slices ensures their goals are correctly summed into `blueScore` or `orangeScore`.

### 1.4 Downstream Consumers: SSE and REST Exposure
- **Server-Sent Events (`internal/daemon/sse.go:46-80, 101-115`)**:
  ```go
  48: 		sessionEv := session.SessionEvent{
  49: 			Event: session.EventSessionUpdate,
  50: 			Data:  summary,
  51: 		}
  ...
  70: 		matchEv := session.SessionEvent{
  71: 			Event: session.EventMatchUpdate,
  72: 			Data:  currentMatch,
  73: 		}
  ...
  106: 			wireBytes, err := ev.FormatSSE()
  ```
  `SessionEvent.FormatSSE()` executes `json.Marshal(e.Data)`. Because Go's `json.Marshal` serializes struct fields using their `json:"..."` tags:
  - Any player with `IsDisconnected: true` in `EventMatchUpdate` (`*CurrentMatchResponse`) will produce `"is_disconnected": true`.
  - In `EventMatchEnded` (`*SessionMatchDetail`), `Players` will serialize each player with `"is_disconnected": true`.
  - In `EventSessionUpdate` (`*SessionResponse`), both `active_match` and `matches` will serialize `"is_disconnected": true`.
  - **No modifications are required in `internal/daemon/sse.go`** — serialization is completely data-driven.
- **REST Endpoints (`internal/daemon/handlers_players.go:25-58` & `internal/daemon/handlers_session.go:10-26`)**:
  - `GET /current-match` and `GET /api/current-match`: Encodes `d.playerTracker.GetCurrentMatch()`. Automatically serializes `"is_disconnected": true` on retained players.
  - `GET /api/session`: Encodes `d.sessionTracker.GetSessionSummary()`. Automatically serializes `"is_disconnected": true` on both `active_match` and completed `matches`.
  - **No modifications are required in `handlers_players.go` or `handlers_session.go`**.

### 1.5 TypeScript Definitions (`web/src/types/api.ts`)
- In `web/src/types/api.ts:37-50` (`SessionMatchPlayer`) and lines 82-93 (`LobbyPlayer`):
  Neither interface includes `is_disconnected?: boolean;`.
  `SessionMatchPlayer` is also missing `won?: boolean;`.

### 1.6 Verification of Existing Tests
- Running `go test -v ./internal/session/...`: 100% pass rate across 27 tests (5.05s runtime).
- Running `go test -v ./internal/daemon/...`: 100% pass rate across all REST, SSE, and security tests (13.29s runtime).

---

## 2. Logic Chain

1. **Premise**: When a player disconnects mid-game, `playertrack.Tracker` retains their identity and accumulated stats in `t.currentMatch.Teammates` or `t.currentMatch.Opponents` with `IsDisconnected = true` (Observation 1.1, 1.2).
2. **Mechanism in Session Ingestion**: When `Tracker.OnUpdateState` notifies `listener.OnActiveMatchUpdated(matchClone)`, `SessionTracker` receives the match snapshot in `RecordActiveMatch` (Observation 1.1).
3. **Deep-Clone Isolation**: `SessionTracker` performs `s.currentMatch = match.DeepClone()`. Because `LobbyPlayer.DeepClone()` copies value fields, `IsDisconnected = true` is preserved in `s.currentMatch` without data races under concurrent reads (Observation 1.2).
4. **SSE Distribution**: `RecordActiveMatch` broadcasts `SessionEvent{Event: EventMatchUpdate, Data: matchClone}`. `SessionEvent.FormatSSE()` marshals `CurrentMatchResponse` into JSON, delivering `"is_disconnected": true` to connected frontend and streaming clients (Observation 1.4).
5. **REST API Queries**: When a client calls `GET /api/session` or `GET /current-match`, `GetSessionSummary()` and `GetCurrentMatch()` return deep clones containing the retained players with `"is_disconnected": true` (Observation 1.4).
6. **Match Conclusion & Historical Snapshot**: When the match finishes, `ConcludeMatch(concludedSnapshot)` compiles `SessionMatchDetail`. Because `allPlayers` includes `match.Teammates` and `match.Opponents`, the disconnected player is included. By mapping `lp.IsDisconnected` to `SessionMatchPlayer.IsDisconnected`, the completed match history permanently preserves the player's disconnect state and their box score stats (Observation 1.3).
7. **Score Integrity**: Goal aggregation in `ConcludeMatch` sums `lp.Stats.Goals` across `allPlayers`. Retaining disconnected players guarantees that goals scored prior to disconnection are counted in `blueScore` or `orangeScore` (Observation 1.3).
8. **Memory Safety & Race Immunity**: `SessionMatchPlayer.DeepClone()` must clone any pointer fields (such as `Won *bool` for M2) to maintain thread safety across concurrent readers of `SessionResponse.Matches` (Observation 1.3).

---

## 3. Caveats

1. **Bot Substitutions in Casual Matches**: When a player disconnects in casual playlists, Rocket League may backfill an AI bot (`Unknown|0|0`). Retaining the disconnected human player means the roster will hold both the human (`IsDisconnected: true, IsBot: false`) and the bot (`IsDisconnected: false, IsBot: true`). In `ConcludeMatch`, both the human's goals and the bot's goals contribute to the team score, which accurately reflects game reality.
2. **Reconnection Deduplication**: If a player leaves and reconnects within the same match, `ConcludeMatch`'s `seenPlayerIDs` map deduplicates entries, ensuring that `SessionMatchDetail.Players` contains exactly one record for that player with their final stats.
3. **Local Player Disconnect Scenario**: If the local player disconnects, `SessionTracker` relies on `match.LocalPlayer != nil` being preserved by `playertrack.Tracker`. If `LocalPlayer` is retained with `IsDisconnected = true`, baseline MMR calculations in `RecordActiveMatch` and `Reset` continue to work without nil dereferences.
4. **Scope Separation (M1 vs M2)**: Milestone M1 focuses on participant state retention during the active match and propagating `IsDisconnected`. Milestone M2 focuses on storage outcome logging (`player_matchups`) and `Won` outcome mapping in concluded matches. Adding `IsDisconnected` and `Won` to `SessionMatchPlayer` now ensures complete interface parity with `PROJECT.md:61-73` and smooth transition to M2.

---

## 4. Conclusion & Recommended Code Changes

### 4.1 Changes in `internal/session/models.go`

Update `SessionMatchPlayer` and `SessionMatchPlayer.DeepClone()`:

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
	Won            *bool                          `json:"won,omitempty"`
	Stats          playertrack.PlayerStatsSummary `json:"stats"`
	RankName       string                         `json:"rank_name"`
	Tier           int                            `json:"tier"`
	Division       int                            `json:"division"`
	MMR            float64                        `json:"mmr"`
	MatchupRecord  *storage.PlayerMatchup         `json:"matchup_record,omitempty"`
}

// DeepClone returns an isolated copy of SessionMatchPlayer.
func (p SessionMatchPlayer) DeepClone() SessionMatchPlayer {
	clone := p
	if p.Won != nil {
		won := *p.Won
		clone.Won = &won
	}
	if p.MatchupRecord != nil {
		rec := *p.MatchupRecord
		clone.MatchupRecord = &rec
	}
	return clone
}
```

### 4.2 Changes in `internal/session/session.go`

In `ConcludeMatch`:

```go
		var rankName string
		var tier, div int
		var mmr float64
		if lp.CurrentRank != nil {
			rankName = lp.CurrentRank.RankName
			tier = lp.CurrentRank.Tier
			div = lp.CurrentRank.Division
			mmr = lp.CurrentRank.MMR
		}

		var won *bool
		if match.WinnerTeam != nil && (lp.TeamNum == 0 || lp.TeamNum == 1) {
			w := (lp.TeamNum == *match.WinnerTeam)
			won = &w
		}

		matchPlayers = append(matchPlayers, SessionMatchPlayer{
			PlayerID:       lp.PlayerID,
			Platform:       lp.Platform,
			Name:           lp.Name,
			TeamNum:        lp.TeamNum,
			IsLocal:        lp.IsLocal,
			IsBot:          lp.IsBot,
			IsDisconnected: lp.IsDisconnected,
			Won:            won,
			Stats:          lp.Stats,
			RankName:       rankName,
			Tier:           tier,
			Division:       div,
			MMR:            mmr,
			MatchupRecord:  lp.MatchupRecord,
		})
```

### 4.3 Changes in `web/src/types/api.ts`

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
  won?: boolean;
  stats: PlayerStatsSummary;
  rank_name: string;
  tier: number;
  division: number;
  mmr: number;
  matchup_record?: PlayerMatchup;
}
```

### 4.4 Proposed Unit Tests for `internal/session/session_test.go`

Add three targeted tests to `internal/session/session_test.go`:

1. **`TestSessionTracker_ActiveMatch_PlayerDisconnect`**:
   - Frame 1: Record active match with teammate `Steam|tm_1|0` (`IsDisconnected: false`, `Goals: 2`, `Score: 250`).
   - Frame 2: Record active match where `Steam|tm_1|0` is marked `IsDisconnected: true`.
   - Assert `s.GetSessionSummary().ActiveMatch.Teammates[0].IsDisconnected == true`.
   - Assert teammate's stats (`Goals: 2`, `Score: 250`) remain completely intact.
   - Assert SSE subscriber receives `EventMatchUpdate` containing `is_disconnected: true`.

2. **`TestSessionTracker_ConcludeMatch_DisconnectedPlayerRetained`**:
   - Construct concluding `CurrentMatchResponse` where teammate `Steam|tm_1|0` disconnected (`IsDisconnected: true`, `Goals: 2`) and opponent `Steam|opp_1|0` (`Goals: 1`).
   - Winner is Blue (Team 0, `winnerTeam = 0`).
   - Call `s.ConcludeMatch(match)`.
   - Assert `detail.BlueScore == 2` (includes disconnected player's goals).
   - Assert `detail.Players` contains `Steam|tm_1|0` with `IsDisconnected == true` and `Won != nil && *Won == true`.
   - Assert `detail.Players` contains opponent with `IsDisconnected == false` and `Won != nil && *Won == false`.

3. **`TestSessionTracker_DeepClone_PreservesDisconnect`**:
   - Construct `SessionMatchPlayer` and `SessionMatchDetail` with disconnected player.
   - Deep clone the detail and session response.
   - Mutate original struct; assert clone retains `IsDisconnected: true` and `Won` pointer is isolated.

---

## 5. Verification Method

### 5.1 Independent Verification Commands
1. Run target unit tests in `internal/session`:
   ```powershell
   go test -v -run "TestSessionTracker_ActiveMatch_PlayerDisconnect|TestSessionTracker_ConcludeMatch_DisconnectedPlayerRetained|TestSessionTracker_DeepClone_PreservesDisconnect" ./internal/session/...
   ```
2. Run full session package test suite:
   ```powershell
   go test -v ./internal/session/...
   ```
3. Run daemon package test suite (verifying SSE and REST serialization):
   ```powershell
   go test -v ./internal/daemon/...
   ```
4. Run full repository regression test suite:
   ```powershell
   go test ./...
   ```
5. Run Vitest frontend test suite:
   ```powershell
   npm --prefix web test
   ```

### 5.2 Files to Inspect
- `internal/session/models.go` (lines 72-96, `SessionMatchPlayer` definition and `DeepClone`)
- `internal/session/session.go` (lines 142-209, `RecordActiveMatch`; lines 246-296, `ConcludeMatch`)
- `internal/daemon/sse.go` (lines 46-80, initial SSE snapshots)
- `internal/daemon/handlers_players.go` (lines 25-58, `handleCurrentMatch`)
- `internal/daemon/handlers_session.go` (lines 10-26, `handleGetSession`)
- `web/src/types/api.ts` (lines 37-50, 82-93)
- `internal/session/session_test.go`

### 5.3 Invalidation Conditions
- If `LobbyPlayer.DeepClone()` is refactored to perform shallow pointer sharing rather than value copy of struct primitives, `IsDisconnected` could experience data races during concurrent read/write cycles.
- If `SessionTracker.ConcludeMatch` were modified to filter out disconnected players from `allPlayers`, team goal calculation (`blueScore`/`orangeScore`) and session match history roster retention would fail.
