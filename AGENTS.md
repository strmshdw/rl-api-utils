# Autonomous Engineering Harness (`v2.0`)

### 1. Core Architectural & Execution Model
- **Declarative Schema Single Source of Truth (`INV-SCHEMA-SSOT`)**:
  All shared cross-boundary data structures and named field/slice views are defined in a single declarative schema source of truth. Hand-editing generated schema contracts or indexing raw magic offsets/slices outside the schema module is blocked unless scoped by `# @perf-isolation(parity_test="tests/parity/...")` with `What:`, `Why:`, and `How:` rationale.
- **SOLID Bounded Contexts & "Clean Outside, Ruthless Inside" (`INV-BOUNDED-CONTEXT-ISOLATION`)**:
  The codebase is partitioned into cohesive, loosely coupled Bounded Contexts enforcing a strict acyclic dependency DAG and `< 800 LOC` file cohesion (`SOLID-LOC-01`). Public module interfaces expose strongly typed contracts; internal performance-critical hot loops must carry `# @perf-isolation(parity_test="tests/parity/...")` verified on disk by `SOLID-PERF-01`.
- **Hub-and-Spoke Sub-Spec Topology (`INV-SPEC-DECOMPOSITION-30KB`)**:
  Specifications preserve 100% unredacted mathematical, data-contract, and algorithmic precision while decomposing multi-part domains into `~25–32 KB` sub-specs so every `.md` file fits in a single `read_file` call (`< 46,080 B`).
- **Unified Full-Stack TDD & Parallel Wave Ownership (`INV-WAVE-OWNERSHIP-PARTITION`)**:
  Engineers execute `Red -> Green -> Refactor -> Atomic Commit` loops on feature branches (`feat/*`, `fix/*`). Direct commits to `main`/`master` are blocked (`Exit Code 42`). Multi-subsystem waves dispatch specialist subagents with non-overlapping file globs via `define_subagent(EnableWriteTools=True)`.
- **Silent Tool Chaining & Reactive Turn Yielding (`INV-SILENT-TOOL-CHAINING`)**:
  Chain tool calls silently with zero conversational preambles. Never poll background tasks or subagents in a loop; yield the turn and rely on reactive wakeup notifications.

### 2. Fast-Path Dispatch Matrix
| Command / Intent | Primary Persona | Execution Pattern | Verification Gate |
| :--- | :--- | :--- | :--- |
| `/wave` | `parallel-wave-orchestrator` | Parallel multi-agent WBS wave execution across disjoint bounded contexts | Wave barrier 4-Pillar Pre-Merge Gate |
| `/audit` | `adversarial-skeptic` | Clean-room Tabula Rasa mathematical, architectural & Proof-of-Regression (PoR) verification | `NO NEW CONCERNS` + invariant suite |
| `/verify` | `verify-governor` | Full 4-Pillar Pre-Merge Gate | `Exit Code 0` clearance for merge |

### 3. Governance Pillars (SSoT)
@.agents/rules/pillar_01_spec_decomposition_and_dod.md
@.agents/rules/pillar_02_solid_ddd_and_schema_ssot.md
@.agents/rules/pillar_03_unified_tdd_and_parallel_waves.md
