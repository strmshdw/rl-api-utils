# Dispatch for M1 Iteration 2 Explorer 3: Duration UnmarshalYAML Integer Decoding Fix

**Milestone**: M1 - Storage & Configuration (Iteration 2)
**Role**: Explorer (`teamwork_preview_explorer`)
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_3`
**Original Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`
**Project Specification**: `d:\code\rl-api-utils\PROJECT.md`
**Failure Evidence**:
- `d:\code\rl-api-utils\.agents\teamwork\m1_challenger_2\handoff.md`
- `d:\code\rl-api-utils\.agents\teamwork\m1_auditor_1\handoff.md`
- Failed Test: `go test -v -run TestBoundary_DurationStrings ./internal/config/...`

## Objectives
1. Read `ORIGINAL_REQUEST.md`, `PROJECT.md`, and the challenger/auditor handoffs.
2. Analyze the defect in `internal/config/config.go` (`Duration.UnmarshalYAML:54-70`):
   - Currently, `value.Decode(&s)` succeeds on integer YAML scalar nodes, converting them to strings without units (e.g. `"300000000000"`), which fails `time.ParseDuration`.
   - Investigate the fix:
     ```go
     func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
         var n int64
         if err := value.Decode(&n); err == nil {
             *d = Duration(time.Duration(n))
             return nil
         }
         var s string
         if err := value.Decode(&s); err == nil {
             parsed, err := time.ParseDuration(s)
             if err != nil {
                 return fmt.Errorf("invalid duration string %q: %w", s, err)
             }
             *d = Duration(parsed)
             return nil
         }
         return fmt.Errorf("cannot unmarshal YAML node into Duration")
     }
     ```
3. Detail the exact patch recommendation and verify that all config tests and boundary tests pass.
4. Output your report to `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_3\handoff.md`.

## 2026-09-25T03:25:13Z
Explore the fix for Duration.UnmarshalYAML decoding integers before strings in internal/config/config.go.
Write your recommendation report to d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_3\handoff.md and notify parent via send_message.
