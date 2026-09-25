# Specification & API Mining Report: github.com/dank/rlapi & PsyNet Integration

**Target**: `github.com/dank/rlapi` and Rocket League PsyNet integration  
**Author**: `survey_miner_rlapi_1`  
**Date**: 2026-09-25T03:02:00Z  

---

## 1. Observation

Direct inspection of `github.com/dank/rlapi` (authoritative source cloned from repository commit `HEAD` into temporary specification workspace) revealed the following concrete package structures, constants, endpoints, data types, and protocol implementations:

### 1.1 Package Structure & Module Definition
- **Go Module**: `module github.com/dank/rlapi`, Go version `1.24.5` (`go.mod:1-3`).
- **Dependencies**: Only standard library packages plus `github.com/gorilla/websocket v1.5.3` (`go.mod:5`).
- **Package Layout**:
  - `auth.go`: Player authentication methods (`AuthPlayer`, `AuthPlayerSteam`) and payload models (`AuthPlayerRequest`, `AuthPlayerResponse`).
  - `egs.go`: Epic Games Store & Epic Online Services client (`EGS`), OAuth 2.0 flows, device authorization grant, token exchange (`ExchangeEOSToken`, `ExchangeEOSTokenFromSteam`), and revocation.
  - `matches.go`: Match history retrieval (`GetMatchHistory`), match metadata structures (`Match`, `MatchPlayer`, `MatchSkills`, `MatchEntry`), and request/response models.
  - `playerid.go`: Composite player identifier abstraction (`PlayerID`), platform enumeration (`Platform`), formatting helper (`NewPlayerID`), and parser (`ParsePlayerID`).
  - `psynet.go`: HTTP bootstrapping client (`PsyNet`), HMAC-SHA256 request signature generation (`generatePsySig`), build ID computation (`decodeBuildID`), and WebSocket dialer (`establishSocket`).
  - `psynetrpc.go`: Persistent WebSocket client (`PsyNetRPC`), text message framing parser (`parseMessage`), message builder (`buildMessage`), keep-alive heartbeats (`schedulePing`, `sendPing`), concurrent multiplexer (`sendRequestAsync`, `awaitResponse`), and event streaming (`Events`).
  - `requestid.go`: Thread-safe atomic counter for request message sequence tracking (`requestIDCounter`).
  - `buildid.go`: Big-endian CRC-32 calculation for `PsyBuildID` header generation.
  - Subsystems: `challenges.go`, `clubs.go`, `matchmaking.go`, `misc.go`, `mtx.go`, `party.go`, `players.go`, `playlists.go`, `population.go`, `products.go`, `rocketpass.go`, `shops.go`, `skills.go`, `stats.go`, `tournaments.go`, `training.go`.
  - Reference Test Harness: `psynetrpc_test.go` (contains `MockWSServer` implementation using `httptest.Server` and `gorilla/websocket`).

### 1.2 Authoritative Constants & Endpoints
From `egs.go:14-22`:
```go
const (
	egsUserAgent    = "UELauncher/11.0.1-14907503+++Portal+Release-Live Windows/10.0.19041.1.256.64bit"
	egsClientID     = "34a02cf8f4414e29b15921876da36f9a"
	egsClientSecret = "daafbccc737745039dffe53d94fc76cf"
	egsOAuthURL     = "account-public-service-prod03.ol.epicgames.com"
	eosDeploymentID = "da32ae9c12ae40e8a112c52e1f17f3ba" // Rocket League
	eosClientID     = "xyza7891p5D7s9R6Gm6moTHWGloerp7B"
	eosSecret       = "Knh18du4NVlFs+3uQ+ZPpDCVto0WYf4yXP8+OcwVt1o"
)
```

From `psynet.go:20-28`:
```go
const (
	baseURL      = "https://api.rlpp.psynet.gg/rpc"
	gameVersion  = "260918.75141.528314"
	featureSet   = "PrimeUpdate60"
	buildSecret  = "ce31914a39dbf2f3ab13ea54d00b7fe87bc5473a2f04707d78de54039ded8951"
	psySigKey    = "c338bd36fb8c42b1a431d30add939fc7"
	pingInterval = 20 * time.Second
	pongTimeout  = 10 * time.Second
)
```

### 1.3 Exact Signatures and Type Definitions

#### 1.3.1 EGS & EOS Client (`egs.go`)
```go
type TokenResponse struct {
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	ExpiresIn      int    `json:"expires_in"`
	ExpiresAt      string `json:"expires_at"`
	TokenType      string `json:"token_type"`
	ClientID       string `json:"client_id"`
	InternalClient bool   `json:"internal_client"`
	ClientService  string `json:"client_service"`
	AccountID      string `json:"account_id"`
	DisplayName    string `json:"displayName"`
	App            string `json:"app"`
	InAppID        string `json:"in_app_id"`
	DeviceID       string `json:"device_id"`
}

type EOSTokenResponse struct {
	AccessToken       string   `json:"access_token"`
	RefreshToken      string   `json:"refresh_token"`
	IDToken           string   `json:"id_token"`
	ExpiresIn         int      `json:"expires_in"`
	ExpiresAt         string   `json:"expires_at"`
	RefreshExpiresIn  int      `json:"refresh_expires_in"`
	RefreshExpiresAt  string   `json:"refresh_expires_at"`
	TokenType         string   `json:"token_type"`
	Scope             string   `json:"scope"`
	ClientID          string   `json:"client_id"`
	ApplicationID     string   `json:"application_id"`
	AccountID         string   `json:"account_id"`
	SelectedAccountID string   `json:"selected_account_id"`
	MergedAccounts    []string `json:"merged_accounts"`
	ACR               string   `json:"acr"`
	AuthTime          string   `json:"auth_time"`
}

func NewEGS() *EGS
func (e *EGS) GetAuthURL() string
func (e *EGS) AuthenticateWithCode(authCode string) (*TokenResponse, error)
func (e *EGS) AuthenticateWithRefreshToken(refreshToken string) (*TokenResponse, error)
func (e *EGS) GetExchangeCode(accessToken string) (string, error)
func (e *EGS) ExchangeEOSToken(exchangeCode string) (*EOSTokenResponse, error)
func (e *EGS) ExchangeEOSTokenFromSteam(steamTicket string) (*EOSTokenResponse, error)
func (e *EGS) RefreshEOSToken(refreshToken string) (*EOSTokenResponse, error)
func (e *EGS) RevokeEOSToken(accessToken string) error
func (e *EGS) AuthenticateWithDevice() (*DeviceAuthResponse, error)
func (e *EGS) WaitForDeviceAuthorization(device *DeviceAuthResponse) (*EOSTokenResponse, error)
```

#### 1.3.2 PsyNet HTTP Bootstrap Client (`psynet.go`, `auth.go`)
```go
type AuthPlayerRequest struct {
	Platform            string `json:"Platform"`
	PlayerName          string `json:"PlayerName"`
	PlayerID            string `json:"PlayerID"`
	Language            string `json:"Language"`
	AuthTicket          string `json:"AuthTicket"`
	BuildRegion         string `json:"BuildRegion"`
	FeatureSet          string `json:"FeatureSet"`
	Device              string `json:"Device"`
	LocalFirstPlayerID  string `json:"LocalFirstPlayerID"`
	SkipAuth            bool   `json:"bSkipAuth"`
	SetAsPrimaryAccount bool   `json:"bSetAsPrimaryAccount"`
	EpicAuthTicket      string `json:"EpicAuthTicket"`
	EpicAccountID       string `json:"EpicAccountID"`
}

type AuthPlayerResponse struct {
	IsLastChanceAuthBan bool     `json:"IsLastChanceAuthBan"`
	SessionID           string   `json:"SessionID"`
	VerifiedPlayerName  string   `json:"VerifiedPlayerName"`
	UseWebSocket        bool     `json:"UseWebSocket"`
	PerConURL           string   `json:"PerConURL"`
	PerConURLv2         string   `json:"PerConURLv2"`
	PsyToken            string   `json:"PsyToken"`
	CountryRestrictions []string `json:"CountryRestrictions"`
}

func NewPsyNet() *PsyNet
func (p *PsyNet) SetLogger(logger *slog.Logger)
func (p *PsyNet) SetVersion(gameVersion, featureSet string)
func (p *PsyNet) GetVersion() (gameVersion, featureSet string)
func (p *PsyNet) AuthPlayer(authToken string, accountID string, accountName string) (*PsyNetRPC, error)
func (p *PsyNet) AuthPlayerSteam(authToken string, epicAccountID string, steamAccountID string, accountName string) (*PsyNetRPC, error)
```

#### 1.3.3 Player ID Specification (`playerid.go`)
```go
type PlayerID string
type Platform string

const (
	PlatformEpic   Platform = "Epic"
	PlatformSteam  Platform = "Steam"
	PlatformPS4    Platform = "PS4"
	PlatformXbox   Platform = "XboxOne"
	PlatformSwitch Platform = "Switch"
)

func NewPlayerID(platform Platform, id string) PlayerID // Returns "Platform|id|0"
func ParsePlayerID(playerID string) (platform Platform, id string, err error)
```

#### 1.3.4 Matches & Replay Synchronization (`matches.go`)
```go
type MatchEntry struct {
	ReplayUrl string `json:"ReplayUrl"`
	Match     Match  `json:"Match"`
}

type Match struct {
	MatchGUID                  string        `json:"MatchGUID"`
	RecordStartTimestamp       int64         `json:"RecordStartTimestamp"`
	MapName                    string        `json:"MapName"`
	Playlist                   int           `json:"Playlist"`
	SecondsPlayed              float64       `json:"SecondsPlayed"`
	OvertimeSecondsPlayed      float64       `json:"OvertimeSecondsPlayed"`
	WinningTeam                int           `json:"WinningTeam"`
	Team0Score                 int           `json:"Team0Score"`
	Team1Score                 int           `json:"Team1Score"`
	OverTime                   bool          `json:"bOverTime"`
	NoContest                  bool          `json:"bNoContest"`
	Forfeit                    bool          `json:"bForfeit"`
	CustomMatchCreatorPlayerID string        `json:"CustomMatchCreatorPlayerID,omitempty"`
	ClubVsClub                 bool          `json:"bClubVsClub"`
	Mutators                   []string      `json:"Mutators"`
	Players                    []MatchPlayer `json:"Players"`
}

type MatchPlayer struct {
	PlayerID         string      `json:"PlayerID"`
	PlayerName       string      `json:"PlayerName"`
	ConnectTimestamp int64       `json:"ConnectTimestamp"`
	JoinTimestamp    int64       `json:"JoinTimestamp"`
	LeaveTimestamp   int64       `json:"LeaveTimestamp"`
	PartyLeaderID    string      `json:"PartyLeaderID"`
	InParty          bool        `json:"InParty"`
	Abandoned        bool        `json:"bAbandoned"`
	MVP              bool        `json:"bMvp"`
	LastTeam         int         `json:"LastTeam"`
	TeamColor        string      `json:"TeamColor"`
	SecondsPlayed    float64     `json:"SecondsPlayed"`
	Score            int         `json:"Score"`
	Goals            int         `json:"Goals"`
	Assists          int         `json:"Assists"`
	Saves            int         `json:"Saves"`
	Shots            int         `json:"Shots"`
	Demolishes       int         `json:"Demolishes"`
	OwnGoals         int         `json:"OwnGoals"`
	Skills           MatchSkills `json:"Skills"`
}

type MatchSkills struct {
	Mu           float64 `json:"Mu"`
	Sigma        float64 `json:"Sigma"`
	Tier         int     `json:"Tier"`
	Division     int     `json:"Division"`
	PrevMu       float64 `json:"PrevMu"`
	PrevSigma    float64 `json:"PrevSigma"`
	PrevTier     int     `json:"PrevTier"`
	PrevDivision int     `json:"PrevDivision"`
	Valid        bool    `json:"bValid"`
}

type GetMatchHistoryRequest struct {
	PlayerID PlayerID `json:"PlayerID"`
}

type GetMatchHistoryResponse struct {
	Matches []MatchEntry `json:"Matches"`
}

func (p *PsyNetRPC) GetMatchHistory(ctx context.Context) ([]MatchEntry, error)
```

---

## 2. Logic Chain

From these direct observations, we trace the step-by-step logic governing authentication, RPC communication, replay downloading, and mock testing:

### 2.1 Epic Games Authentication Pipeline
1. **Initial Grant**: The caller executes either `egs.AuthenticateWithCode(code)` (using the web redirect from `egs.GetAuthURL()`) or `egs.AuthenticateWithRefreshToken(refreshToken)` (`egs.go:87-101`).
   - HTTP Target: `POST https://account-public-service-prod03.ol.epicgames.com/account/api/oauth/token`
   - Headers: `Authorization: Basic base64(34a02cf8f4414e29b15921876da36f9a:daafbccc737745039dffe53d94fc76cf)`, `Content-Type: application/x-www-form-urlencoded`.
   - Result: Yields `*TokenResponse` with `AccessToken`, `RefreshToken`, and `AccountID`.
   - *State Persistence Implication*: The returned `RefreshToken` should be stored in the daemon's state database or persistent config immediately so future restarts can reconnect without manual login intervention.
2. **Exchange Code Acquisition**: Call `egs.GetExchangeCode(tokenResp.AccessToken)` (`egs.go:151-183`).
   - HTTP Target: `GET https://account-public-service-prod03.ol.epicgames.com/account/api/oauth/exchange`
   - Headers: `Authorization: bearer <tokenResp.AccessToken>`.
   - Result: Yields exchange code string.
3. **EOS Token Exchange**: Call `egs.ExchangeEOSToken(exchangeCode)` (`egs.go:186-191, 210-248`).
   - HTTP Target: `POST https://api.epicgames.dev/epic/oauth/v2/token`
   - Body Form: `grant_type=exchange_code&exchange_code=<code_str>&deployment_id=da32ae9c12ae40e8a112c52e1f17f3ba&scope=basic_profile`
   - Headers: `Authorization: Basic base64(eosClientID:eosSecret)`
   - Result: Yields `*EOSTokenResponse` with EOS `AccessToken`.
4. **PsyNet Player Auth & WebSocket Handshake**: Call `psyNet.AuthPlayer(eosToken.AccessToken, tokenResp.AccountID, tokenResp.DisplayName)` (`auth.go:33-66`).
   - Generates local `PlayerID`: `NewPlayerID(PlatformEpic, accountID)` -> `"Epic|<accountID>|0"`.
   - Sends HTTP POST to `https://api.rlpp.psynet.gg/rpc/Auth/AuthPlayer/v2` containing signed JSON `AuthPlayerRequest`.
   - Receives `AuthPlayerResponse` containing `PerConURLv2` (`wss://...`), `PsyToken`, and `SessionID`.
   - Immediately dials the WebSocket at `PerConURLv2` sending custom handshake headers:
     ```
     PsyBuildID: <buildID>
     User-Agent: RL Win/<gameVersion> gzip
     PsyEnvironment: Prod
     PsyToken: <res.PsyToken>
     PsySessionID: <res.SessionID>
     ```
   - Launches background goroutine `rpc.readMessages()` and schedules ping timer `rpc.schedulePing()`.
   - Returns connected `*PsyNetRPC`.

### 2.2 Steam Authentication Pipeline
1. **Steam Session Ticket**: Generated externally via Steamworks API or Node.js `steam-user` as a hex ticket string; paired with the 64-bit Steam ID (`steamID64`).
2. **EOS Exchange from Steam**: Call `egs.ExchangeEOSTokenFromSteam(ticket)` (`egs.go:194-200, 210-248`).
   - HTTP Target: `POST https://api.epicgames.dev/epic/oauth/v2/token`
   - Body Form: `grant_type=external_auth&external_auth_type=steam_session_ticket&external_auth_token=<ticket>&deployment_id=da32ae9c12ae40e8a112c52e1f17f3ba&scope=basic_profile`
   - Result: Yields `*EOSTokenResponse` where `AccessToken` is the EOS access token and `AccountID` is the linked Epic Account ID.
3. **PsyNet AuthPlayerSteam**: Call `psyNet.AuthPlayerSteam(eosToken.AccessToken, eosToken.AccountID, steamID64, accountName)` (`auth.go:69-101`).
   - Generates local `PlayerID`: `NewPlayerID(PlatformSteam, steamAccountID)` -> `"Steam|<steamID64>|0"`.
   - Sends HTTP POST to `https://api.rlpp.psynet.gg/rpc/Auth/AuthPlayer/v2` with `Platform: "Steam"`, `PlayerID: steamAccountID`, `EpicAuthTicket: eosToken.AccessToken`, `EpicAccountID: eosToken.AccountID`.
   - Receives `AuthPlayerResponse` and dials the WebSocket at `PerConURLv2`.
   - Returns connected `*PsyNetRPC`.

### 2.3 PsyNet Wire Protocol & WebSocket RPC Framing
1. **Delimiter Framing**:
   Every message on the WebSocket connection follows an HTTP-like text format (`psynetrpc.go:100-154`):
   ```
   <HeaderKey1>: <HeaderValue1>\r\n
   <HeaderKey2>: <HeaderValue2>\r\n
   \r\n
   <JSON_PAYLOAD>
   ```
2. **Request Packaging**:
   - Header `PsyService`: Procedure name (e.g. `Matches/GetMatchHistory v1`).
   - Header `PsyRequestID`: Unique string from atomic counter `PsyNetMessage_X_<n>` (`requestid.go:12-15`).
   - Header `PsySig`: Base64 HMAC-SHA256 signature calculated over `"-" + jsonBytes` using key `c338bd36fb8c42b1a431d30add939fc7` (`psynet.go:62-67`).
3. **Response Matching & Async Multiplexing**:
   - Client allocates channel `chan *PsyResponse` in `pendingReqs[requestID]` before writing message (`psynetrpc.go:273`).
   - `readMessages()` extracts `PsyResponseID` from response headers and delivers the message to the corresponding channel (`psynetrpc.go:235-242`).
   - `awaitResponse` unmarshals the response `Result` into the caller's target struct (`psynetrpc.go:298-323`).
   - If an error object is present (`"Error": {"Type": "...", "Message": "..."}`), it returns `psyNetError` (`psynet.go:30-37`).
4. **Heartbeat Protocol**:
   - Every 20 seconds (`pingInterval`), client sends `PsyPing: \r\n\r\n`.
   - Server must respond with a message starting with `PsyPong:`.
   - If pong is not received within 10 seconds (`pongTimeout`), client closes the socket (`psynetrpc.go:167-198`).
5. **Connection Teardown**:
   - If WebSocket closes (e.g. server drops connection on duplicate login), all active pending channels are closed, returning `ErrConnectionClosed` to waiting callers (`psynetrpc.go:85-88, 308`).

### 2.4 Matches/GetMatchHistory v1 Specification
- **Request**:
  - `PlayerID`: string in `Platform|AccountID|0` format (`matches.go:64-66, 74-76`).
- **Response**:
  - `Matches`: slice of `MatchEntry` objects (`matches.go:68-70`).
  - Each `MatchEntry` contains:
    - `ReplayUrl`: HTTP URL string (e.g. `http://api.rlpp.psynet.gg/Match.replay?MatchGUID=...&Timestamp=...&Expiration=...&Signature=...`).
    - `Match.MatchGUID`: Unique UUID/GUID string representing the match.
    - `Match.RecordStartTimestamp`: Unix epoch timestamp (int64) when match recording began.
    - `Match.Playlist`: Integer identifier (e.g. 1=1v1 Duel, 2=2v2 Doubles, 3=3v3 Standard, 6=Private match, 13=Dropshot).
    - `Match.MapName`: Map name string (e.g. `Wasteland_P`, `Stadium_P`).
    - `Match.SecondsPlayed` / `OvertimeSecondsPlayed`: Float64 durations.
    - `Match.WinningTeam`: Integer (0 for Blue, 1 for Orange, -1 for no contest/tie).
    - `Match.Team0Score` / `Team1Score`: Integer final scores.
    - `Match.OverTime` (`bOverTime`), `Match.Forfeit` (`bForfeit`), `Match.NoContest` (`bNoContest`): Booleans.
    - `Match.Players`: Array of participating players, their platform IDs, names, timestamps, game stats (Goals, Assists, Saves, Shots, Demolishes, OwnGoals, Score), and `Skills` MMR tier/division metrics.
- **Replay Download Processing**:
  - If `ReplayUrl != ""`, issue standard HTTP GET request to `ReplayUrl`.
  - The response payload is the raw `.replay` binary file.
  - If `ReplayUrl == ""`, no replay binary is available for that match (e.g., forfeit before minimum threshold, cancelled, or replay expired on PsyNet).

### 2.5 Reliable Test Mocking Strategy
Because `github.com/dank/rlapi` relies on `http.Client` and `websocket.Dialer`:
1. **Mocking HTTP Calls (EGS & PsyNet REST Bootstrap)**:
   - Both `rlapi.NewEGS()` and `rlapi.NewPsyNet()` initialize `http.Client` without custom transports, meaning they use `http.DefaultTransport`.
   - By creating a custom `http.RoundTripper` and setting `http.DefaultTransport = customTransport`:
     - Intercept `account-public-service-prod03.ol.epicgames.com`: Return synthetic `TokenResponse` and exchange codes.
     - Intercept `api.epicgames.dev`: Return synthetic `EOSTokenResponse`.
     - Intercept `api.rlpp.psynet.gg/rpc/Auth/AuthPlayer/v2`: Return synthetic `AuthPlayerResponse` where `PerConURLv2` points to a local test WebSocket server (e.g. `ws://127.0.0.1:<port>`).
2. **Mocking PsyNet WebSocket Server (`MockWSServer`)**:
   - `psynetrpc_test.go:19-115` provides the exact reference implementation for a mock server using `net/http/httptest.Server` and `gorilla/websocket.Upgrader`:
     - Upgrade HTTP to WebSocket.
     - Read incoming text frames.
     - On receiving `PsyPing:`, write `PsyPong: \r\n\r\n`.
     - On receiving `PsyRequestID: <req_id>`, extract `<req_id>`, look up configured JSON payload for `Matches/GetMatchHistory v1`, and reply:
       ```
       PsyTime: <unix_ts>\r\nPsySig: mock_sig\r\nPsyResponseID: <req_id>\r\n\r\n{"Result":{"Matches":[...]}}
       ```
3. **Mocking Replay Downloads**:
   - Set `ReplayUrl` in the mock match entries to `http://127.0.0.1:<port>/test.replay`.
   - An `httptest.Server` serves sample `.replay` binary data.
4. **Mocking In Application Design (Interface Abstraction)**:
   - For high-level daemon testing without raw socket emulation, define:
     ```go
     type MatchHistoryClient interface {
         GetMatchHistory(ctx context.Context) ([]rlapi.MatchEntry, error)
     }
     ```
   - Implement a mock that returns dynamic slices of matches on consecutive polling ticks to simulate new matches appearing over time.

---

## 3. Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Auth - EGS | `NewEGS` | Initializes Epic Games Store HTTP client with 30s timeout | None | `*EGS` | None | `egs.go:73` |
| 2 | Auth - EGS | `GetAuthURL` | Returns Epic OAuth login URL for browser authentication | None | URL string | None | `egs.go:80` |
| 3 | Auth - EGS | `AuthenticateWithCode` | Exchanges OAuth authorization code for EGS access & refresh tokens | `authCode string` | `*TokenResponse, error` | HTTP status != 200 or unmarshal error | `egs.go:87` |
| 4 | Auth - EGS | `AuthenticateWithRefreshToken` | Refreshes EGS access token using stored refresh token | `refreshToken string` | `*TokenResponse, error` | Returns error on invalid/expired token | `egs.go:96` |
| 5 | Auth - EGS | `GetExchangeCode` | Converts EGS access token into short-lived exchange code for EOS | `accessToken string` | `string, error` | HTTP status != 200 | `egs.go:151` |
| 6 | Auth - EGS | `ExchangeEOSToken` | Exchanges exchange code for EOS access token scoped to Rocket League | `exchangeCode string` | `*EOSTokenResponse, error` | HTTP error if code expired/invalid | `egs.go:186` |
| 7 | Auth - EGS | `ExchangeEOSTokenFromSteam` | Exchanges Steam session ticket for EOS token and linked Epic Account ID | `steamTicket string` | `*EOSTokenResponse, error` | HTTP error if ticket invalid/unlinked | `egs.go:194` |
| 8 | Auth - EGS | `RefreshEOSToken` | Refreshes an EOS authentication token using EOS refresh token | `refreshToken string` | `*EOSTokenResponse, error` | HTTP error if token invalid | `egs.go:203` |
| 9 | Auth - EGS | `RevokeEOSToken` | Revokes an active EOS access token | `accessToken string` | `error` | Status != 204 NoContent | `egs.go:251` |
| 10 | Auth - EGS | `AuthenticateWithDevice` | Initiates OAuth 2.0 Device Authorization Grant (RFC 8628) | None | `*DeviceAuthResponse, error` | HTTP error if client rejected | `egs.go:280` |
| 11 | Auth - EGS | `WaitForDeviceAuthorization`| Polls EOS until user enters user code in browser | `*DeviceAuthResponse` | `*EOSTokenResponse, error` | Returns timeout error if expired | `egs.go:313` |
| 12 | Auth - PsyNet | `NewPsyNet` | Initializes PsyNet HTTP bootstrapping client with default version & CRC32 build ID | None | `*PsyNet` | None | `psynet.go:69` |
| 13 | Auth - PsyNet | `SetVersion` / `GetVersion` | Overrides or reads the active game version string and feature set | `gameVersion, featureSet string` | Version string or sets buildID | None | `psynet.go:97, 104` |
| 14 | Auth - PsyNet | `AuthPlayer` | Authenticates Epic player via HTTP POST to Auth/AuthPlayer/v2 and establishes WebSocket | `authToken, accountID, accountName string` | `*PsyNetRPC, error` | HTTP failure, socket dial error, or auth ban | `auth.go:33` |
| 15 | Auth - PsyNet | `AuthPlayerSteam` | Authenticates Steam player with EOS token + SteamID64 and establishes WebSocket | `authToken, epicAccountID, steamAccountID, accountName string` | `*PsyNetRPC, error` | HTTP failure, socket dial error | `auth.go:69` |
| 16 | Identity | `NewPlayerID` | Constructs composite player identifier formatted as `Platform\|ID\|0` | `platform Platform, id string` | `PlayerID` | None | `playerid.go:29` |
| 17 | Identity | `ParsePlayerID` | Parses `Platform\|ID\|0` into platform and account ID components | `playerID string` | `Platform, string, error` | Error if not exactly 3 parts | `playerid.go:34` |
| 18 | RPC Protocol | `IsConnected` | Thread-safe check if WebSocket connection is open and active | None | `bool` | None | `psynetrpc.go:65` |
| 19 | RPC Protocol | `Close` | Closes WebSocket, cancels pings, closes all pending request channels | None | `error` | Returns underlying net close error | `psynetrpc.go:71` |
| 20 | RPC Protocol | `Events` | Returns read-only channel receiving raw unsolicited frames or disconnect events | None | `<-chan *Event` | Drops events if buffer full (32 items) | `psynetrpc.go:335` |
| 21 | Matches RPC | `GetMatchHistory` | Calls `Matches/GetMatchHistory v1` to fetch recent match records for authenticated player | `ctx context.Context` | `[]MatchEntry, error` | Network drop, timeout, or PsyNet error | `matches.go:73` |
| 22 | Match Payload | `MatchEntry.ReplayUrl` | Direct HTTP GET URL for downloading the .replay binary file | N/A | URL string | Empty string `""` if replay unavailable | `matches.go:6` |
| 23 | Match Payload | `Match.MatchGUID` | Unique GUID identifying the match (idempotency key) | N/A | GUID string | None | `matches.go:11` |
| 24 | Match Payload | `Match` metadata | Full match context: timestamps, map, playlist, duration, scores, overtime, forfeit, players | N/A | Struct values | None | `matches.go:10-27` |
| 25 | Match Payload | `MatchPlayer` stats | Detailed participant stats: goals, assists, saves, shots, score, team, MMR rating tier/division | N/A | Struct values | None | `matches.go:29-50` |
| 26 | Additional RPC | `GetProfiles` | Calls `Players/GetProfile v1` for player metadata and presence | `ctx, []PlayerID` | `[]PlayerData, error` | Standard RPC error | `players.go:98` |
| 27 | Additional RPC | `GetXP` | Calls `Players/GetXP v1` for player total XP, level, title, and progress | `ctx` | `*PlayerXPInfo, error` | Standard RPC error | `players.go:112` |
| 28 | Additional RPC | `GetBanStatus` | Calls `Players/GetBanStatus v3` for active ban messages | `ctx, []PlayerID` | `[]interface{}, error` | Standard RPC error | `players.go:84` |
| 29 | Additional RPC | `GetActivePlaylists` | Calls `Playlists/GetActivePlaylists v1` for active playlist IDs | `ctx` | `*GetActivePlaylistsResponse, error` | Standard RPC error | `playlists.go:28` |
| 30 | Additional RPC | `GetPlayerProducts` | Calls `Products/GetPlayerProducts v4` for inventory product instances | `ctx, updatedTimestamp int` | `[]Product, error` | Standard RPC error | `products.go:71` |
| 31 | Additional RPC | `GetClubDetails` | Calls `Clubs/GetClubDetails v1` for club roster and details | `ctx, ClubID` | `*ClubDetails, error` | Standard RPC error | `clubs.go:177` |
| 32 | Additional RPC | `GetActiveChallenges` | Calls `Challenges/GetActiveChallenges v1` for weekly/seasonal challenges | `ctx` | `[]Challenge, error` | Standard RPC error | `challenges.go:110` |
| 33 | Additional RPC | `StartMatchmaking` | Calls `Matchmaking/StartMatchmaking v2` to queue for playlists | `ctx, playlists, region, crossplay, partyID, members` | `int, error` | Standard RPC error | `matchmaking.go:45` |
| 34 | Additional RPC | `PlayerCancelMatchmaking` | Calls `Matchmaking/PlayerCancelMatchmaking v1` to leave queue | `ctx` | `error` | Standard RPC error | `matchmaking.go:65` |

---

## 4. Edge Cases

| # | Feature | Input / Scenario | Observed Behavior |
|---|---------|------------------|-------------------|
| 1 | `GetMatchHistory` | Replay URL expired or match cancelled / forfeit under threshold | `MatchEntry.ReplayUrl` is empty string `""`. Daemon must check `if entry.ReplayUrl != ""` before attempting download. |
| 2 | `AuthenticateWithRefreshToken` | Refresh token revoked, expired, or invalid | HTTP 400 Bad Request returned from `account-public-service-prod03.ol.epicgames.com`. `TokenResponse` parsing returns `authentication failed: 400, <errorCode> - <errorMessage>`. |
| 3 | `ExchangeEOSTokenFromSteam` | Steam ticket expired or Steam account not linked to Epic account | HTTP 400/401 returned from `api.epicgames.dev`. Method returns `unexpected status code 400...`. |
| 4 | `AuthPlayer` | Account permanently banned from PsyNet | Server returns `{"Result": {"IsLastChanceAuthBan": true, ...}}` or HTTP error. `AuthPlayerResponse.IsLastChanceAuthBan` is true. |
| 5 | `PsyNetRPC` Keep-Alive | Server does not respond with `PsyPong:` within 10 seconds | `sendPing` times out on `select <-time.After(pongTimeout)`. Logs error `"pong timeout reached"`, calls `Close()`, and terminates connection. |
| 6 | `PsyNetRPC` In-flight Request | Connection drops (e.g. duplicate login kick from official game client) while request is pending | `readMessages` detects EOF / socket closure, invokes `Close()`. Pending channel is closed and `awaitResponse` returns `ErrConnectionClosed`. |
| 7 | `PsyNetRPC` Timeout | Context deadline expires before server replies to `GetMatchHistory` | Context cancel goroutine unregisters `pendingReqs[requestID]` and closes the response channel. `awaitResponse` returns `context.DeadlineExceeded` with zero channel leak. |
| 8 | `parseMessage` | Server sends frame without double CRLF separator `\r\n\r\n` | `parseMessage` returns `fmt.Errorf("message does not contain expected delimiter")`. Message is routed as raw `EventTypeMessage` to `Events()` channel without crashing client. |
| 9 | `parseMessage` | Server returns error payload `{"Error": {"Type": "InvalidParameters", "Message": "..."}}` | `parseMessage` parses error into `*psyNetError`. `awaitResponse` returns the `psyNetError` which formats as `InvalidParameters: ...`. |
| 10 | `PlayerID` Parsing | Invalid string passed without two pipe `\|` characters | `ParsePlayerID` returns `invalid PlayerID format: ...`. Valid format strictly requires `Platform\|ID\|0`. |

---

## 5. Caveats

1. **No Direct BaseURL Overwrite on PsyNet**: `baseURL` in `psynet.go:21` is defined as a package-level constant (`https://api.rlpp.psynet.gg/rpc`). Wire-level integration tests must redirect HTTP calls by overriding `http.DefaultTransport` with a custom `http.RoundTripper` or provide an application-level interface abstraction (`MatchHistoryClient`).
2. **Game Version & FeatureSet Updates**: Rocket League periodically updates `gameVersion` and `featureSet`. `rlapi` exposes `SetVersion(gameVersion, featureSet)` on `PsyNet`, which should be configurable via environment variables in the daemon to avoid hardcoding outdated version strings.
3. **Steam Ticket Lifetime**: Steam session tickets have limited lifetimes and cannot be renewed without Steam client connectivity. The daemon must handle authentication expiry and prompt or signal when a fresh Steam session ticket is required.
4. **Rate Limits & IP Bans**: PsyNet RPC endpoints are not publicly documented by Psyonix/Epic. Excessive polling (e.g. polling every second instead of the requested 5-minute interval) risks account flagging or IP rate limiting.

---

## 6. Conclusion

1. **Suitability**: `github.com/dank/rlapi` directly and completely implements all Rocket League API requirements specified in `ORIGINAL_REQUEST.md`:
   - Dual authentication for Epic Games (via OAuth refresh token / code exchange for EOS tokens) and Steam (via Steam session tickets exchanged for EOS tokens).
   - Match history retrieval via `Matches/GetMatchHistory v1`, returning match GUIDs, start timestamps, metadata, and direct `.replay` binary download URLs.
   - Robust WebSocket connection management with automatic keep-alive pings, atomic request ID sequencing, and clean context cancellation.
2. **Integration Architecture Recommendations**:
   - **Persistence**: Store the returned `RefreshToken` from Epic authentication in persistent state so restarts are completely headless.
   - **Idempotency**: Use `MatchEntry.Match.MatchGUID` as the unique primary key in the local state store to guarantee no match is ever downloaded or uploaded more than once.
   - **Replay Download**: Only trigger downloads when `MatchEntry.ReplayUrl != ""`.
   - **Test Harness**: Build the automated test suite using a two-tier strategy:
     - Tier 1: Unit tests with a `MatchHistoryClient` interface returning simulated match entries across multiple polling ticks.
     - Tier 2: End-to-end wire tests using `http.DefaultTransport` redirection and a `MockWSServer` (identical to `rlapi/psynetrpc_test.go`) to test the actual `rlapi` SDK integration without real credentials.

---

## 7. Verification Method

To independently verify all findings and validate the specification:

1. **Inspect Cloned Repository**:
   Inspect the cloned repository files in `$env:TEMP\rlapi_spec`:
   - `auth.go`: Examine lines 33-101 for `AuthPlayer` and `AuthPlayerSteam`.
   - `egs.go`: Examine lines 87-200 for token exchange functions.
   - `matches.go`: Examine lines 5-84 for `MatchEntry`, `Match`, and `GetMatchHistory`.
   - `psynetrpc.go`: Examine lines 100-154 for message framing.
   - `psynetrpc_test.go`: Examine lines 19-115 for the reference `MockWSServer` implementation.
2. **Run Library Unit Tests**:
   Once Go is available in the test environment:
   ```bash
   cd $env:TEMP\rlapi_spec
   go test -v ./...
   ```
   This validates all WebSocket framing, ping/pong scheduling, error payload parsing, and mock server interactions.
