package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

// Syncer is the domain orchestrator that coordinates match discovery,
// replay downloading, Ballchasing uploads, stats polling, and state persistence.
type Syncer struct {
	store                  StateStore
	provider               MatchHistoryProvider
	downloader             ReplayDownloader
	uploader               ReplayUploader
	statsFetcher           ReplayStatsFetcher
	replayDir              string
	dryRun                 bool
	keepLocalFiles         bool
	logger                 *slog.Logger
	localPlayerID          string
	localPlayerName        string
	maxRetries             int
	rateInterval           time.Duration
	rateLimiter            *ballchasing.RateLimiter
	fetcherManagesRate     bool
	maxConcurrentDownloads int
	cycleMu                sync.Mutex
}

// New creates a new Syncer instance with functional options.
func New(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, opts ...Option) (*Syncer, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if provider == nil {
		return nil, ErrNilProvider
	}
	if downloader == nil {
		return nil, ErrNilDownloader
	}
	if uploader == nil {
		return nil, ErrNilUploader
	}

	s := &Syncer{
		store:                  store,
		provider:               provider,
		downloader:             downloader,
		uploader:               uploader,
		replayDir:              "./replays",
		dryRun:                 false,
		keepLocalFiles:         true,
		logger:                 slog.Default(),
		maxRetries:             storage.DefaultMaxRetries,
		rateInterval:           ballchasing.DefaultRateInterval,
		maxConcurrentDownloads: 1,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if s.statsFetcher == nil {
		if fetcher, ok := uploader.(ReplayStatsFetcher); ok {
			s.statsFetcher = fetcher
		}
	}

	if p, ok := s.statsFetcher.(rateLimiterProvider); ok {
		if rl := p.RateLimiter(); rl != nil {
			if s.rateLimiter == nil {
				s.rateLimiter = rl
				s.fetcherManagesRate = true
			} else if s.rateLimiter == rl {
				s.fetcherManagesRate = true
			}
		}
	}

	return s, nil
}

// NewWithConfig creates a new Syncer instance with a Config struct.
func NewWithConfig(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, cfg Config) (*Syncer, error) {
	return New(store, provider, downloader, uploader,
		WithReplayDir(cfg.ReplayDir),
		WithDryRun(cfg.DryRun),
		WithKeepLocalFiles(cfg.KeepLocalFiles),
		WithLocalPlayerID(cfg.LocalPlayerID),
		WithLocalPlayerName(cfg.LocalPlayerName),
		WithLogger(cfg.Logger),
	)
}

// NewSyncerEngine creates a new Syncer matching E2E test harness conventions.
func NewSyncerEngine(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, replayDir string, dryRun bool) *Syncer {
	s, err := New(store, provider, downloader, uploader,
		WithReplayDir(replayDir),
		WithDryRun(dryRun),
	)
	if err != nil {
		panic(err)
	}
	return s
}

// RateLimiter returns the configured or auto-detected Ballchasing rate limiter, if any.
func (s *Syncer) RateLimiter() *ballchasing.RateLimiter {
	return s.rateLimiter
}

// RetryMatch serializes a manual retry for a single match GUID under cycleMu,
// resetting retry counters and running download/upload/stats steps as eligible.
func (s *Syncer) RetryMatch(ctx context.Context, matchGUID string) error {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	guid := strings.TrimSpace(matchGUID)
	if guid == "" {
		return fmt.Errorf("syncer: matchGUID cannot be empty")
	}

	if _, err := s.store.GetMatch(ctx, guid); err != nil {
		return err
	}
	if err := s.store.ResetMatchRetry(ctx, guid); err != nil {
		return err
	}
	if s.dryRun {
		return nil
	}

	rec, err := s.store.GetMatch(ctx, guid)
	if err != nil {
		return err
	}

	stats := &SyncStats{}
	if rec.DownloadStatus != storage.DownloadDownloaded && strings.TrimSpace(rec.ReplayURL) != "" {
		opErr, cancelErr := s.downloadOneMatch(ctx, rec, nil, stats)
		if cancelErr != nil {
			return cancelErr
		}
		if opErr != nil {
			return opErr
		}
		if rec, err = s.store.GetMatch(ctx, guid); err != nil {
			return err
		}
	}

	if rec.DownloadStatus == storage.DownloadDownloaded &&
		rec.UploadStatus != storage.UploadUploaded &&
		rec.UploadStatus != storage.UploadDuplicate &&
		strings.TrimSpace(rec.LocalFilePath) != "" {
		opErr, cancelErr := s.uploadOneMatch(ctx, rec, stats)
		if cancelErr != nil {
			return cancelErr
		}
		if opErr != nil {
			return opErr
		}
		if rec, err = s.store.GetMatch(ctx, guid); err != nil {
			return err
		}
	}

	if s.statsFetcher != nil &&
		(rec.UploadStatus == storage.UploadUploaded || rec.UploadStatus == storage.UploadDuplicate) &&
		strings.TrimSpace(rec.BallchasingID) != "" &&
		rec.StatsSyncStatus != storage.StatsSyncSynced {
		return s.syncSinglePendingStats(ctx, rec)
	}

	return nil
}

// ReplayPlayerCandidate represents a player candidate in a match roster for identity resolution.
type ReplayPlayerCandidate struct {
	Name        string
	Platform    string
	OnlineID    string
	CanonicalID string
	Team        int
	IsBot       bool
}

// LocalPlayerIdentity aggregates all known identities of the local user.
type LocalPlayerIdentity struct {
	ConfigLocalPlayerID   string
	ConfigLocalPlayerName string
	AuthAccountID         string
	AuthDisplayName       string
	AuthPlatform          string
	PingSteamID           string
	PingName              string
	UploaderSteamID       string
	UploaderName          string
	ReplayPlayerName      string
	ReplayLocalTeam       *int
}

// ResolveLocalPlayer resolves the local player index and team from a player roster.
// Returns (localPlayerIndex, localTeamNum, found).
// If an individual player cannot be resolved, but team is known from header, returns (-1, &team, true).
// Strict bot immunity: any candidate with IsBot == true is NEVER matched as the local player.
func ResolveLocalPlayer(players []ReplayPlayerCandidate, id LocalPlayerIdentity) (int, *int, bool) {
	if len(players) == 0 {
		return -1, nil, false
	}

	if cleanID := strings.TrimSpace(id.ConfigLocalPlayerID); cleanID != "" {
		if idx, team, ok := findCandidateByID(players, "", cleanID); ok {
			return idx, team, true
		}
	}
	if authID := strings.TrimSpace(id.AuthAccountID); authID != "" {
		if idx, team, ok := findCandidateByID(players, "", authID); ok {
			return idx, team, true
		}
	}
	for _, steamID := range []string{id.PingSteamID, id.UploaderSteamID} {
		if clean := strings.TrimSpace(steamID); clean != "" {
			if idx, team, ok := findCandidateByID(players, "Steam", clean); ok {
				return idx, team, true
			}
		}
	}
	if cfgName := strings.TrimSpace(id.ConfigLocalPlayerName); cfgName != "" {
		if idx, team, ok := findCandidateByName(players, cfgName); ok {
			return idx, team, true
		}
	}
	for _, name := range []string{id.AuthDisplayName, id.PingName, id.UploaderName} {
		if clean := strings.TrimSpace(name); clean != "" {
			if idx, team, ok := findCandidateByName(players, clean); ok {
				return idx, team, true
			}
		}
	}
	if clean := strings.TrimSpace(id.ReplayPlayerName); clean != "" {
		if idx, team, ok := findCandidateByName(players, clean); ok {
			return idx, team, true
		}
	}
	if id.ReplayLocalTeam != nil {
		t := *id.ReplayLocalTeam
		return -1, &t, true
	}

	return -1, nil, false
}

func findCandidateByID(players []ReplayPlayerCandidate, requiredPlatform, targetID string) (int, *int, bool) {
	for i, p := range players {
		if p.IsBot {
			continue
		}
		if requiredPlatform != "" && !strings.EqualFold(p.Platform, requiredPlatform) {
			continue
		}
		if matchPlayerID(p, targetID) {
			team := p.Team
			return i, &team, true
		}
	}
	return -1, nil, false
}

func findCandidateByName(players []ReplayPlayerCandidate, targetName string) (int, *int, bool) {
	for i, p := range players {
		if p.IsBot {
			continue
		}
		if strings.EqualFold(p.Name, targetName) {
			team := p.Team
			return i, &team, true
		}
	}
	return -1, nil, false
}

func matchPlayerID(p ReplayPlayerCandidate, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if strings.EqualFold(p.CanonicalID, target) || strings.EqualFold(p.OnlineID, target) {
		return true
	}
	if strings.HasPrefix(strings.ToLower(p.CanonicalID), strings.ToLower(target)+"|") {
		return true
	}
	if parts := strings.Split(target, "|"); len(parts) >= 2 {
		if strings.EqualFold(p.OnlineID, parts[1]) {
			return true
		}
	}
	return false
}

func (s *Syncer) gatherLocalPlayerIdentity(ctx context.Context, replayPlayerName string, replayLocalTeam *int, uploader *ballchasing.UploaderInfo) LocalPlayerIdentity {
	id := LocalPlayerIdentity{
		ConfigLocalPlayerID:   s.localPlayerID,
		ConfigLocalPlayerName: s.localPlayerName,
		ReplayPlayerName:      replayPlayerName,
		ReplayLocalTeam:       replayLocalTeam,
	}
	if uploader != nil {
		id.UploaderSteamID = strings.TrimSpace(uploader.SteamID)
		id.UploaderName = strings.TrimSpace(uploader.Name)
	}
	populateAuthIdentity(ctx, s.store, &id)
	return id
}

func populateAuthIdentity(ctx context.Context, store StateStore, id *LocalPlayerIdentity) {
	if store == nil || id == nil {
		return
	}
	for _, provider := range []string{"epic", "steam"} {
		_, accID, disp, err := store.GetAuthState(ctx, provider)
		if err != nil {
			continue
		}
		if accID != "" && id.AuthAccountID == "" {
			id.AuthAccountID = accID
			id.AuthPlatform = provider
		}
		if disp != "" && id.AuthDisplayName == "" {
			id.AuthDisplayName = disp
		}
	}
}

func isContextCanceled(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func contextCancelErr(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
