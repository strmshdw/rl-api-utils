package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var jsonMemStoreSeq atomic.Uint64

// SQLiteStore implements StateStore backed by pure Go modernc.org/sqlite.
type SQLiteStore struct {
	db          *sql.DB
	dbPath      string
	retryMu     sync.RWMutex
	maxRetries  int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// Compile-time checks ensuring SQLiteStore satisfies StateStore and MatchPruner.
var (
	_ StateStore  = (*SQLiteStore)(nil)
	_ MatchPruner = (*SQLiteStore)(nil)
)

// SetRetryPolicy configures the maximum retries and backoff limits for pending queries.
func (s *SQLiteStore) SetRetryPolicy(maxRetries int, baseBackoff, maxBackoff time.Duration) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	if maxRetries > 0 {
		s.maxRetries = maxRetries
	}
	s.baseBackoff = baseBackoff
	s.maxBackoff = maxBackoff
}

func (s *SQLiteStore) retryConfig(overrideMax int) (maxRetries int, baseSec, maxSec int64) {
	s.retryMu.RLock()
	mr := s.maxRetries
	base := s.baseBackoff
	maxB := s.maxBackoff
	s.retryMu.RUnlock()

	maxRetries = overrideMax
	if maxRetries <= 0 {
		maxRetries = mr
		if maxRetries <= 0 {
			maxRetries = DefaultMaxRetries
		}
	}
	baseSec = int64(base / time.Second)
	if base > 0 && baseSec == 0 {
		baseSec = 1
	}
	maxSec = int64(maxB / time.Second)
	if maxSec <= 0 {
		maxSec = 86400
	}
	return maxRetries, baseSec, maxSec
}

// NewSQLiteStore creates and initializes a SQLiteStore at the given database path.
// If dbPath is ":memory:", an in-memory database with shared cache is created.
// Parent directories are automatically created if they do not exist.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, errors.New("dbPath cannot be empty")
	}
	connStr := dbPath
	isMemory := dbPath == ":memory:" || strings.HasPrefix(dbPath, "file::memory:")
	if dbPath == ":memory:" {
		connStr = "file::memory:?cache=shared"
	}
	return openSQLiteStore(dbPath, connStr, isMemory, true)
}

// newIsolatedMemorySQLiteStore opens a private in-memory SQLiteStore used by JSONStore.
func newIsolatedMemorySQLiteStore(runStartupRepair bool) (*SQLiteStore, error) {
	seq := jsonMemStoreSeq.Add(1)
	dsn := fmt.Sprintf("file:rljson_%d?mode=memory&cache=shared", seq)
	return openSQLiteStore(":memory:", dsn, true, runStartupRepair)
}

func openSQLiteStore(dbPath, connStr string, isMemory, runStartupRepair bool) (*SQLiteStore, error) {
	if !isMemory {
		dir := filepath.Dir(dbPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create sqlite directory %s: %w", dir, err)
			}
		}
	}

	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Single connection serializes writes without SQLITE_BUSY contention.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pragmas := []string{
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
	}
	if !isMemory {
		pragmas = append([]string{"PRAGMA journal_mode = WAL;"}, pragmas...)
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to execute %q: %w", p, err)
		}
	}

	if err := runSchemaMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &SQLiteStore{
		db:          db,
		dbPath:      dbPath,
		maxRetries:  DefaultMaxRetries,
		baseBackoff: DefaultBaseBackoff,
		maxBackoff:  DefaultMaxBackoff,
	}

	if runStartupRepair {
		if err := store.repairHistoricalMatchesAndOutcomes(ctx); err != nil {
			slog.Default().Warn("sqlite startup historical repair failed", "error", err)
		}
	}

	return store, nil
}

// SaveAuthState upserts authentication tokens and metadata for a provider.
func (s *SQLiteStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	if provider == "" {
		return errors.New("provider cannot be empty")
	}

	now := time.Now().UTC().Unix()
	const query = `
	INSERT INTO auth_state (provider, refresh_token, account_id, display_name, updated_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(provider) DO UPDATE SET
	    refresh_token = excluded.refresh_token,
	    account_id = excluded.account_id,
	    display_name = excluded.display_name,
	    updated_at = excluded.updated_at;
	`
	if _, err := s.db.ExecContext(ctx, query, provider, refreshToken, accountID, displayName, now); err != nil {
		return fmt.Errorf("failed to save auth state for %s: %w", provider, err)
	}
	return nil
}

// GetAuthState retrieves persisted authentication metadata for a given provider.
func (s *SQLiteStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
	if provider == "" {
		return "", "", "", errors.New("provider cannot be empty")
	}

	const query = `
	SELECT refresh_token, account_id, display_name
	FROM auth_state
	WHERE provider = ?;
	`
	err = s.db.QueryRowContext(ctx, query, provider).Scan(&refreshToken, &accountID, &displayName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", ErrAuthStateNotFound
	}
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get auth state for %s: %w", provider, err)
	}
	return refreshToken, accountID, displayName, nil
}

// Close closes the SQLite database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
