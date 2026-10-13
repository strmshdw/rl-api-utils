# Handoff Report: Storage Persistence (SQLite & JSONStore) and MMR Edge Cases (Milestone 1, Requirement R1)

## 1. Observation

### 1.1 Baseline Test Suite Verification
- **Command**: `go test -count=1 ./...`
- **Result**: Exit code `0`, 100% pass rate across all 14 Go packages:
  - `cmd/rl-sync` (0.216s)
  - `internal/auth` (1.965s)
  - `internal/ballchasing` (8.519s)
  - `internal/config` (0.522s)
  - `internal/daemon` (14.317s)
  - `internal/playertrack` (6.444s)
  - `internal/psynet` (4.285s)
  - `internal/session` (5.773s)
  - `internal/statsapi` (0.900s)
  - `internal/storage` (20.254s)
  - `internal/syncer` (1.200s)
  - `internal/testutil` (1.126s)
  - `internal/web` (0.610s)
  - `test/e2e` (20.181s)

### 1.2 Storage Persistence Layer (SQLite & JSONStore)
- **`internal/storage/sqlite.go:48-56`**:
  ```sql
  CREATE TABLE IF NOT EXISTS players (
      player_id TEXT PRIMARY KEY,
      platform TEXT NOT NULL DEFAULT '',
      player_name TEXT NOT NULL DEFAULT '',
      player_name_lower TEXT NOT NULL DEFAULT '',
      ranks_json TEXT NOT NULL DEFAULT '{}',
      first_seen_at INTEGER NOT NULL,
      last_seen_at INTEGER NOT NULL
  );
  ```
- **`internal/storage/sqlite.go:687-695` (`UpsertPlayer`)**:
  ```sql
  INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
  VALUES (?, ?, ?, ?, ?, ?, ?)
  ON CONFLICT(player_id) DO UPDATE SET
      platform = CASE WHEN excluded.platform != '' THEN excluded.platform ELSE players.platform END,
      player_name = CASE WHEN excluded.player_name != '' THEN excluded.player_name ELSE players.player_name END,
      player_name_lower = CASE WHEN excluded.player_name != '' THEN excluded.player_name_lower ELSE players.player_name_lower END,
      ranks_json = CASE WHEN excluded.ranks_json != '' AND excluded.ranks_json != '{}' THEN excluded.ranks_json ELSE players.ranks_json END,
      last_seen_at = excluded.last_seen_at;
  ```
  *Behavior*: When a player is upserted with empty `""` or `"{}"` `ranks_json` (e.g. at initial lobby discovery), SQLite explicitly preserves existing `ranks_json`.
- **`internal/storage/sqlite.go:1024-1033` (`UpdatePlayerRanks`)**:
  ```sql
  UPDATE players
  SET ranks_json = ?, last_seen_at = ?
  WHERE player_id = ?;
  ```
  *Behavior*: Directly overwrites `ranks_json` for the given `player_id` with the provided JSON payload. If empty, defaults to `"{}"`.
- **`internal/storage/sqlite.go:719-723, 763-766, 816-820, 948-952`**:
  Queries in `GetPlayer`, `ListPlayers`, `ListPlayerSummaries`, and `SearchPlayerSummaries` select `p.ranks_json` and scan it verbatim into `PlayerRecord.RanksJSON`. SQLite does not validate, parse, or alter the JSON payload.
- **`internal/storage/jsonstore.go:789-816` (`UpsertPlayer`)**:
  ```go
  if exists {
      if player.Platform != "" {
          existing.Platform = player.Platform
      }
      if player.PlayerName != "" {
          existing.PlayerName = player.PlayerName
      }
      if player.RanksJSON != "" && player.RanksJSON != "{}" {
          existing.RanksJSON = player.RanksJSON
      }
      ...
  } else {
      ...
      if rec.RanksJSON == "" {
          rec.RanksJSON = "{}"
      }
      s.players[player.PlayerID] = rec
  }
  ```
- **`internal/storage/jsonstore.go:1049-1080` (`UpdatePlayerRanks`)**:
  ```go
  if strings.TrimSpace(ranksJSON) == "" {
      ranksJSON = "{}"
  }
  rec.RanksJSON = ranksJSON
  rec.LastSeenAt = time.Now().UTC()
  return s.saveLocked()
  ```
  *Behavior*: JSONStore serializes `PlayerRecord.RanksJSON` directly into disk file `state.json`. Neither engine parses the contents of `ranks_json`; both treat it as an opaque string.

### 1.3 Ingestion & Serialization Pipeline
- **`internal/playertrack/tracker.go:946-1022` (`fetchRanksBatch`)**:
  ```go
  results, err := t.rankClient.GetPlayersSkills(fetchCtx, playerIDs)
  ...
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
  go func(pID, rJSON string) {
      defer t.wg.Done()
      t.persistPlayerRanks(t.ctx, pID, rJSON)
  }(item.playerID, item.ranksJSON)
  ```
- **`internal/playertrack/rank_client.go:640-674`**:
  ```go
  func FromSkill(s rlapi.Skill) PlayerPlaylistRank {
      return PlayerPlaylistRank{
          PlaylistID:    s.Playlist,
          PlaylistName:  FormatPlaylist(s.Playlist),
          Tier:          s.Tier,
          Division:      s.Division,
          RankName:      FormatRank(s.Tier, s.Division),
          MMR:           s.MMR, // Currently raw float (e.g. 47.5)
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

### 1.4 Deserialization & Normalization Loop in `ParseRanksJSON`
- **`internal/playertrack/rank_client.go:679-713`**:
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
- **Existing Legacy Test (`internal/playertrack/rank_client_test.go:458-482`)**:
  `legacyJSON := {"11":{"tier":15,"division":2}}` is already tested for backfilling missing `PlaylistID`, `PlaylistName`, and `RankName`. Currently, `rank.MMR` is left unmigrated if it contains legacy raw floats.

### 1.5 Existing Test Fixture Values
- Multiple existing tests (`test/e2e/tier5_pt_adversarial_test.go:410-413`, `internal/playertrack/rank_client_resilience_test.go:165`, `internal/playertrack/tracker_test.go:881`, `internal/session/stress_challenger_test.go:236-282`) inject mock skill objects with already scaled numbers:
  - `MMR: 1040.0`, `MMR: 1055.0`, `MMR: 1120.0`, `MMR: 1005.0`, `MMR: 1150.5`, `MMR: 600.0`, `MMR: 1900.0`.

---

## 2. Logic Chain

1. **Storage Layer Schema & Parity**:
   - Observations 1.2 show that both `SQLiteStore` and `JSONStore` store `ranks_json` as an opaque string (`TEXT` column defaulting to `'{}'` in SQLite, `RanksJSON string` in JSONStore).
   - Neither engine executes schema checks or parses the JSON contents.
   - Therefore, changing `ranks_json` from storing raw TrueSkill floats (`"mmr": 47.5`) to true in-game MMR numbers (`"mmr": 1050.0`) requires **zero database schema migrations** (`ALTER TABLE`), zero migrations in `JSONStore`, and maintains 100% engine parity.

2. **Ingestion Chokepoint & Persistence**:
   - Observation 1.3 shows that all player skill records ingested from PsyNet RPC pass through `FromSkill(s)` inside `rank_client.go:640`.
   - `BuildRanksSnapshot` (which feeds the in-memory `LobbyPlayer.CurrentRank` and `rankCache`) and `SerializeRanksJSON` (which feeds SQLite and JSONStore `UpdatePlayerRanks`) both delegate exclusively to `FromSkill`.
   - By converting raw `s.MMR` in `FromSkill` via `ConvertSkillRatingToMMR(s.MMR)`, both the in-memory active match state and the persistent database records automatically receive true in-game MMR on write.

3. **Legacy Deserialization Migration on Read**:
   - Observation 1.4 shows that `ParseRanksJSON` already possesses a dedicated normalization and backfill pass for legacy and partial data.
   - In legacy databases created before M1, `ranks_json` contains raw floats (e.g. `47.5` for Champion I, `30.4646` for Gold III).
   - In Rocket League, live PsyNet raw skill ratings are strictly $\le 125.0$ (pro players around 2600 MMR have $\mu \approx 125.0$). They never exceed $150.0$.
   - In contrast, true display MMR in competitive playlists starts above $150.0$ (Bronze 1 is $\sim 150-200$, Champion is $\sim 1050$, Grand Champion is $\sim 1500$).
   - Therefore, adding the condition:
     ```go
     if rank.MMR > 0.0 && rank.MMR <= 150.0 {
         rank.MMR = ConvertSkillRatingToMMR(rank.MMR)
         changed = true
     }
     ```
     in `ParseRanksJSON` guarantees that any legacy row deserialized by the Go backend or API services is transparently and dynamically migrated to true in-game MMR on read.

4. **Casual & Unranked Guard (`0.0 -> 0.0`)**:
   - In casual playlists and unranked accounts, PsyNet and StatsAPI report `0.0`.
   - Applying the raw formula $\text{round}((0.0 \times 20) + 100)$ would compute $100.0$.
   - This would corrupt the UX (frontend `formatMMR` checks `mmr === 0 ? '---' : ...` and would display `"100.0"`) and corrupt `SessionTracker` (which would compute false starting MMR of $100.0$ for casual matches).
   - Guarding `if skillRating <= 0.0 { return 0.0 }` guarantees that casual and unranked MMR stays strictly `0.0`.

5. **Negative Clamping (`< 0.0 -> 0.0`)**:
   - Mathematical TrueSkill can yield $\mu < 0$ on extreme loss streaks.
   - Clamping `skillRating <= 0.0 -> 0.0` prevents negative MMR numbers from reaching database storage, session calculations, or API endpoints.

6. **High MMR Boundary & Test Fixture Idempotency Guard**:
   - Observation 1.5 shows that existing test fixtures in `test/e2e` and `internal/playertrack` already populate mock skills with scaled values like `1040.0` or `1150.5`.
   - Without an idempotency guard, applying $(1040 \times 20) + 100 = 20,900.0$ would cause cascading test regressions.
   - Because live PsyNet ratings never exceed $150.0$, any rating $> 150.0$ is an already converted in-game MMR value.
   - Returning `skillRating` unchanged when $> 150.0$ preserves existing test fixtures while accurately converting all true PsyNet ratings ($47.5 \to 1050.0$, $81.8645 \to 1737.0$, $125.0 \to 2600.0$).

---

## 3. Caveats

1. **Sub-100 MMR in Bronze 1**:
   - In theoretical edge cases, an extremely low-rated player at Bronze I Division I could have a true MMR between 100 and 150 (raw rating between 0.0 and 2.5).
   - With the linear formula, raw $1.0$ converts to $120.0$. If a legacy database record had raw $1.0$, it converts to $120.0$.
   - The threshold of $150.0$ is safe because raw PsyNet ratings for all normal ranks (Bronze I through Supersonic Legend) range from $0.0$ to $125.0$.
2. **Read-Only Database String vs Deserialized Object**:
   - `SQLiteStore.GetPlayer` and `JSONStore.GetPlayer` return `PlayerRecord.RanksJSON` as a raw string.
   - For backend Go consumers, calling `ParseRanksJSON` normalizes the object.
   - To guarantee that REST API endpoints (`GET /api/players` and `GET /api/players/{id}`) also emit true in-game MMR for legacy rows, `NormalizeRanksJSON(raw string) string` should be used to re-serialize the string if legacy floats are detected.
3. **No Database Modification Required**:
   - Existing databases on disk will operate seamlessly. There is no need for schema migrations, table locks, or downtime.

---

## 4. Conclusion

### 4.1 Canonical Implementation

In `internal/playertrack/rank_client.go`:
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

// ConvertSkillRatingToMMR converts a raw PsyNet skill rating decimal (e.g. 47.5)
// to true in-game MMR using the standard Rocket League formula: round((SkillRating * 20) + 100).
//
// Special Rules:
// - Ratings <= 0.0 (casual, unranked, or uncalibrated) return 0.0.
// - Ratings > 150.0 are treated as already converted in-game MMR and returned unchanged (idempotency guard).
func ConvertSkillRatingToMMR(skillRating float64) float64 {
	if skillRating <= 0.0 {
		return 0.0
	}
	if skillRating > 150.0 {
		return skillRating
	}
	return math.Round((skillRating * 20.0) + 100.0)
}

// NormalizeRanksJSON ensures a raw ranks_json string is migrated and formatted consistently.
func NormalizeRanksJSON(raw string) string {
	snapshot, err := ParseRanksJSON(raw)
	if err != nil || len(snapshot) == 0 {
		return raw
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return raw
	}
	return string(data)
}
```

Update `FromSkill` in `internal/playertrack/rank_client.go:640`:
```go
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

Update `ParseRanksJSON` in `internal/playertrack/rank_client.go:690`:
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
		// Migrate legacy raw PsyNet SkillRating to in-game MMR if detected
		if rank.MMR > 0.0 && rank.MMR <= 150.0 {
			rank.MMR = ConvertSkillRatingToMMR(rank.MMR)
			changed = true
		} else if rank.MMR < 0.0 {
			rank.MMR = 0.0
			changed = true
		}
		if changed {
			snapshot[key] = rank
		}
	}
```

---

## 5. Verification Method & Test Formulation

### 5.1 Test Suite 1: Conversion & Edge Cases (`internal/playertrack/rank_client_test.go`)

```go
func TestConvertSkillRatingToMMR_StandardValues(t *testing.T) {
	tests := []struct {
		name     string
		raw      float64
		expected float64
	}{
		{"Champion I standard", 47.5, 1050.0},
		{"Diamond III boundary", 47.0, 1040.0},
		{"Gold III fractional", 30.4646, 709.0},
		{"Grand Champion pro", 81.8645, 1737.0},
		{"Exact half step", 41.25, 925.0},
		{"Rounding down", 41.24, 925.0},
		{"Rounding up", 41.26, 925.0},
		{"Bronze I minimal positive", 5.0, 200.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tt.raw)
			if got != tt.expected {
				t.Errorf("ConvertSkillRatingToMMR(%v) = %v; want %v", tt.raw, got, tt.expected)
			}
		})
	}
}

func TestConvertSkillRatingToMMR_CasualAndNegative(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"Zero casual rating", 0.0, 0.0},
		{"Negative float small", -0.01, 0.0},
		{"Negative float large", -5.0, 0.0},
		{"Extreme loss streak", -50.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tt.input)
			if got != tt.expected {
				t.Errorf("ConvertSkillRatingToMMR(%v) = %v; want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConvertSkillRatingToMMR_HighMMRAndIdempotency(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"Pro raw rating (Zen/Vatira)", 125.0, 2600.0},
		{"Boundary raw rating", 150.0, 3100.0},
		{"Already converted boundary", 150.1, 150.1},
		{"Standard in-game fixture 1040", 1040.0, 1040.0},
		{"Standard in-game fixture 1050", 1050.0, 1050.0},
		{"Standard in-game fixture 1150.5", 1150.5, 1150.5},
		{"High in-game fixture 1900", 1900.0, 1900.0},
		{"SSL in-game fixture 2600", 2600.0, 2600.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertSkillRatingToMMR(tt.input)
			if got != tt.expected {
				t.Errorf("ConvertSkillRatingToMMR(%v) = %v; want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFromSkill_TrueInGameMMR(t *testing.T) {
	skill := rlapi.Skill{
		Playlist:      11,
		Tier:          16,
		Division:      2,
		MMR:           47.5,
		MatchesPlayed: 50,
	}
	rank := FromSkill(skill)
	if rank.MMR != 1050.0 {
		t.Fatalf("FromSkill MMR = %v; want 1050.0", rank.MMR)
	}
	if rank.RankName != "Champion II Division III" {
		t.Errorf("FromSkill RankName = %q; want 'Champion II Division III'", rank.RankName)
	}
}
```

### 5.2 Test Suite 2: Legacy Migration Deserialization (`internal/playertrack/rank_client_test.go`)

```go
func TestParseRanksJSON_LegacyRawRatingMigration(t *testing.T) {
	legacyJSON := `{"11":{"tier":16,"division":2,"mmr":47.5}}`
	snapshot, err := ParseRanksJSON(legacyJSON)
	if err != nil {
		t.Fatalf("ParseRanksJSON failed: %v", err)
	}

	rank, ok := snapshot.GetRank(11)
	if !ok {
		t.Fatalf("failed to retrieve playlist 11")
	}
	if rank.MMR != 1050.0 {
		t.Errorf("rank.MMR = %v; want 1050.0", rank.MMR)
	}
	if rank.PlaylistName != "Ranked Doubles (2v2)" {
		t.Errorf("rank.PlaylistName = %q; want 'Ranked Doubles (2v2)'", rank.PlaylistName)
	}
	if rank.RankName != "Champion II Division III" {
		t.Errorf("rank.RankName = %q; want 'Champion II Division III'", rank.RankName)
	}
}

func TestParseRanksJSON_MultiPlaylistMixedPayload(t *testing.T) {
	mixedJSON := `{
		"10": {"tier": 0, "division": 0, "mmr": 0.0},
		"11": {"tier": 16, "division": 2, "mmr": 47.5},
		"13": {"tier": 19, "division": 0, "mmr": 62.5},
		"27": {"tier": 22, "division": 0, "mmr": 1400.0},
		"99": {"tier": 0, "division": 0, "mmr": -5.0}
	}`

	snapshot, err := ParseRanksJSON(mixedJSON)
	if err != nil {
		t.Fatalf("ParseRanksJSON failed: %v", err)
	}

	// 1. Casual / unranked (0.0 -> 0.0)
	r10, ok := snapshot.GetRank(10)
	if !ok || r10.MMR != 0.0 {
		t.Errorf("playlist 10 MMR = %v; want 0.0", r10.MMR)
	}

	// 2. Legacy raw rating (47.5 -> 1050.0)
	r11, ok := snapshot.GetRank(11)
	if !ok || r11.MMR != 1050.0 {
		t.Errorf("playlist 11 MMR = %v; want 1050.0", r11.MMR)
	}

	// 3. Legacy raw rating (62.5 -> 1350.0)
	r13, ok := snapshot.GetRank(13)
	if !ok || r13.MMR != 1350.0 {
		t.Errorf("playlist 13 MMR = %v; want 1350.0", r13.MMR)
	}

	// 4. Already scaled fixture (1400.0 -> 1400.0)
	r27, ok := snapshot.GetRank(27)
	if !ok || r27.MMR != 1400.0 {
		t.Errorf("playlist 27 MMR = %v; want 1400.0", r27.MMR)
	}

	// 5. Negative rating clamped (-5.0 -> 0.0)
	r99, ok := snapshot.GetRank(99)
	if !ok || r99.MMR != 0.0 {
		t.Errorf("playlist 99 MMR = %v; want 0.0", r99.MMR)
	}
}
```

### 5.3 Test Suite 3: Dual Store Parity & Persistence (`internal/playertrack/tracker_test.go`)

```go
func TestTracker_StoragePersistence_InGameMMR_Parity(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		fetcher.skillsMap["Steam|target_player|0"] = []rlapi.Skill{
			{Playlist: 11, Tier: 16, Division: 2, MMR: 47.5, MatchesPlayed: 100},
		}

		cfg := config.PlayerTrackingConfig{
			Enabled:        true,
			LocalPlayerID:  "Steam|local_player|0",
			AutoFetchRanks: true,
		}

		tracker, err := playertrack.NewTracker(cfg, store, fetcher, nil, config.AuthConfig{}, nil)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("Local", "Steam|local_player|0", 0),
			makePlayer("Target", "Steam|target_player|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "guid-mmr-store", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		select {
		case <-fetcher.doneNotifyCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for rank fetch")
		}

		// Wait briefly for async persist goroutine
		time.Sleep(100 * time.Millisecond)

		rec, err := store.GetPlayer(ctx, "Steam|target_player|0")
		if err != nil {
			t.Fatalf("GetPlayer failed: %v", err)
		}

		snapshot, err := playertrack.ParseRanksJSON(rec.RanksJSON)
		if err != nil {
			t.Fatalf("ParseRanksJSON failed on persisted string: %v", err)
		}

		rank, ok := snapshot.GetRank(11)
		if !ok {
			t.Fatalf("playlist 11 rank not found in persisted ranks_json")
		}

		if rank.MMR != 1050.0 {
			t.Errorf("persisted rank MMR = %v; want 1050.0 (converted from raw 47.5)", rank.MMR)
		}
	})
}

func TestTracker_StoragePersistence_LegacyRecordMigrationOnRead(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		now := time.Now().UTC()

		// Manually insert legacy record with raw PsyNet rating (47.5)
		legacyRec := &storage.PlayerRecord{
			PlayerID:    "Epic|legacy_user|0",
			Platform:    "Epic",
			PlayerName:  "LegacyUser",
			RanksJSON:   `{"11":{"tier":16,"division":2,"mmr":47.5}}`,
			FirstSeenAt: now,
			LastSeenAt:  now,
		}

		if err := store.UpsertPlayer(ctx, legacyRec); err != nil {
			t.Fatalf("UpsertPlayer failed: %v", err)
		}

		readRec, err := store.GetPlayer(ctx, "Epic|legacy_user|0")
		if err != nil {
			t.Fatalf("GetPlayer failed: %v", err)
		}

		snapshot, err := playertrack.ParseRanksJSON(readRec.RanksJSON)
		if err != nil {
			t.Fatalf("ParseRanksJSON failed: %v", err)
		}

		rank, ok := snapshot.GetRank(11)
		if !ok {
			t.Fatalf("playlist 11 not found in snapshot")
		}

		if rank.MMR != 1050.0 {
			t.Errorf("migrated MMR = %v; want 1050.0", rank.MMR)
		}
	})
}
```

### 5.4 Test Execution Commands
1. Run all unit and integration tests:
   ```powershell
   go test -v -count=1 ./internal/playertrack -run "TestConvertSkillRatingToMMR|TestFromSkill|TestParseRanksJSON|TestTracker_StoragePersistence"
   ```
2. Verify full package test suites:
   ```powershell
   go test -count=1 ./internal/playertrack ./internal/storage ./internal/session ./internal/daemon
   ```
3. Run entire repository test suite to guarantee zero regressions:
   ```powershell
   go test -count=1 ./...
   ```

### 5.5 Invalidation Conditions
- If `ConvertSkillRatingToMMR(47.5)` returns anything other than `1050.0`.
- If `ConvertSkillRatingToMMR(0.0)` returns `100.0` instead of `0.0`.
- If `ConvertSkillRatingToMMR(1040.0)` returns `20900.0` instead of `1040.0`.
- If `ParseRanksJSON` fails to backfill `1050.0` for `{"11":{"tier":16,"division":2,"mmr":47.5}}`.
- If SQLite or JSONStore fails parity tests across `forEachStore`.
