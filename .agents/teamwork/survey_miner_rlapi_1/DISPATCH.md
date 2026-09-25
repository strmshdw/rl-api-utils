# Dispatch for Survey Spec Miner: RLAPI & PsyNet

**Target**: Specification & API mining for `github.com/dank/rlapi` and Rocket League PsyNet integration.
**Original Requirements**: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md
**Working Directory**: d:\code\rl-api-utils\.agents\teamwork\survey_miner_rlapi_1

## Objectives
1. Read `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` thoroughly.
2. Investigate `github.com/dank/rlapi` (and online sources / Go docs / web search / git repository if reachable):
   - Package structure of `github.com/dank/rlapi`
   - How `egs.ExchangeEOSToken` works (Epic Games auth)
   - How `psyNet.AuthPlayer` works
   - How `egs.ExchangeEOSTokenFromSteam` works
   - How `psyNet.AuthPlayerSteam` works with Steam session ticket and 64-bit Steam ID
   - How `Matches/GetMatchHistory v1` RPC is constructed, called, and what data structure it returns (Match GUIDs, replay URLs, timestamps, player IDs, etc.)
   - How PsyNet RPC requests/responses are formatted (headers, tokens, session IDs, environment URLs, error handling)
3. Detail how to mock PsyNet and rlapi in automated tests without real credentials.
4. Output your full report to `d:\code\rl-api-utils\.agents\teamwork\survey_miner_rlapi_1\handoff.md`.

## 2026-09-25T02:57:56Z

You are survey_miner_rlapi_1.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\survey_miner_rlapi_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md and d:\code\rl-api-utils\.agents\teamwork\survey_miner_rlapi_1\DISPATCH.md.
25: 
26: Perform comprehensive specification and API mining on github.com/dank/rlapi and Rocket League PsyNet integration:
27: - Package structure and exports of github.com/dank/rlapi
28: - Epic Games auth flow: egs.ExchangeEOSToken and psyNet.AuthPlayer
29: - Steam auth flow: egs.ExchangeEOSTokenFromSteam and psyNet.AuthPlayerSteam with Steam session tickets & Steam ID 64
30: - Matches/GetMatchHistory v1 RPC format, request params, response payload structure (GUIDs, replay URLs, timestamps, metadata)
31: - How PsyNet RPC calls work (headers, auth tokens, session IDs, endpoints) and how they can be reliably mocked in tests.
32: Write your detailed report to d:\code\rl-api-utils\.agents\teamwork\survey_miner_rlapi_1\handoff.md. Use send_message to report completion to parent.
