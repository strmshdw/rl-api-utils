package statsapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// MatchChecker defines the read-only storage interface required by the Tracker to inspect existing matches.
type MatchChecker interface {
	GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error)
}

// SyncTriggerFunc represents a callback that triggers an end-to-end replay sync pass.
type SyncTriggerFunc func(ctx context.Context, reason string) error

// TrackerConfig controls match evaluation, thresholds, and trigger actions.
type TrackerConfig struct {
	TriggerThreshold   int           // Default: 15
	ForceSyncOnTrigger bool          // If true, automatically queries match history & syncs replays
	EnableToast        bool          // If true, dispatches desktop toast notifications
	ToastCooldown      time.Duration // Minimum duration between repeated toast notifications (default: 2m)
}

// Tracker compares detected match GUIDs against the persistent state store,
// maintains a queue of un-downloaded matches at risk of being lost, and fires
// triggers/notifications when the configured threshold is reached.
type Tracker struct {
	mu        sync.RWMutex
	store     MatchChecker
	notifier  ToastNotifier
	triggerFn SyncTriggerFunc
	cfg       TrackerConfig
	logger    *slog.Logger

	pendingMatches  map[string]time.Time
	lastToastTime   time.Time
	isGameConnected bool
}

// Option configures Tracker behavior.
type Option func(*Tracker)

// WithToastNotifier supplies a custom ToastNotifier implementation.
func WithToastNotifier(n ToastNotifier) Option {
	return func(t *Tracker) {
		if n != nil {
			t.notifier = n
		}
	}
}

// WithSyncTrigger supplies a callback invoked when the trigger threshold fires.
func WithSyncTrigger(fn SyncTriggerFunc) Option {
	return func(t *Tracker) {
		t.triggerFn = fn
	}
}

// WithLogger supplies a custom slog.Logger.
func WithLogger(l *slog.Logger) Option {
	return func(t *Tracker) {
		if l != nil {
			t.logger = l
		}
	}
}

// NewTracker constructs a new Tracker instance.
func NewTracker(store MatchChecker, cfg TrackerConfig, opts ...Option) (*Tracker, error) {
	if store == nil {
		return nil, errors.New("tracker: store cannot be nil")
	}
	if cfg.TriggerThreshold <= 0 {
		cfg.TriggerThreshold = 15
	}
	if cfg.ToastCooldown <= 0 {
		cfg.ToastCooldown = 2 * time.Minute
	}

	t := &Tracker{
		store:          store,
		cfg:            cfg,
		logger:         slog.Default(),
		pendingMatches: make(map[string]time.Time),
	}

	for _, opt := range opts {
		opt(t)
	}

	if t.notifier == nil {
		t.notifier = NewWindowsToastNotifier(t.logger)
	}

	return t, nil
}

// SetGameConnected updates the in-game connection status.
func (t *Tracker) SetGameConnected(connected bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.isGameConnected = connected
}

// IsGameConnected reports whether the Rocket League Stats API is currently connected.
func (t *Tracker) IsGameConnected() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.isGameConnected
}

// RecordMatch evaluates an observed MatchGuid against the state store.
// If the match is not already downloaded or uploaded, it is added to the pending queue.
// When the pending queue reaches or exceeds TriggerThreshold, notifications and/or auto-sync triggers fire.
func (t *Tracker) RecordMatch(ctx context.Context, matchGUID string) (isNewPending bool, pendingCount int, err error) {
	if matchGUID == "" {
		return false, 0, nil
	}

	// 1. Check if the match is already downloaded/uploaded in the persistent store
	existing, err := t.store.GetMatch(ctx, matchGUID)
	if err == nil && existing != nil {
		// If already successfully downloaded or uploaded, it is not at risk of being lost
		if existing.DownloadStatus == storage.DownloadDownloaded ||
			existing.UploadStatus == storage.UploadUploaded ||
			existing.UploadStatus == storage.UploadDuplicate {
			t.mu.Lock()
			delete(t.pendingMatches, matchGUID)
			count := len(t.pendingMatches)
			t.mu.Unlock()
			return false, count, nil
		}
	} else if err != nil && !errors.Is(err, storage.ErrMatchNotFound) {
		t.logger.Warn("error querying state store for match",
			slog.String("match_guid", matchGUID),
			slog.Any("error", err),
		)
	}

	// 2. Add to pending queue
	t.mu.Lock()
	_, alreadyPending := t.pendingMatches[matchGUID]
	if !alreadyPending {
		t.pendingMatches[matchGUID] = time.Now()
		isNewPending = true
	}
	pendingCount = len(t.pendingMatches)
	threshold := t.cfg.TriggerThreshold
	forceSync := t.cfg.ForceSyncOnTrigger
	enableToast := t.cfg.EnableToast
	shouldToast := enableToast && (time.Since(t.lastToastTime) >= t.cfg.ToastCooldown)
	if shouldToast && pendingCount >= threshold {
		t.lastToastTime = time.Now()
	}
	t.mu.Unlock()

	t.logger.Info("match recorded via Stats API",
		slog.String("match_guid", matchGUID),
		slog.Bool("is_new_pending", isNewPending),
		slog.Int("pending_count", pendingCount),
		slog.Int("threshold", threshold),
	)

	// 3. Evaluate threshold trigger
	if pendingCount >= threshold {
		t.logger.Warn("pending match threshold reached; replays at risk of FIFO overflow",
			slog.Int("pending_count", pendingCount),
			slog.Int("threshold", threshold),
			slog.Bool("force_sync", forceSync),
		)

		if shouldToast {
			title := "Rocket League Replay Warning"
			msg := fmt.Sprintf("%d matches in queue! Replays at risk of being lost. Run sync soon to backup.", pendingCount)
			if toastErr := t.notifier.ShowToast(ctx, title, msg); toastErr != nil {
				t.logger.Warn("failed to send toast notification", slog.Any("error", toastErr))
			}
		}

		if forceSync && t.triggerFn != nil {
			t.logger.Info("force_sync_on_trigger enabled: triggering immediate replay sync")
			go func() {
				syncCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				if syncErr := t.triggerFn(syncCtx, fmt.Sprintf("threshold_%d_reached", pendingCount)); syncErr != nil {
					t.logger.Error("forced sync trigger failed", slog.Any("error", syncErr))
				}
			}()
		}
	}

	return isNewPending, pendingCount, nil
}

// ClearMatches removes the specified match GUIDs from the pending queue (e.g., following a successful sync).
func (t *Tracker) ClearMatches(matchGUIDs []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, guid := range matchGUIDs {
		delete(t.pendingMatches, guid)
	}
}

// RefreshPending reconciles the pending queue against the store, removing matches that are now downloaded/uploaded.
func (t *Tracker) RefreshPending(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for guid := range t.pendingMatches {
		match, err := t.store.GetMatch(ctx, guid)
		if err == nil && match != nil {
			if match.DownloadStatus == storage.DownloadDownloaded ||
				match.UploadStatus == storage.UploadUploaded ||
				match.UploadStatus == storage.UploadDuplicate {
				delete(t.pendingMatches, guid)
			}
		}
	}
}

// PendingCount returns the number of matches currently pending in queue.
func (t *Tracker) PendingCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.pendingMatches)
}

// PendingGUIDs returns a snapshot slice of all match GUIDs currently pending.
func (t *Tracker) PendingGUIDs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	guids := make([]string, 0, len(t.pendingMatches))
	for guid := range t.pendingMatches {
		guids = append(guids, guid)
	}
	return guids
}

// Status returns a snapshot of the current tracker status.
func (t *Tracker) Status() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()

	matches := make([]PendingMatch, 0, len(t.pendingMatches))
	for guid, detectedAt := range t.pendingMatches {
		matches = append(matches, PendingMatch{
			MatchGUID:  guid,
			DetectedAt: detectedAt,
		})
	}

	return Status{
		Enabled:            true,
		GameConnected:      t.isGameConnected,
		PendingCount:       len(t.pendingMatches),
		TriggerThreshold:   t.cfg.TriggerThreshold,
		ForceSyncOnTrigger: t.cfg.ForceSyncOnTrigger,
		PendingMatches:     matches,
	}
}
