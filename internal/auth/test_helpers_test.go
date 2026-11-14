package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// mockEGS implements EGSClient for unit testing without network requests.
type mockEGS struct {
	mu sync.Mutex

	getAuthURLFunc                   func() string
	authenticateWithCodeFunc         func(authCode string) (*rlapi.TokenResponse, error)
	authenticateWithRefreshTokenFunc func(refreshToken string) (*rlapi.TokenResponse, error)
	getExchangeCodeFunc              func(accessToken string) (string, error)
	exchangeEOSTokenFunc             func(exchangeCode string) (*rlapi.EOSTokenResponse, error)
	exchangeEOSTokenFromSteamFunc    func(steamTicket string) (*rlapi.EOSTokenResponse, error)
	refreshEOSTokenFunc              func(refreshToken string) (*rlapi.EOSTokenResponse, error)

	codeCalls         []string
	refreshCalls      []string
	exchangeCodeCalls []string
	eosTokenCalls     []string
	steamCalls        []string
	refreshEOSCalls   []string
}

func (m *mockEGS) GetAuthURL() string {
	m.mu.Lock()
	fn := m.getAuthURLFunc
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return "https://www.epicgames.com/id/api/redirect"
}

func (m *mockEGS) AuthenticateWithCode(authCode string) (*rlapi.TokenResponse, error) {
	m.mu.Lock()
	m.codeCalls = append(m.codeCalls, authCode)
	fn := m.authenticateWithCodeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(authCode)
	}
	return nil, errors.New("unexpected AuthenticateWithCode call")
}

func (m *mockEGS) AuthenticateWithRefreshToken(refreshToken string) (*rlapi.TokenResponse, error) {
	m.mu.Lock()
	m.refreshCalls = append(m.refreshCalls, refreshToken)
	fn := m.authenticateWithRefreshTokenFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(refreshToken)
	}
	return nil, errors.New("unexpected AuthenticateWithRefreshToken call")
}

func (m *mockEGS) GetExchangeCode(accessToken string) (string, error) {
	m.mu.Lock()
	m.exchangeCodeCalls = append(m.exchangeCodeCalls, accessToken)
	fn := m.getExchangeCodeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(accessToken)
	}
	return "mock-exchange-code", nil
}

func (m *mockEGS) ExchangeEOSToken(exchangeCode string) (*rlapi.EOSTokenResponse, error) {
	m.mu.Lock()
	m.eosTokenCalls = append(m.eosTokenCalls, exchangeCode)
	fn := m.exchangeEOSTokenFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(exchangeCode)
	}
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-eos-access-token",
		ExpiresIn:   3600,
	}, nil
}

func (m *mockEGS) ExchangeEOSTokenFromSteam(steamTicket string) (*rlapi.EOSTokenResponse, error) {
	m.mu.Lock()
	m.steamCalls = append(m.steamCalls, steamTicket)
	fn := m.exchangeEOSTokenFromSteamFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(steamTicket)
	}
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-steam-eos-token",
		AccountID:   "epic-account-linked",
		ExpiresIn:   3600,
	}, nil
}

func (m *mockEGS) RefreshEOSToken(refreshToken string) (*rlapi.EOSTokenResponse, error) {
	m.mu.Lock()
	m.refreshEOSCalls = append(m.refreshEOSCalls, refreshToken)
	fn := m.refreshEOSTokenFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(refreshToken)
	}
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-refreshed-eos-token",
		ExpiresIn:   3600,
	}, nil
}

// mockStateStore implements storage.StateStore in memory for testing auth persistence.
type mockStateStore struct {
	storage.StateStore
	mu        sync.Mutex
	saveOrder []string
	authState map[string]struct {
		refreshToken string
		accountID    string
		displayName  string
	}
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{
		authState: make(map[string]struct {
			refreshToken string
			accountID    string
			displayName  string
		}),
	}
}

func (s *mockStateStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveOrder = append(s.saveOrder, refreshToken)
	s.authState[provider] = struct {
		refreshToken string
		accountID    string
		displayName  string
	}{refreshToken, accountID, displayName}
	return nil
}

func (s *mockStateStore) GetAuthState(ctx context.Context, provider string) (string, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.authState[provider]
	if !ok {
		return "", "", "", storage.ErrAuthStateNotFound
	}
	return rec.refreshToken, rec.accountID, rec.displayName, nil
}

func (s *mockStateStore) Close() error { return nil }

func (s *mockStateStore) GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error) {
	return nil, storage.ErrMatchNotFound
}
func (s *mockStateStore) ListPendingDownloads(ctx context.Context) ([]*storage.MatchRecord, error) {
	return nil, nil
}
func (s *mockStateStore) ListPendingUploads(ctx context.Context) ([]*storage.MatchRecord, error) {
	return nil, nil
}
func (s *mockStateStore) UpsertDiscoveredMatches(ctx context.Context, matches []*storage.MatchRecord) error {
	return nil
}
func (s *mockStateStore) MarkDownloading(ctx context.Context, matchGUID string) error { return nil }
func (s *mockStateStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	return nil
}
func (s *mockStateStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	return nil
}
func (s *mockStateStore) MarkUploading(ctx context.Context, matchGUID string) error { return nil }
func (s *mockStateStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	return nil
}
func (s *mockStateStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	return nil
}
func (s *mockStateStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	return nil
}
func (s *mockStateStore) RecoverInFlight(ctx context.Context) error { return nil }
func (s *mockStateStore) UpsertPlayer(ctx context.Context, player *storage.PlayerRecord) error {
	return nil
}
func (s *mockStateStore) GetPlayer(ctx context.Context, playerID string) (*storage.PlayerRecord, error) {
	return nil, storage.ErrPlayerNotFound
}
func (s *mockStateStore) ListPlayers(ctx context.Context, limit, offset int) ([]*storage.PlayerRecord, error) {
	return nil, nil
}
func (s *mockStateStore) ListPlayerSummaries(ctx context.Context, limit, offset int) ([]*storage.PlayerSummary, error) {
	return nil, nil
}
func (s *mockStateStore) SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*storage.PlayerSummary, int, error) {
	return nil, 0, nil
}
func (s *mockStateStore) UpdatePlayerRanks(ctx context.Context, playerID string, ranksJSON string) error {
	return nil
}
func (s *mockStateStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []storage.PlayerOutcome, matchTime ...time.Time) error {
	return nil
}
func (s *mockStateStore) GetPlayerMatchup(ctx context.Context, playerID string, playlistID int) (*storage.PlayerMatchup, error) {
	return nil, nil
}
func (s *mockStateStore) GetPlayerMatchups(ctx context.Context, playerID string) ([]*storage.PlayerMatchup, error) {
	return nil, nil
}
func (s *mockStateStore) SyncReplayStats(ctx context.Context, stats *storage.ReplayMatchStats) error {
	return nil
}
func (s *mockStateStore) ListHistoricalMatches(ctx context.Context, filter storage.MatchFilter) ([]*storage.HistoricalMatchRecord, int, error) {
	return nil, 0, nil
}

// mockPrompter captures prompt requests and returns configured authorization codes.
type mockPrompter struct {
	mu          sync.Mutex
	promptCalls []promptCall
	returnCode  string
	returnErr   error
}

type promptCall struct {
	AccountLabel string
	AuthURL      string
}

func (m *mockPrompter) PromptForCode(ctx context.Context, accountLabel, authURL string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.promptCalls = append(m.promptCalls, promptCall{
		AccountLabel: accountLabel,
		AuthURL:      authURL,
	})
	if m.returnErr != nil {
		return "", m.returnErr
	}
	return m.returnCode, nil
}

// mockSteamGenerator records calls and returns simulated steam ticket results.
type mockSteamGenerator struct {
	mu           sync.Mutex
	calls        []SteamTicketOptions
	generateFunc func(ctx context.Context, opts SteamTicketOptions) (*SteamTicketResult, error)
	returnResult *SteamTicketResult
	returnErr    error
}

func (m *mockSteamGenerator) GenerateTicket(ctx context.Context, opts SteamTicketOptions) (*SteamTicketResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, opts)
	fn := m.generateFunc
	res := m.returnResult
	err := m.returnErr
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, opts)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}
