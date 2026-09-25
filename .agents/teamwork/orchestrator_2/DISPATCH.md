# Dispatch for Orchestrator Successor (Generation 2)

**Role**: Project Orchestrator (Generation 2)  
**Parent (Sentinel)**: `d253ad5e-a1e2-4999-82f7-597ad4cded8f`  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\orchestrator_2`  
**Predecessor Handoff**: `d:\code\rl-api-utils\.agents\teamwork\orchestrator_1\handoff.md`  
**Master Specification**: `d:\code\rl-api-utils\PROJECT.md`  
**Original Requirements**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`  

## Instructions
Resume work at `d:\code\rl-api-utils\.agents\teamwork\orchestrator_2`.
Read:
- `d:\code\rl-api-utils\.agents\teamwork\orchestrator_1\handoff.md`
- `d:\code\rl-api-utils\.agents\teamwork\orchestrator_1\BRIEFING.md`
- `d:\code\rl-api-utils\PROJECT.md`
- `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`
- `d:\code\rl-api-utils\.agents\teamwork\orchestrator_1\GATE_STATUS.md`

Your parent is `d253ad5e-a1e2-4999-82f7-597ad4cded8f` — use this ID for all escalation, status reporting, and final victory claim (send_message).

Execute the remaining milestones:
1. Complete Milestone 1 Iteration 2:
   - Spawn worker `m1_worker_2` to apply patches from `m1_r2_explorer_1`, `m1_r2_explorer_2`, and `m1_r2_explorer_3`.
   - Spawn 2 Reviewers, 2 Challengers, and 1 Auditor for M1 Iteration 2 Gate.
   - When passed, mark M1 DONE in `PROJECT.md`.
2. Execute Milestone 2 (Auth & PsyNet Integration) and Milestone 3 (Ballchasing Replay Uploader).
3. Execute Milestone 4 (Syncer, Daemon Engine & CLI).
4. Execute Milestone 5 (Run 100% of E2E test suite in `test/e2e/`, then Tier 5 Adversarial Coverage Hardening).
5. Claim victory to Sentinel `d253ad5e-a1e2-4999-82f7-597ad4cded8f`.
