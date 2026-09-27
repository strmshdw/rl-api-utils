package playertrack

// MatchStateListener receives real-time notifications when match telemetry is updated or concluded.
// It decouples the playertrack subsystem from downstream session tracking and SSE streaming,
// preventing circular package dependencies.
type MatchStateListener interface {
	// OnActiveMatchUpdated is invoked whenever lobby roster, player stats, or ranks update.
	OnActiveMatchUpdated(match *CurrentMatchResponse)
	// OnMatchConcluded is invoked when a match concludes and outcomes are settled.
	OnMatchConcluded(match *CurrentMatchResponse)
}

// WithMatchStateListener configures an initial MatchStateListener observer.
func WithMatchStateListener(listener MatchStateListener) TrackerOption {
	return func(t *Tracker) {
		t.listener = listener
	}
}

// SetMatchStateListener sets or replaces the active observer thread-safely.
func (t *Tracker) SetMatchStateListener(listener MatchStateListener) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listener = listener
}

// MatchStateListener returns the currently registered observer (if any).
func (t *Tracker) MatchStateListener() MatchStateListener {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.listener
}
