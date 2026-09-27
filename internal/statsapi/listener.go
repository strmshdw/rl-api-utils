package statsapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ListenerConfig defines the connection parameters for the Rocket League Stats API.
type ListenerConfig struct {
	Address        string        // e.g. "127.0.0.1:49124" or "localhost:49123"
	Protocol       string        // "websocket" or "tcp"
	ReconnectDelay time.Duration // Reconnection backoff interval (default: 3s)
}

// PlayerEventHandler receives decoded real-time match and player events from the Stats API Listener.
type PlayerEventHandler interface {
	OnUpdateState(ctx context.Context, matchGUID string, playlistID int, players []StatsPlayer) error
	OnMatchEnded(ctx context.Context, matchGUID string, winnerTeamNum *int) error
}

// ListenerOption configures Listener behavior.
type ListenerOption func(*Listener)

// WithPlayerEventHandler sets the PlayerEventHandler callback for the Listener.
func WithPlayerEventHandler(h PlayerEventHandler) ListenerOption {
	return func(l *Listener) {
		l.playerEvents = h
	}
}

// Listener connects to Rocket League's local Stats API (MatchStatsExporter_TA),
// parses real-time match events, delivers observed match GUIDs to the Tracker,
// and optionally forwards match state updates and outcomes to a PlayerEventHandler.
type Listener struct {
	cfg          ListenerConfig
	tracker      *Tracker
	playerEvents PlayerEventHandler
	logger       *slog.Logger

	mu        sync.RWMutex
	running   bool
	cancelFn  context.CancelFunc
	connected bool
}

// NewListener constructs a new Listener instance.
func NewListener(cfg ListenerConfig, tracker *Tracker, logger *slog.Logger, opts ...ListenerOption) (*Listener, error) {
	if tracker == nil {
		return nil, errors.New("listener: tracker cannot be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.Address) == "" {
		cfg.Address = "127.0.0.1:49124"
	}
	if strings.ToLower(strings.TrimSpace(cfg.Protocol)) == "" {
		cfg.Protocol = "websocket"
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = 3 * time.Second
	}

	l := &Listener{
		cfg:     cfg,
		tracker: tracker,
		logger:  logger,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}

	return l, nil
}

// SetPlayerEventHandler safely sets or updates the PlayerEventHandler at runtime.
func (l *Listener) SetPlayerEventHandler(h PlayerEventHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.playerEvents = h
}

// PlayerEventHandler returns the currently registered PlayerEventHandler, or nil if none is set.
func (l *Listener) PlayerEventHandler() PlayerEventHandler {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.playerEvents
}

// Start initiates the background listener loop. It runs until the provided context is canceled.
func (l *Listener) Start(ctx context.Context) error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return errors.New("listener is already running")
	}
	l.running = true
	runCtx, cancel := context.WithCancel(ctx)
	l.cancelFn = cancel
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.setConnected(false)
		l.mu.Unlock()
	}()

	l.logger.Info("starting Rocket League Stats API listener",
		slog.String("address", l.cfg.Address),
		slog.String("protocol", l.cfg.Protocol),
	)

	for {
		select {
		case <-runCtx.Done():
			l.logger.Info("Stats API listener stopping due to context cancellation")
			return nil
		default:
		}

		var err error
		if strings.EqualFold(l.cfg.Protocol, "tcp") {
			err = l.runTCP(runCtx)
		} else {
			err = l.runWebSocket(runCtx)
		}

		l.setConnected(false)

		if err != nil && !errors.Is(err, context.Canceled) {
			l.logger.Debug("Stats API connection closed; retrying in backoff window",
				slog.Any("reason", err),
				slog.Duration("backoff", l.cfg.ReconnectDelay),
			)
		}

		select {
		case <-runCtx.Done():
			return nil
		case <-time.After(l.cfg.ReconnectDelay):
		}
	}
}

// Stop shuts down the listener.
func (l *Listener) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancelFn != nil {
		l.cancelFn()
	}
}

// IsConnected reports whether the listener currently holds an active connection to Rocket League.
func (l *Listener) IsConnected() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.connected
}

func (l *Listener) setConnected(c bool) {
	l.mu.Lock()
	l.connected = c
	l.mu.Unlock()
	l.tracker.SetGameConnected(c)
}

func (l *Listener) runWebSocket(ctx context.Context) error {
	addr := l.cfg.Address
	wsURL := addr
	if !strings.HasPrefix(wsURL, "ws://") && !strings.HasPrefix(wsURL, "wss://") {
		wsURL = "ws://" + wsURL
	}

	u, err := url.Parse(wsURL)
	if err != nil {
		return fmt.Errorf("invalid websocket URL %q: %w", wsURL, err)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 3 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	defer conn.Close()

	l.setConnected(true)
	l.logger.Info("connected to Rocket League Stats API via WebSocket", slog.String("url", u.String()))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		l.handleRawMessage(ctx, message)
	}
}

func (l *Listener) runTCP(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", l.cfg.Address)
	if err != nil {
		return err
	}
	defer conn.Close()

	l.setConnected(true)
	l.logger.Info("connected to Rocket League Stats API via TCP", slog.String("address", l.cfg.Address))

	reader := bufio.NewReader(conn)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			l.handleRawMessage(ctx, line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("TCP server closed connection")
			}
			return err
		}
	}
}

func (l *Listener) handleRawMessage(ctx context.Context, data []byte) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return
	}

	var msg EventMessage
	if err := json.Unmarshal([]byte(trimmed), &msg); err != nil {
		l.logger.Debug("ignoring non-JSON or malformed Stats API message",
			slog.String("raw", trimmed),
			slog.Any("error", err),
		)
		return
	}

	guid := strings.TrimSpace(msg.Data.MatchGuid)

	switch msg.Event {
	case "MatchCreated":
		if guid != "" {
			l.logger.Info("match event detected via Stats API",
				slog.String("event", msg.Event),
				slog.String("match_guid", guid),
			)
			if _, _, err := l.tracker.RecordMatch(ctx, guid); err != nil {
				l.logger.Warn("error recording match in tracker",
					slog.String("match_guid", guid),
					slog.Any("error", err),
				)
			}
		}

	case "UpdateState":
		handler := l.PlayerEventHandler()
		if handler != nil {
			playlistID := msg.Data.GetPlaylist()
			if err := handler.OnUpdateState(ctx, guid, playlistID, msg.Data.Players); err != nil {
				l.logger.Warn("error handling UpdateState in player tracker",
					slog.String("match_guid", guid),
					slog.Int("playlist_id", playlistID),
					slog.Any("error", err),
				)
			}
		}

	case "MatchEnded":
		if guid != "" {
			l.logger.Info("match event detected via Stats API",
				slog.String("event", msg.Event),
				slog.String("match_guid", guid),
			)
			if _, _, err := l.tracker.RecordMatch(ctx, guid); err != nil {
				l.logger.Warn("error recording match in tracker",
					slog.String("match_guid", guid),
					slog.Any("error", err),
				)
			}
		}

		handler := l.PlayerEventHandler()
		if handler != nil {
			if err := handler.OnMatchEnded(ctx, guid, msg.Data.WinnerTeamNum); err != nil {
				l.logger.Warn("error handling MatchEnded in player tracker",
					slog.String("match_guid", guid),
					slog.Any("winner_team", msg.Data.WinnerTeamNum),
					slog.Any("error", err),
				)
			}
		}
	}
}
