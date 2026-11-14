package playertrack_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// ============================================================================
// Test Doubles & Helpers
// ============================================================================

type testSkillFetcher struct {
	mu           sync.Mutex
	skillsMap    map[rlapi.PlayerID][]rlapi.Skill
	delay        time.Duration
	err          error
	calls        [][]rlapi.PlayerID
	callNotifyCh chan struct{}
	doneNotifyCh chan struct{}
	enabled      bool
	closed       bool
}

func newTestSkillFetcher() *testSkillFetcher {
	return &testSkillFetcher{
		skillsMap:    make(map[rlapi.PlayerID][]rlapi.Skill),
		callNotifyCh: make(chan struct{}, 1000),
		doneNotifyCh: make(chan struct{}, 1000),
		enabled:      true,
	}
}

func (m *testSkillFetcher) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, playertrack.ErrRankClientClosed
	}
	m.calls = append(m.calls, playerIDs)
	delay := m.delay
	errResp := m.err
	m.mu.Unlock()

	select {
	case m.callNotifyCh <- struct{}{}:
	default:
	}

	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	defer func() {
		select {
		case m.doneNotifyCh <- struct{}{}:
		default:
		}
	}()

	if errResp != nil {
		return nil, errResp
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var res []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		if skills, ok := m.skillsMap[pid]; ok {
			res = append(res, rlapi.PlayerWithSkills{
				PlayerID: pid,
				Skills:   skills,
			})
		}
	}
	return res, nil
}

func (m *testSkillFetcher) Calls() [][]rlapi.PlayerID {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([][]rlapi.PlayerID, len(m.calls))
	for i, c := range m.calls {
		res[i] = append([]rlapi.PlayerID(nil), c...)
	}
	return res
}

func (m *testSkillFetcher) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled && !m.closed
}

func (m *testSkillFetcher) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *testSkillFetcher) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockAuthProvider struct {
	providerName string
	token        *auth.TokenInfo
	authErr      error
}

func newMockAuthProvider(name string, token *auth.TokenInfo) *mockAuthProvider {
	return &mockAuthProvider{providerName: name, token: token}
}

func (m *mockAuthProvider) Name() string { return m.providerName }
func (m *mockAuthProvider) Authenticate(ctx context.Context) (*auth.TokenInfo, error) {
	return m.token, m.authErr
}
func (m *mockAuthProvider) Refresh(ctx context.Context, refreshToken string) (*auth.TokenInfo, error) {
	return m.token, m.authErr
}
func (m *mockAuthProvider) TokenInfo() *auth.TokenInfo { return m.token }
func (m *mockAuthProvider) Validate() error            { return nil }

type mockFaultStore struct {
	storage.StateStore
	upsertPlayerErr  error
	recordResultsErr error
}

func (m *mockFaultStore) UpsertPlayer(ctx context.Context, player *storage.PlayerRecord) error {
	if m.upsertPlayerErr != nil {
		return m.upsertPlayerErr
	}
	return m.StateStore.UpsertPlayer(ctx, player)
}

func (m *mockFaultStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []storage.PlayerOutcome, matchTime ...time.Time) error {
	if m.recordResultsErr != nil {
		return m.recordResultsErr
	}
	return m.StateStore.RecordMatchResults(ctx, matchGUID, playlistID, outcomes, matchTime...)
}

func newSQLiteTestStore(t *testing.T) storage.StateStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_playertrack.db")
	s, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newJSONTestStore(t *testing.T) storage.StateStore {
	t.Helper()
	jsonPath := filepath.Join(t.TempDir(), "test_playertrack.json")
	s, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to create test json store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func forEachStore(t *testing.T, fn func(t *testing.T, store storage.StateStore)) {
	fixtures := []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{"SQLite", newSQLiteTestStore},
		{"JSONStore", newJSONTestStore},
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			store := f.factory(t)
			fn(t, store)
		})
	}
}

func makePlayer(name, id string, team int) statsapi.StatsPlayer {
	return statsapi.StatsPlayer{
		Name:      name,
		PrimaryId: id,
		TeamNum:   team,
		Score:     100,
		Goals:     1,
		Assists:   0,
		Saves:     1,
		Shots:     2,
		Demos:     0,
	}
}

func makePlayerWithStats(name, id string, team int, score, goals, assists, saves, shots, demos int) statsapi.StatsPlayer {
	return statsapi.StatsPlayer{
		Name:      name,
		PrimaryId: id,
		TeamNum:   team,
		Score:     score,
		Goals:     goals,
		Assists:   assists,
		Saves:     saves,
		Shots:     shots,
		Demos:     demos,
	}
}

// ============================================================================
