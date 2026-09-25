# Explorer Recommendation Report: Duration.UnmarshalYAML Integer Decoding Fix

**Author**: `m1_r2_explorer_3` (Roles: teamwork_preview_explorer, investigation, synthesis)  
**Milestone**: M1 - Storage & Configuration (Iteration 2)  
**Date**: 2026-09-25T03:29:00Z  
**Target Codebase**: `internal/config/config.go`  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_3`  
**Status**: COMPLETE (Hard Handoff)

---

## 1. Observation

### 1.1 Existing Implementation in `internal/config/config.go`
In `internal/config/config.go:54-70`:
```go
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration string %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}
	var n int64
	if err := value.Decode(&n); err == nil {
		*d = Duration(time.Duration(n))
		return nil
	}
	return fmt.Errorf("cannot unmarshal YAML node into Duration")
}
```

In contrast, `Duration.UnmarshalJSON` in `internal/config/config.go:29-48`:
```go
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch val := v.(type) {
	case float64:
		*d = Duration(time.Duration(val))
		return nil
	case string:
		parsed, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid duration string %q: %w", val, err)
		}
		*d = Duration(parsed)
		return nil
	default:
		return fmt.Errorf("invalid duration type: %T", val)
	}
}
```

### 1.2 Test Defect Reproduction & Verbatim Error
Executing the challenger test in `internal/config/boundary_test.go:250-263`:
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
go test -v -run TestBug_YAMLRawNanosecondsDecoding ./internal/config/...
```

Verbatim tool output:
```
=== RUN   TestBug_YAMLRawNanosecondsDecoding
    boundary_test.go:259: BUG CONFIRMED: Duration.UnmarshalYAML failed to parse raw numeric nanoseconds: invalid duration string "300000000000": time: missing unit in duration "300000000000"
--- FAIL: TestBug_YAMLRawNanosecondsDecoding (0.00s)
FAIL
FAIL	github.com/dank/rl-api-utils/internal/config	0.309s
FAIL
```

### 1.3 `yaml.v3` Decoding Mechanics (Empirical Probe)
A probe script decoding various YAML representations into `int64` and `string` with `gopkg.in/yaml.v3` revealed:

| YAML Input | `yaml.Node.Tag` | `Decode(&n int64)` Result | `Decode(&s string)` Result |
|------------|-----------------|---------------------------|----------------------------|
| `300000000000` | `!!int` | **Success** (`n = 300000000000`) | **Success** (`s = "300000000000"`) |
| `0` | `!!int` | **Success** (`n = 0`) | **Success** (`s = "0"`) |
| `"100"` | `!!str` | **Error**: `cannot unmarshal !!str into int64` | **Success** (`s = "100"`) |
| `"5m"` | `!!str` | **Error**: `cannot unmarshal !!str into int64` | **Success** (`s = "5m"`) |
| `5m` | `!!str` | **Error**: `cannot unmarshal !!str into int64` | **Success** (`s = "5m"`) |
| `"300000000000ns"` | `!!str` | **Error**: `cannot unmarshal !!str into int64` | **Success** (`s = "300000000000ns"`) |
| `true` | `!!bool` | **Error**: `cannot unmarshal !!bool into int64` | **Success** (`s = "true"`) |
| `[1, 2]` | `!!seq` | **Error**: `cannot unmarshal !!seq into int64` | **Error**: `cannot unmarshal !!seq into string` |

---

## 2. Logic Chain

1. **Root Cause Analysis**:
   - In `gopkg.in/yaml.v3`, a scalar node with `Tag: "!!int"` (e.g. `duration: 300000000000`) can be decoded into ANY basic scalar type including `string`. When `value.Decode(&s)` is called, `yaml.v3` populates `s` with the raw digit string `"300000000000"` and returns `nil`.
   - In the existing implementation (`internal/config/config.go:56`), `value.Decode(&s)` is called first. Since it returns `err == nil`, execution immediately enters the block and calls `time.ParseDuration("300000000000")`.
   - Go's `time.ParseDuration` expects durations to contain unit suffixes (e.g., `ns`, `us`, `ms`, `s`, `m`, `h`). Digits without units return `time: missing unit in duration "300000000000"`.
   - Because the code returns immediately upon `time.ParseDuration` failure (`return fmt.Errorf("invalid duration string %q: %w", s, err)`), lines 64-68 (`value.Decode(&n)`) are **100% unreachable dead code** for any numeric literal.

2. **Analysis of Proposed Fix (Decode `int64` First)**:
   - When `value.Decode(&n)` is attempted first:
     1. **Numeric literals** (e.g. `300000000000`, `0`): `value.Decode(&n)` succeeds immediately. `*d = Duration(time.Duration(n))` correctly sets the duration in nanoseconds.
     2. **Quoted numeric strings without units** (e.g. `"100"`): Because the node has tag `!!str`, `yaml.v3` fails to decode it into `int64` (`cannot unmarshal !!str into int64`). Execution falls through to string decoding (`value.Decode(&s)`), where `time.ParseDuration("100")` fails with `missing unit in duration "100"`. This properly preserves rejection of invalid duration strings.
     3. **Human-readable duration strings** (e.g. `"5m"`, `5m`, `"30s"`, `100000h`): `value.Decode(&n)` fails because the string contains non-digits or tag `!!str`. Execution falls through to `value.Decode(&s)` and `time.ParseDuration`, parsing the duration correctly.
     4. **Explicit nanosecond strings** (e.g. `"300000000000ns"`): `value.Decode(&n)` fails. `value.Decode(&s)` and `time.ParseDuration` parse it to 5 minutes.
     5. **Invalid types** (e.g. `[1, 2]`, mappings): Both `value.Decode(&n)` and `value.Decode(&s)` fail, returning `fmt.Errorf("cannot unmarshal YAML node into Duration")`.

3. **Behavioral Equivalence with JSON Unmarshaling**:
   - `Duration.UnmarshalJSON` accepts both numeric numbers (via `float64` in Go's JSON parser) and formatted duration strings (`"5m"`).
   - Reordering `Duration.UnmarshalYAML` ensures YAML and JSON configuration files provide identical semantics and parity.

4. **Empirical Verification of All 13 Test Permutations**:
   All 13 test permutations were verified using an isolated probe runner:
   - `zero_duration` (`"0s"`): PASS (0s)
   - `negative_duration` (`"-5m"`): PASS (-5m)
   - `extreme_large_duration_100000h` (`"100000h"`): PASS (100000h)
   - `raw_nanoseconds_string_with_unit` (`"300000000000ns"`): PASS (5m)
   - `invalid_string_abc` (`"abc"`): PASS (expected error: invalid duration string "abc")
   - `invalid_string_5months` (`"5months"`): PASS (expected error: unknown unit "months")
   - `invalid_string_raw_number_no_units` (`"100"`): PASS (expected error: missing unit in duration "100")
   - `invalid_type_boolean` (`true`): PASS (expected error: invalid duration string "true")
   - `invalid_type_array` (`[1, 2]`): PASS (expected error: cannot unmarshal YAML node into Duration)
   - `raw_nanoseconds_unquoted_int` (`300000000000`): PASS (5m)
   - `unquoted_string_5m` (`5m`): PASS (5m)
   - `quoted_string_5m` (`"5m"`): PASS (5m)
   - `zero_raw_int` (`0`): PASS (0s)

---

## 3. Caveats

1. **Floating Point Literals in YAML**:
   - If an unquoted float literal is provided in YAML without units (e.g. `poll_interval: 3.14`), `yaml.v3` will decode it into `int64` as `3` (3 nanoseconds). However, `config.Validate()` subsequently rejects any `PollInterval` or `Timeout` that is non-positive or less than acceptable thresholds. In JSON, `float64` is similarly converted via `time.Duration(val)`. Standard user configuration files always specify units (e.g. `"5m"`).
2. **Read-Only Scope**:
   - As an explorer agent, no modifications were made directly to `internal/config/config.go`. The patch file `duration_unmarshal_yaml.patch` is provided in this directory for the implementer (`m1_worker_1` / `m1_worker_2`).

---

## 4. Conclusion & Actionable Recommendation

### 4.1 Recommended Implementation
In `internal/config/config.go`, replace lines 54-70 with the following implementation:

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

### 4.2 Patch File Location
The exact unified diff patch is stored at:
`d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_3\duration_unmarshal_yaml.patch`

### 4.3 Recommended Unit Test Addition
In addition to the existing boundary test in `internal/config/boundary_test.go:250`, recommend adding the following test case directly into `TestConfig_DurationCustomType` in `internal/config/config_test.go:413`:

```go
	// YAML unmarshaling from numeric nanoseconds
	numYAML := []byte("duration: 300000000000\n")
	var parsedNumYAML testStruct
	if err := yaml.Unmarshal(numYAML, &parsedNumYAML); err != nil {
		t.Fatalf("failed to unmarshal duration from numeric YAML: %v", err)
	}
	if parsedNumYAML.D.Duration() != 5*time.Minute {
		t.Errorf("expected 5m from numeric YAML, got %v", parsedNumYAML.D)
	}
```

---

## 5. Verification Method

To verify this recommendation and confirm the fix once applied:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Verify the specific bug test passes after patch application:
go test -v -run TestBug_YAMLRawNanosecondsDecoding ./internal/config/...

# 2. Verify all boundary duration string tests continue to pass:
go test -v -run TestBoundary_DurationStrings ./internal/config/...

# 3. Verify all config package unit tests pass:
go test -v ./internal/config/...

# 4. Verify static analysis:
go vet ./internal/config/...
```

**Invalidation Conditions**:
- If `value.Decode(&n)` causes quoted numbers without units (e.g. `"100"`) to succeed instead of failing validation. (Empirically verified that it does NOT: `yaml.v3` rejects decoding `!!str` to `int64`).
- If any existing tests in `internal/config/config_test.go` or `internal/config/boundary_test.go` fail.
