# Dispatch: m2_explorer_1

**Milestone**: M2 - Auth & PsyNet Integration
**Role**: Auth Explorer (internal/auth)
**Scope**:
Investigate and design `internal/auth` providing authentication for both Epic Games and Steam:
- Interface: `AuthProvider` (`Authenticate(ctx context.Context) (*TokenInfo, error)`, `Refresh(ctx context.Context, refreshToken string) (*TokenInfo, error)`, etc.)
- Epic Games provider (`epic.go`):
  - Uses `rlapi.NewEGS()`
  - Exchange auth code (`AuthenticateWithCode`) or refresh token (`AuthenticateWithRefreshToken`)
  - Obtain exchange code (`GetExchangeCode`)
  - Exchange for EOS token (`ExchangeEOSToken`)
  - Integration with `StateStore.SaveAuthState` / `GetAuthState`
- Steam provider (`steam.go`):
  - Uses `rlapi.NewEGS()`
  - Exchange Steam session ticket (`ExchangeEOSTokenFromSteam`)
  - Steam ID extraction / handling
- Error handling, credential validation, token expiry handling
- Unit tests (`auth_test.go`) covering Epic and Steam flows, mock exchanges, expired tokens, invalid credentials.

Output report: `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1\handoff.md`

## 2026-09-25T03:38:00Z
You are m2_explorer_1.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1\DISPATCH.md.
Also review the survey findings in d:\code\rl-api-utils\.agents\teamwork\survey_miner_rlapi_1\handoff.md.

Explore the authentication subsystem for Milestone 2 (internal/auth):
- Interface: AuthProvider (Authenticate, Refresh, TokenInfo)
- Epic Games implementation (epic.go): EGS client, OAuth exchange, EOS token exchange, refresh token management, persistence with StateStore.SaveAuthState
- Steam implementation (steam.go): Steam ticket exchange for EOS token
- Error handling, credential validation, mockability for tests
- Unit test design (auth_test.go)
Write your report and proposed code to d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1\handoff.md and notify parent via send_message.

