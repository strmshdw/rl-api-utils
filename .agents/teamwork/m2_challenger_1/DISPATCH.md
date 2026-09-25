# Dispatch: m2_challenger_1

**Milestone**: M2 - Auth & PsyNet Integration
**Role**: Auth Challenger (internal/auth)

## Scope
Adversarially challenge and stress-test `internal/auth`:
- Test scenarios:
  - Epic auth: refresh token renewal, auth code exchange, fallback to `StateStore.GetAuthState`, expired token detection via `IsExpired()`.
  - Steam auth: strictly numeric 17-digit SteamID64 validation (boundary tests: 16 digits, 18 digits, letters, special characters, wrong prefix), session ticket exchange.
  - Concurrency: multiple goroutines reading `TokenInfo()` concurrently while `Refresh()` or `Authenticate()` is in progress.
  - Error resilience: canceled context, rate limits, invalid credentials.
- Run tests and provide verdict (APPROVE or CHALLENGE_FAILED) in `d:\code\rl-api-utils\.agents\teamwork\m2_challenger_1\handoff.md` and notify parent via `send_message`.

## 2026-09-25T03:51:30Z
You are m2_challenger_1.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_challenger_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m2_challenger_1\DISPATCH.md.
Worker report is at d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md.

Adversarially challenge internal/auth:
1. Stress-test Epic auth (refresh token vs auth code, fallback to store, expired tokens, token info caching).
2. Stress-test Steam auth (malformed SteamID64, session ticket exchange, ticket renewal error).
3. Test concurrency safety and context cancellation.
4. Run tests and provide your verdict (APPROVE or CHALLENGE_FAILED) in d:\code\rl-api-utils\.agents\teamwork\m2_challenger_1\handoff.md and notify parent via send_message.

