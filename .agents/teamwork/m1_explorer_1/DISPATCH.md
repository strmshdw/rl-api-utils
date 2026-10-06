# DISPATCH: m1_explorer_1

## Objective
Investigate the precise implementation details for Milestone M1 (Requirement R2: Persistent Player State on Mid-Game Disconnect) focusing on `internal/playertrack/tracker.go`.

## Scope Boundaries
- Read-only technical investigation. Do NOT edit source files.
- Deliver `handoff.md` to `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\handoff.md`.

## Context & Inputs
- Authoritative User Request: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-10-06T08:30:09Z`)
- Project Architecture & Milestones: `d:\code\rl-api-utils\PROJECT.md`
- Survey report on player state: `d:\code\rl-api-utils\.agents\teamwork\survey_explorer_state_1\handoff.md`
- Codebase: `internal/playertrack/tracker.go`, `internal/playertrack/models.go`

## Specific Tasks
1. Analyze the exact retention algorithm for `Tracker.OnUpdateState`:
   - How to track participant history within the active `matchGUID`.
   - How to retain players who leave early with their accumulated stats (`Score`, `Goals`, `Assists`, `Saves`, `Shots`, `Demos`).
   - Adding `IsDisconnected bool json:"is_disconnected,omitempty"` to `LobbyPlayer`.
   - Preserving `LocalPlayer` and `LocalTeam` if local player disconnects or is omitted.
   - Handling reconnections (player reappears: update stats, set `IsDisconnected = false`, prevent duplicates).
   - Resetting retention cleanly when transitioning to a new match GUID.
2. Provide concrete, exact code changes/diffs for `internal/playertrack/tracker.go`.
3. Highlight edge cases (splitscreen players, bot replacements, thread-safety under `t.mu`).


## 2026-10-06T08:48:03Z
[Message] sender=f26416a7-29be-4b99-8406-d28bf983644d priority=MESSAGE_PRIORITY_HIGH
Investigate the exact differential retention algorithm for internal/playertrack/tracker.go:
- OnUpdateState participant retention, IsDisconnected flag, stats preservation, reconnection without duplicate rows, local player fallback, and match GUID scoping.
Produce a comprehensive handoff report at: d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\handoff.md.
When finished, send a completion message back to your caller (orchestrator_6).
