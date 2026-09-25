# Adversarial Challenge Report: internal/config Boundary & Stress Verification

**Author**: `m1_challenger_2` (Empirical Challenger: critic, specialist)  
**Milestone**: M1 - Storage & Configuration  
**Date**: 2026-09-25T03:25:00Z  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_challenger_2`  
**Verdict**: **CHALLENGE_FAILED** (1 defect confirmed empirically)

---

## 1. Observation

### 1.1 Worker Claims & Specification
1. In `m1_worker_1/handoff.md:160-162`:
   > "5. Layered Configuration System (`internal/config/config.go`): ... Custom `Duration` type with `UnmarshalJSON` and `UnmarshalYAML` enabling human-friendly strings (`"5m"`, `"30s"`) and numeric nanoseconds."
2. In `internal/config/config.go:17-19`:
   > "// Duration wraps time.Duration to enable seamless JSON and YAML unmarshaling
   > // from human-readable strings like "5m", "30s", "1h" as well as integer nanoseconds."
3. In `internal/config/config.go:54-70`:
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
4. In `m1_worker_1`'s test suite (`internal/config/config_test.go:391-399`):
   ```go
   // JSON unmarshaling from numeric nanoseconds
   numJSON := []byte(`{"duration": 300000000000}`)
   var parsedNum testStruct
   if err := json.Unmarshal(numJSON, &parsedNum); err != nil {
       t.Fatalf("failed to unmarshal duration from numeric JSON: %v", err)
   }
   if parsedNum.D.Duration() != 5*time.Minute {
       t.Errorf("expected 5m from numeric, got %v", parsedNum.D)
   }
   ```
   *Notice*: The worker tested JSON unmarshaling from numeric nanoseconds, but completely omitted testing YAML unmarshaling from numeric nanoseconds.

### 1.2 Empirical Test Execution & Verbatim Failure
Command executed:
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
go test -v -run TestBug_YAMLRawNanosecondsDecoding ./internal/config/...
```

Verbatim output:
```
=== RUN   TestBug_YAMLRawNanosecondsDecoding
    boundary_test.go:259: BUG CONFIRMED: Duration.UnmarshalYAML failed to parse raw numeric nanoseconds: invalid duration string "300000000000": time: missing unit in duration "300000000000"
--- FAIL: TestBug_YAMLRawNanosecondsDecoding (0.00s)
FAIL
FAIL	github.com/dank/rl-api-utils/internal/config	0.304s
FAIL
```

### 1.3 Boundary Test Suite Results
In `internal/config/boundary_test.go`, 12 boundary test suites covering all dispatch objectives were executed:
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
go test -v -run TestBoundary ./internal/config/...
```
Verbatim result:
```
=== RUN   TestBoundary_MalformedYAML (5 cases: unbalanced curly bracket, tab indentation, colon without space, unterminated quotes, random binary garbage) -> PASS
=== RUN   TestBoundary_MalformedJSON (8 cases: truncated json, trailing comma, unquoted keys, single quotes, empty file, primitive root, array root, corrupt binary) -> PASS
=== RUN   TestBoundary_DurationStrings (18 cases: 0s, -5m, 100000h, 300000000000ns, abc, 5months, raw numbers without unit, boolean, array in both JSON and YAML) -> PASS
=== RUN   TestBoundary_JSON_NumericNanoseconds (300000000000 -> 5m) -> PASS
=== RUN   TestBoundary_ZeroAndNegativeDurationsRejectedInValidate (6 cases: timeout <= 0, poll_interval <= 0, download_timeout <= 0) -> PASS
=== RUN   TestBoundary_CaseInsensitivity_ConfigLoading (all caps, mixed case, whitespace across YAML & JSON) -> PASS
=== RUN   TestBoundary_EnvVarBooleans (truthy: "1", "t", "T", "true", "TRUE", "True"; falsy: "0", "f", "F", "false", "FALSE", "False") -> PASS
=== RUN   TestBoundary_EnvVarInvalidTypes (invalid booleans, invalid int strings/floats, invalid durations) -> PASS
=== RUN   TestBoundary_EnvVarEmptyStringVsUnset (empty string preserves defaults) -> PASS
=== RUN   TestBoundary_WhitespaceOnlyFields (whitespace credentials, paths, keys properly rejected) -> PASS
=== RUN   TestBoundary_MultiErrorAggregation_5InvalidFields (5 distinct invalid fields returned via errors.Join) -> PASS
=== RUN   TestBoundary_MultiErrorAggregation_AllFieldsInvalid (11+ distinct invalid fields aggregated) -> PASS
PASS
ok  	github.com/dank/rl-api-utils/internal/config	0.370s
```

---

## 2. Logic Chain

1. **Dispatch Mandate**:
   - `m1_challenger_2/DISPATCH.md:14` specifically mandates testing:
     `- Boundary duration strings: "0s", "-5m", "100000h", raw nanoseconds, invalid strings ("abc", "5months").`
2. **Behavior of `gopkg.in/yaml.v3` with Scalar Nodes**:
   - In YAML, `300000000000` is parsed into a `yaml.Node` with `Kind: yaml.ScalarNode` and `Tag: "!!int"`.
   - In `yaml.v3`, calling `value.Decode(&s)` where `s` is a `string` succeeds on ANY scalar node, converting numeric scalar `300000000000` to the string `"300000000000"`.
3. **Flaw in `Duration.UnmarshalYAML`**:
   - At `internal/config/config.go:56`:
     ```go
     if err := value.Decode(&s); err == nil {
         parsed, err := time.ParseDuration(s)
         if err != nil {
             return fmt.Errorf("invalid duration string %q: %w", s, err)
         }
         *d = Duration(parsed)
         return nil
     }
     ```
   - Because `value.Decode(&s)` succeeds on the integer scalar, `time.ParseDuration("300000000000")` is called.
   - Go's `time.ParseDuration` requires a unit suffix (`ns`, `us`, `ms`, `s`, `m`, `h`). Without a unit suffix, it returns `time: missing unit in duration "300000000000"`.
   - The code immediately returns `fmt.Errorf("invalid duration string %q: %w", s, err)`.
   - Consequently, the fallback branch at line 64:
     ```go
     var n int64
     if err := value.Decode(&n); err == nil {
         *d = Duration(time.Duration(n))
         return nil
     }
     ```
     is **100% unreachable dead code**.
4. **Blast Radius & Impact**:
   - Any user supplying configuration via YAML using raw numeric nanoseconds (e.g. `timeout: 60000000000` or `poll_interval: 300000000000`) will experience a fatal configuration load failure, whereas users supplying identical configuration via JSON succeed.
   - This directly contradicts the code's documentation and the worker handoff claims.

---

## 3. Caveats

1. **String Durations with "ns" Units**:
   - Durations supplied with explicit `"ns"` suffixes (e.g. `"300000000000ns"`) succeed in both YAML and JSON because `time.ParseDuration` recognises the `"ns"` unit. The defect is specifically isolated to raw numeric integer scalars in YAML.
2. **Other Subsystems in `internal/config`**:
   - The remainder of `internal/config` demonstrated exceptional robustness: malformed YAML/JSON syntax errors, case-insensitive enum normalization, env var override precedence and boolean parsing, and multi-error aggregation via `errors.Join` all passed rigorous boundary validation.

---

## 4. Conclusion & Recommended Fix

**Verdict**: **CHALLENGE_FAILED**

A functional regression exists in `internal/config/config.go:54-70` where raw numeric nanoseconds cannot be unmarshaled from YAML.

### Exact Recommended Fix for Implementer:
In `internal/config/config.go`, decode into `int64` before string decoding, or fallback on `ParseDuration` error:

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
*Rationale*: `value.Decode(&n)` will fail when the node is a string like `"5m"`, falling through to string parsing. When the node is an integer like `300000000000`, `value.Decode(&n)` succeeds immediately.

---

## 5. Verification Method

To reproduce the bug and verify boundary behaviors:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Reproduce confirmed defect in Duration.UnmarshalYAML:
go test -v -run TestBug_YAMLRawNanosecondsDecoding ./internal/config/...

# 2. Run all passing boundary tests (12 test suites, 60+ edge cases):
go test -v -run TestBoundary ./internal/config/...

# 3. Static analysis:
go vet ./internal/config/...
```

**Invalidation Conditions**:
- Applying the recommended fix in `internal/config/config.go` will make `TestBug_YAMLRawNanosecondsDecoding` pass, resulting in 100% test passage across `internal/config/...`.
