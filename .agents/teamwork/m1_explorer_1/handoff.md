# Handoff Report: Milestone 1 - True In-Game MMR Conversion (R1)

## 1. Observation

### 1.1 Ingestion Layer & Single Conversion Chokepoint
- **File: `internal/playertrack/rank_client.go` (Lines 1–19, 616–675)**:
  `internal/playertrack/rank_client.go` currently imports:
  ```go
  package playertrack

  import (
  	"context"
  	"encoding/json"
  	"errors"
  	"fmt"
  	"log/slog"
  	"net"
  	"strconv"
  	"strings"
  	"sync"

  	"github.com/dank/rl-api-utils/internal/auth"
  	"github.com/dank/rl-api-utils/internal/config"
  	"github.com/dank/rl-api-utils/internal/psynet"
  	"github.com/dank/rlapi"
  )
  ```
  Note: `"math"` is **not** currently imported.

  Lines 616–675 define `PlayerPlaylistRank`, `FromSkill`, `BuildRanksSnapshot`, and `SerializeRanksJSON`:
  ```go
  type PlayerPlaylistRank struct {
  	PlaylistID    int     `json:"playlist_id"`
  	PlaylistName  string  `json:"playlist_name"`
  	Tier          int     `json:"tier"`
  	Division      int     `json:"division"`
  	RankName      string  `json:"rank_name"`
  	MMR           float64 `json:"mmr"`
  	MatchesPlayed int     `json:"matches_played"`
  }

  type PlayerRanksSnapshot map[string]PlayerPlaylistRank

  func (s PlayerRanksSnapshot) GetRank(playlistID int) (PlayerPlaylistRank, bool) {
  	if s == nil {
  		return PlayerPlaylistRank{}, false
  	}
  	rank, ok := s[strconv.Itoa(playlistID)]
  	return rank, ok
  }

  func FromSkill(s rlapi.Skill) PlayerPlaylistRank {
  	return PlayerPlaylistRank{
  		PlaylistID:    s.Playlist,
  		PlaylistName:  FormatPlaylist(s.Playlist),
  		Tier:          s.Tier,
  		Division:      s.Division,
  		RankName:      FormatRank(s.Tier, s.Division),
  		MMR:           s.MMR, // Line 647: raw s.MMR copied directly
  		MatchesPlayed: s.MatchesPlayed,
  	}
  }

  func BuildRanksSnapshot(skills []rlapi.Skill) PlayerRanksSnapshot {
  	snapshot := make(PlayerRanksSnapshot, len(skills))
  	for _, s := range skills {
  		key := strconv.Itoa(s.Playlist)
  		snapshot[key] = FromSkill(s)
  	}
  	return snapshot
  }

  func SerializeRanksJSON(skills []rlapi.Skill) (string, error) {
  	if len(skills) == 0 {
  		return "{}", nil
  	}
  	snapshot := BuildRanksSnapshot(skills)
  	data, err := json.Marshal(snapshot)
  	if err != nil {
  		return "{}", fmt.Errorf("failed to serialize ranks to JSON: %w", err)
  	}
  	return string(data), nil
  }
  ```

- **File: `internal/playertrack/tracker.go` (Lines 984–1034)**:
  In `fetchRanksBatch`, rank telemetry received from PsyNet (`rlapi.Skill`) is processed exclusively through `BuildRanksSnapshot` and `SerializeRanksJSON`:
  ```go
  snapshot := BuildRanksSnapshot(skills)
  t.rankCache[idStr] = cachedRankEntry{
  	snapshot:  snapshot,
  	fetchedAt: now,
  }

  if t.currentMatch != nil {
  	t.updateLobbyPlayerRanksLocked(idStr, snapshot)
  }

  ranksJSON, serErr := SerializeRanksJSON(skills)
  if serErr == nil {
  	toPersist = append(toPersist, struct {
  		playerID  string
  		ranksJSON string
  	}{playerID: idStr, ranksJSON: ranksJSON})
  }
  ...
  listener := t.listener
  t.mu.Unlock()

  if listener != nil && rankSnapshot != nil {
  	listener.OnActiveMatchUpdated(rankSnapshot)
  }
  ```
  `FromSkill` is therefore the solitary chokepoint through which incoming PsyNet skill telemetry enters the application runtime.

### 1.2 Legacy Deserialization Layer
- **File: `internal/playertrack/rank_client.go` (Lines 679–713)**:
  ```go
  func ParseRanksJSON(raw string) (PlayerRanksSnapshot, error) {
  	trimmed := strings.TrimSpace(raw)
  	if trimmed == "" || trimmed == "{}" {
  		return make(PlayerRanksSnapshot), nil
  	}

  	var snapshot PlayerRanksSnapshot
  	if err := json.Unmarshal([]byte(trimmed), &snapshot); err != nil {
  		return nil, fmt.Errorf("failed to unmarshal ranks_json: %w", err)
  	}

  	// Normalization & backfill pass for legacy or partial data
  	for key, rank := range snapshot {
  		changed := false
  		if rank.PlaylistID == 0 {
  			if pid, err := strconv.Atoi(key); err == nil {
  				rank.PlaylistID = pid
  				changed = true
  			}
  		}
  		if rank.PlaylistName == "" && rank.PlaylistID != 0 {
  			rank.PlaylistName = FormatPlaylist(rank.PlaylistID)
  			changed = true
  		}
  		if rank.RankName == "" {
  			rank.RankName = FormatRank(rank.Tier, rank.Division)
  			changed = true
  		}
  		if changed {
  			snapshot[key] = rank
  		}
  	}

  	return snapshot, nil
  }
  ```
  `ParseRanksJSON` contains an existing backfill normalization pass, which currently checks `rank.PlaylistID == 0`, `rank.PlaylistName == ""`, and `rank.RankName == ""`.

### 1.3 Downstream Session Tracking & MMR Delta Computations
- **File: `internal/session/models.go` (Lines 50–60, 85, 116–118)**:
  ```go
  type PlaylistSessionStats struct {
  	InitialMMR    float64 `json:"initial_mmr"`
  	CurrentMMR    float64 `json:"current_mmr"`
  	MMRDelta      float64 `json:"mmr_delta"`
  }
  type SessionMatchPlayer struct {
  	MMR float64 `json:"mmr"`
  }
  type SessionMatchDetail struct {
  	StartingMMR float64 `json:"starting_mmr"`
  	EndingMMR   float64 `json:"ending_mmr"`
  	MMRChange   float64 `json:"mmr_change"`
  }
  ```
- **File: `internal/session/session.go` (Lines 146–179, 320–328)**:
  `RecordActiveMatch` reads `match.LocalPlayer.CurrentRank.MMR` directly into `startingMMR` and `InitialMMR`/`CurrentMMR`.
  `ConcludeMatch` reads `match.LocalPlayer.CurrentRank.MMR` into `endingMMR` and computes `mmrChange = roundFloat(endingMMR-startingMMR, 2)`.
  When `CurrentRank.MMR` holds true in-game MMR, all session metrics immediately compute deltas on the true in-game MMR scale without needing code modifications in `internal/session`.

### 1.4 Database Storage (SQLite & JSONStore)
- **File: `internal/storage/sqlite.go` (Lines 48–56, 1015–1042)** and **`internal/storage/jsonstore.go` (Lines 17–25)**:
  The `players` table defines `ranks_json TEXT NOT NULL DEFAULT '{}'`. Neither engine inspects or validates the contents of `ranks_json`; it is stored and retrieved as an opaque string. No database schema migration (`ALTER TABLE`) is needed.

### 1.5 Existing Test Fixtures with Pre-Scaled MMR (> 150.0)
- Multiple test suites across the repository initialize mock `rlapi.Skill` payloads with already-scaled in-game MMR values:
  - `internal/playertrack/tracker_test.go:881`: `MMR: 1150.5`
  - `internal/playertrack/tracker_test.go:979`: `MMR: 1200.0`
  - `internal/playertrack/rank_client_test.go:321`: `MMR: 1150.5`, Line 328: `MMR: 1080.2`
  - `internal/playertrack/tracker_stress_test.go:90`: `MMR: 1150.0`, Line 91: `MMR: 1080.0`
  - `test/e2e/playertrack_e2e_test.go:230`: `1040.0`, `1055.0`, `1120.0`, `1005.0`
  - `test/e2e/tier5_pt_adversarial_test.go:410-413`: `1040.0`, `1055.0`, `1120.0`, `1005.0`
  - `test/e2e/tier5_dashboard_adversarial_test.go:60-63`: `1040.0`, `1055.0`, `1120.0`, `1005.0`
  - Casual playlist tests across `internal/session/session_test.go:284` and `stress_challenger_test.go:242`: `MMR: 0.0`.

All existing Go tests currently pass cleanly:
```powershell
go test ./internal/playertrack -> ok (cached)
go test ./internal/session -> ok (cached)
go test ./internal/storage ./internal/daemon -> ok (cached)
go test ./test/e2e -> ok (cached)
```

---

## 2. Logic Chain

1. **PsyNet Telemetry Semantics**:
   - In PsyNet RPC `Skills/GetPlayersSkills v1`, the property named `"MMR"` in the response payload (unmarshaled into `rlapi.Skill.MMR`) represents TrueSkill raw skill rating $\mu$, typically a decimal between $20.0$ and $85.0$ (e.g., $47.5$).
   - The user requirement (R1) specifies converting raw skill ratings to true in-game display MMR using the canonical formula:
     $$\text{MMR} = \text{round}((\text{SkillRating} \times 20.0) + 100.0)$$
   - For a raw rating of $47.5$:
     $$\text{round}((47.5 \times 20.0) + 100.0) = \text{round}(950.0 + 100.0) = 1050.0$$

2. **Single Chokepoint Conversion Architecture**:
   - As observed in Observation 1.1, `FromSkill(s rlapi.Skill)` is the singular function that instantiates `PlayerPlaylistRank` from `rlapi.Skill`.
   - `BuildRanksSnapshot` iterates over `[]rlapi.Skill` and invokes `FromSkill(s)` for each playlist.
   - `SerializeRanksJSON` calls `BuildRanksSnapshot(skills)` and serializes the result.
   - `Tracker.fetchRanksBatch` calls `BuildRanksSnapshot` for runtime caching and active match updates, and calls `SerializeRanksJSON` for database persistence.
   - Therefore, converting `s.MMR` inside `FromSkill` propagates in-game MMR simultaneously to:
     - Active match state (`LobbyPlayer.CurrentRank` and `LobbyPlayer.Ranks`)
     - Persistent storage (`players.ranks_json` in SQLite and JSONStore)
     - Session tracker notifications (`OnActiveMatchUpdated`)
     - REST and SSE API responses (`/api/current-match`, `/api/session`, `/api/players`)

3. **Guards & Precision Analysis for `ConvertSkillRatingToMMR`**:
   - **Guard 1: Casual & Unranked Ratings (`skillRating <= 0.0 -> 0.0`)**:
     - Casual playlists and unranked accounts provide `0.0` (or theoretically negative $\mu$ under extreme loss streaks on new accounts).
     - Without this guard, `0.0` would evaluate to $(0.0 \times 20.0) + 100.0 = 100.0$ MMR.
     - In Rocket League, 100 MMR corresponds to Bronze I Division I. Displaying 100 MMR for casual playlists would corrupt casual match tracking, break casual session delta suppression in `session.go` (which guards on `startingMMR > 0 && endingMMR > 0`), and prevent the web UI from displaying `"---"` via `formatMMR`.
     - Clamping `skillRating <= 0.0` to `0.0` guarantees casual and unranked ratings remain `0.0`.
   - **Guard 2: Pre-Scaled Ratings & Idempotency (`skillRating > 150.0 -> skillRating`)**:
     - In Rocket League TrueSkill mechanics, a raw skill rating $\mu = 125.0$ yields $(125.0 \times 20.0) + 100.0 = 2600.0$ MMR, which is the highest competitive MMR ever achieved in Rocket League history (top 10 world professional players).
     - Even an impossible 3000 MMR rating corresponds to $\mu = (3000 - 100) / 20 = 145.0$.
     - Therefore, a genuine raw PsyNet skill rating will never exceed $150.0$.
     - Conversely, true in-game display MMR numbers are virtually always $\ge 100.0$ (typically $600 - 2000$).
     - Observation 1.5 identified that existing test fixtures across `playertrack`, `daemon`, `session`, and `test/e2e` populate `rlapi.Skill.MMR` with pre-scaled values such as `1040.0`, `1150.5`, `1055.0`.
     - Without the `> 150.0` guard, `1150.5` would be re-converted to $(1150.5 \times 20.0) + 100.0 = 23,110.0$, causing widespread test failures and regressions.
     - With `if skillRating > 150.0 { return skillRating }`, pre-scaled numbers pass through completely untouched, and the conversion is strictly idempotent:
       $$\text{ConvertSkillRatingToMMR}(\text{ConvertSkillRatingToMMR}(x)) = \text{ConvertSkillRatingToMMR}(x)$$
   - **Precision**:
     - `math.Round((skillRating * 20.0) + 100.0)` returns `float64` rounded to the nearest integer.
     - Exact test values:
       - $47.5 \to 1050.0$
       - $47.0 \to 1040.0$
       - $30.4646 \to 709.0$
       - $81.8645 \to 1737.0$
       - $0.0 \to 0.0$
       - $1050.0 \to 1050.0$

4. **Transparent Legacy Data Migration in `ParseRanksJSON`**:
   - In production databases populated prior to Milestone 1, `ranks_json` may contain raw ratings (e.g. `{"11": {"playlist_id": 11, "tier": 16, "division": 2, "mmr": 47.5}}`).
   - By updating the normalization loop in `ParseRanksJSON`:
     ```go
     if rank.MMR > 0.0 && rank.MMR <= 150.0 {
         rank.MMR = ConvertSkillRatingToMMR(rank.MMR)
         changed = true
     }
     ```
     any legacy record loaded from SQLite or JSONStore is dynamically migrated to true in-game MMR upon deserialization.
   - Already-migrated records (`rank.MMR > 150.0`) and casual records (`rank.MMR == 0.0`) are not modified.
   - This provides seamless zero-downtime backward compatibility without database schema migrations.

---

## 3. Caveats

1. **Unranked / Casual Playlists**:
   - PsyNet returns `0.0` (or omits skill telemetry) for unranked and casual playlists. The guard `skillRating <= 0.0 -> 0.0` is essential to prevent casual ratings from becoming `100.0` MMR.
2. **Ratings in Range (100.0, 150.0]**:
   - In theory, an in-game MMR between 100 and 150 (extremely low Bronze I) falls within $(0.0, 150.0]$. However, in practice, Bronze I Division I players have raw ratings $\mu \approx 2.5$ (which converts to 150 MMR). PsyNet raw ratings in the range $100.0 - 150.0$ correspond to Grand Champion 3 and Supersonic Legend ratings ($2100 - 3100$ MMR). The threshold of $150.0$ provides the ideal discrimination boundary between raw TrueSkill ratings ($\le 125.0$) and pre-scaled display MMR ($\ge 600.0$).
3. **Importing `"math"`**:
   - `internal/playertrack/rank_client.go` does not currently import `"math"`. The implementer must add `"math"` to the import block.

---

## 4. Conclusion

Milestone 1 (R1) is cleanly achievable by editing a single file: `internal/playertrack/rank_client.go`, supported by comprehensive unit tests in `internal/playertrack/rank_client_test.go`.

### 4.1 Implementation Specification

#### 1. Import Block (`internal/playertrack/rank_client.go`)
Add `"math"` to standard imports:
```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
...
)
```

#### 2. `ConvertSkillRatingToMMR` (`internal/playertrack/rank_client.go`)
Place immediately before `FromSkill`:
```go
// ConvertSkillRatingToMMR converts a raw PsyNet skill rating decimal (e.g. 47.5)
// to true in-game MMR using the standard Rocket League formula: round((SkillRating * 20) + 100).
//
// Special Rules:
// - Ratings <= 0.0 (casual, unranked, or uncalibrated) return 0.0.
// - Ratings > 150.0 are treated as already converted in-game MMR and returned unchanged.
func ConvertSkillRatingToMMR(skillRating float64) float64 {
	if skillRating <= 0.0 {
		return 0.0
	}
	if skillRating > 150.0 {
		return skillRating
	}
	return math.Round((skillRating * 20.0) + 100.0)
}
```

#### 3. `FromSkill` (`internal/playertrack/rank_client.go`)
Update Line 647:
```go
// FromSkill constructs a PlayerPlaylistRank from an rlapi.Skill telemetry entry.
func FromSkill(s rlapi.Skill) PlayerPlaylistRank {
	return PlayerPlaylistRank{
		PlaylistID:    s.Playlist,
		PlaylistName:  FormatPlaylist(s.Playlist),
		Tier:          s.Tier,
		Division:      s.Division,
		RankName:      FormatRank(s.Tier, s.Division),
		MMR:           ConvertSkillRatingToMMR(s.MMR),
		MatchesPlayed: s.MatchesPlayed,
	}
}
```

#### 4. `ParseRanksJSON` (`internal/playertrack/rank_client.go`)
Update the normalization loop in `ParseRanksJSON`:
```go
	// Normalization & backfill pass for legacy or partial data
	for key, rank := range snapshot {
		changed := false
		if rank.PlaylistID == 0 {
			if pid, err := strconv.Atoi(key); err == nil {
				rank.PlaylistID = pid
				changed = true
			}
		}
		if rank.PlaylistName == "" && rank.PlaylistID != 0 {
			rank.PlaylistName = FormatPlaylist(rank.PlaylistID)
			changed = true
		}
		if rank.RankName == "" {
			rank.RankName = FormatRank(rank.Tier, rank.Division)
			changed = true
		}
		if rank.MMR > 0.0 && rank.MMR <= 150.0 {
			rank.MMR = ConvertSkillRatingToMMR(rank.MMR)
			changed = true
		}
		if changed {
			snapshot[key] = rank
		}
	}
```

### 4.2 Test Suite Specification (`internal/playertrack/rank_client_test.go`)
Add the following test functions to `rank_client_test.go`:

1. `TestConvertSkillRatingToMMR_Table`:
   - Raw conversions: `47.5 -> 1050.0`, `47.0 -> 1040.0`, `30.4646 -> 709.0`, `81.8645 -> 1737.0`, `125.0 -> 2600.0`, `2.5 -> 150.0`.
   - Casual / guards: `0.0 -> 0.0`, `-1.0 -> 0.0`, `-50.0 -> 0.0`.
   - Pre-scaled passthrough: `1050.0 -> 1050.0`, `1150.5 -> 1150.5`, `150.0001 -> 150.0001`.
   - Boundary: `150.0 -> 3100.0`.
   - Idempotency: `ConvertSkillRatingToMMR(ConvertSkillRatingToMMR(v)) == ConvertSkillRatingToMMR(v)`.

2. `TestFromSkill_TrueInGameMMR`:
   - `rlapi.Skill{Playlist: 11, Tier: 16, Division: 2, MMR: 47.5}` yields `PlayerPlaylistRank.MMR == 1050.0`.
   - Casual `rlapi.Skill{Playlist: 2, MMR: 0.0}` yields `MMR == 0.0`.
   - Pre-scaled `rlapi.Skill{Playlist: 13, MMR: 1120.0}` yields `MMR == 1120.0`.

3. `TestParseRanksJSON_LegacyMigration`:
   - Legacy JSON string `{"11":{"playlist_id":11,"tier":16,"division":2,"mmr":47.5}}` parses to `MMR == 1050.0`.
   - In-game scale JSON string `{"11":{"playlist_id":11,"tier":16,"division":2,"mmr":1050.0}}` parses to `MMR == 1050.0` (not 21,100.0).
   - Casual JSON string `{"2":{"playlist_id":2,"tier":0,"division":0,"mmr":0.0}}` parses to `MMR == 0.0`.

4. `TestBuildRanksSnapshot_And_SerializeRanksJSON_TrueInGameMMR`:
   - Skills with `MMR: 47.5` produces snapshot with `1050.0` and serialized JSON containing `"mmr":1050`.

---

## 5. Verification Method

### 5.1 Independent Test Commands
Execute the following verification commands from repository root (`d:\code\rl-api-utils`):

1. **Targeted Unit Tests**:
   ```powershell
   go test -v -count=1 ./internal/playertrack -run "TestConvertSkillRatingToMMR|TestFromSkill_TrueInGameMMR|TestParseRanksJSON_LegacyMigration"
   ```

2. **Full Playertrack Package Test Suite**:
   ```powershell
   go test -v -count=1 ./internal/playertrack
   ```

3. **Session & Daemon Verification**:
   ```powershell
   go test -v -count=1 ./internal/session ./internal/daemon
   ```

4. **Full Repository Regression Test**:
   ```powershell
   go test -count=1 ./...
   ```
   *Expected Result*: 100% pass across all packages with zero regressions.

5. **Executable Compilation**:
   ```powershell
   go build ./cmd/rl-sync
   ```
   *Expected Result*: Clean compilation of `rl-sync.exe` without compiler or linker errors.

### 5.2 Files to Inspect
- `internal/playertrack/rank_client.go`: Verify imports, `ConvertSkillRatingToMMR`, `FromSkill`, and `ParseRanksJSON`.
- `internal/playertrack/rank_client_test.go`: Verify newly added unit tests.

### 5.3 Invalidation Conditions
- If `ConvertSkillRatingToMMR(47.5)` returns anything other than `1050.0`.
- If `ConvertSkillRatingToMMR(0.0)` returns `100.0` instead of `0.0`.
- If `ConvertSkillRatingToMMR(1150.5)` returns `23110.0` instead of `1150.5`.
- If any existing tests in `internal/playertrack`, `internal/session`, `internal/daemon`, or `test/e2e` fail.
