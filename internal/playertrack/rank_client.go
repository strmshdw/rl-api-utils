package playertrack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rlapi"
)

// Sentinel errors for rank retrieval.
var (
	ErrRankClientClosed    = errors.New("rank client is closed")
	ErrRankClientDisabled  = errors.New("rank client is disabled")
	ErrCredentialCollision = errors.New("polling auth credentials conflict with primary auth credentials (risk of Error 67)")
	ErrMissingCredentials  = errors.New("missing polling authentication credentials")
)

// SkillFetcher provides an abstraction for querying player competitive skill metrics.
type SkillFetcher interface {
	// GetPlayersSkills retrieves competitive skill data across all playlists for the given player IDs.
	// An empty playerIDs slice returns (nil, nil) without network overhead.
	GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error)

	// IsEnabled returns true if the client is actively configured, online, and not closed.
	IsEnabled() bool

	// Close terminates the underlying WebSocket RPC connection and cleans up resources.
	Close() error
}

// SkillRPCClient abstracts WebSocket RPC methods required for rank queries.
// Satisfied directly by *rlapi.PsyNetRPC.
type SkillRPCClient interface {
	GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error)
	IsConnected() bool
	Close() error
}

// SkillRPCFactory instantiates a SkillRPCClient from credentials (used for tests and mock injection).
type SkillRPCFactory func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error)

// RankClientConfig configures RankClient construction.
type RankClientConfig struct {
	PrimaryAuth         config.AuthConfig
	PollingAuth         config.PollingAuthConfig
	CredentialsSupplier psynet.CredentialsSupplier
	AuthProvider        auth.AuthProvider
	Logger              *slog.Logger
	PsyNet              *rlapi.PsyNet
	RPCFactory          SkillRPCFactory
}

// AuthProviderSupplier bridges auth.AuthProvider into psynet.CredentialsSupplier.
type AuthProviderSupplier struct {
	provider auth.AuthProvider
}

// NewAuthProviderSupplier creates an AuthProviderSupplier wrapping an AuthProvider.
func NewAuthProviderSupplier(provider auth.AuthProvider) *AuthProviderSupplier {
	return &AuthProviderSupplier{provider: provider}
}

// GetCredentials resolves fresh authentication tokens from the underlying AuthProvider.
func (s *AuthProviderSupplier) GetCredentials(ctx context.Context) (*psynet.Credentials, error) {
	if s.provider == nil {
		return nil, ErrMissingCredentials
	}

	token := s.provider.TokenInfo()
	if token == nil || token.IsExpired() {
		var err error
		token, err = s.provider.Authenticate(ctx)
		if err != nil {
			return nil, fmt.Errorf("authenticating polling account (%s): %w", s.provider.Name(), err)
		}
	}

	platform := "Epic"
	if strings.EqualFold(s.provider.Name(), "steam") {
		platform = "Steam"
	}

	creds := &psynet.Credentials{
		Platform:    platform,
		AuthToken:   token.AccessToken,
		AccountID:   token.EpicAccountID,
		DisplayName: token.DisplayName,
	}
	if platform == "Steam" {
		creds.SteamAccountID = token.AccountID // For Steam, AccountID is SteamID64
	}
	if creds.AccountID == "" {
		creds.AccountID = token.AccountID
	}

	return creds, nil
}

// CheckCredentialCollision inspects configuration to verify that polling_auth credentials
// do not conflict with primary auth credentials. If a collision is detected, connecting
// the secondary account would terminate the player's active game with Error 67.
func CheckCredentialCollision(primary config.AuthConfig, polling config.PollingAuthConfig) error {
	primaryProvider := strings.ToLower(strings.TrimSpace(primary.Provider))
	pollingProvider := strings.ToLower(strings.TrimSpace(polling.Provider))

	if primaryProvider == "" || pollingProvider == "" || primaryProvider != pollingProvider {
		// Differing providers (e.g. primary is Steam, polling is Epic) cannot collide
		return nil
	}

	switch primaryProvider {
	case "epic":
		primAcc := strings.TrimSpace(primary.Epic.AccountID)
		pollAcc := strings.TrimSpace(polling.Epic.AccountID)
		if primAcc != "" && pollAcc != "" && strings.EqualFold(primAcc, pollAcc) {
			return fmt.Errorf("%w: epic account_id %q matches primary account", ErrCredentialCollision, pollAcc)
		}

		primTok := strings.TrimSpace(primary.Epic.RefreshToken)
		pollTok := strings.TrimSpace(polling.Epic.RefreshToken)
		if primTok != "" && pollTok != "" && primTok == pollTok {
			return fmt.Errorf("%w: epic refresh_token matches primary account", ErrCredentialCollision)
		}

		primCode := strings.TrimSpace(primary.Epic.AuthCode)
		pollCode := strings.TrimSpace(polling.Epic.AuthCode)
		if primCode != "" && pollCode != "" && primCode == pollCode {
			return fmt.Errorf("%w: epic auth_code matches primary account", ErrCredentialCollision)
		}

	case "steam":
		primSteam := strings.TrimSpace(primary.Steam.SteamID64)
		pollSteam := strings.TrimSpace(polling.Steam.SteamID64)
		if primSteam != "" && pollSteam != "" && primSteam == pollSteam {
			return fmt.Errorf("%w: steam steam_id_64 %q matches primary account", ErrCredentialCollision, pollSteam)
		}

		primTicket := strings.TrimSpace(primary.Steam.SessionTicket)
		pollTicket := strings.TrimSpace(polling.Steam.SessionTicket)
		if primTicket != "" && pollTicket != "" && primTicket == pollTicket {
			return fmt.Errorf("%w: steam session_ticket matches primary account", ErrCredentialCollision)
		}

		primUser := strings.ToLower(strings.TrimSpace(primary.Steam.Username))
		pollUser := strings.ToLower(strings.TrimSpace(polling.Steam.Username))
		if primUser != "" && pollUser != "" && primUser == pollUser {
			return fmt.Errorf("%w: steam username %q matches primary account", ErrCredentialCollision, pollUser)
		}
	}

	return nil
}

// NoOpRankClient is an offline fallback that satisfies SkillFetcher without performing network operations.
type NoOpRankClient struct{}

// NewNoOpRankClient returns an instance of NoOpRankClient.
func NewNoOpRankClient() *NoOpRankClient {
	return &NoOpRankClient{}
}

// GetPlayersSkills returns nil, nil immediately without performing any network operations.
func (n *NoOpRankClient) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	return nil, nil
}

// IsEnabled always returns false for NoOpRankClient.
func (n *NoOpRankClient) IsEnabled() bool {
	return false
}

// Close is a no-op returning nil.
func (n *NoOpRankClient) Close() error {
	return nil
}

// PsyNetRankClient executes PsyNet Skills/GetPlayersSkills v1 RPC queries
// over an isolated WebSocket connection using a dedicated non-playing secondary account.
type PsyNetRankClient struct {
	mu         sync.Mutex
	cfg        RankClientConfig
	supplier   psynet.CredentialsSupplier
	psyNet     *rlapi.PsyNet
	rpc        SkillRPCClient
	rpcFactory SkillRPCFactory
	logger     *slog.Logger
	closed     bool
}

// NewPsyNetRankClient constructs a direct PsyNetRankClient instance.
func NewPsyNetRankClient(cfg RankClientConfig) (*PsyNetRankClient, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	supplier := cfg.CredentialsSupplier
	if supplier == nil {
		if cfg.AuthProvider != nil {
			supplier = NewAuthProviderSupplier(cfg.AuthProvider)
		} else {
			return nil, ErrMissingCredentials
		}
	}

	psy := cfg.PsyNet
	if psy == nil {
		psy = rlapi.NewPsyNet()
		psy.SetLogger(logger)
	}

	return &PsyNetRankClient{
		cfg:        cfg,
		supplier:   supplier,
		psyNet:     psy,
		rpcFactory: cfg.RPCFactory,
		logger:     logger,
	}, nil
}

// NewRankClient creates a SkillFetcher based on system configuration.
// It implements Error 67 collision detection and graceful degradation:
// - If polling_auth is disabled -> returns NoOpRankClient (info log).
// - If credentials conflict with primary auth -> logs critical warning and returns NoOpRankClient.
// - If credentials are missing or invalid -> logs warning and returns NoOpRankClient.
// - If provider instantiation fails -> logs warning and returns NoOpRankClient.
// It NEVER returns a fatal error that halts daemon startup or blocks player tracking.
func NewRankClient(cfg RankClientConfig) (SkillFetcher, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// 1. Check if disabled
	if !cfg.PollingAuth.Enabled {
		logger.Info("polling_auth is disabled; using NoOpRankClient (live rank queries disabled)")
		return NewNoOpRankClient(), nil
	}

	// 2. Check for Error 67 Credential Collision
	if err := CheckCredentialCollision(cfg.PrimaryAuth, cfg.PollingAuth); err != nil {
		logger.Warn("CRITICAL: polling auth credential collision detected; falling back to NoOpRankClient to prevent kicking game client with Error 67",
			slog.Any("error", err),
			slog.String("primary_provider", cfg.PrimaryAuth.Provider),
			slog.String("polling_provider", cfg.PollingAuth.Provider),
		)
		return NewNoOpRankClient(), nil
	}

	// 3. Resolve CredentialsSupplier
	supplier := cfg.CredentialsSupplier
	if supplier == nil {
		authProvider := cfg.AuthProvider
		if authProvider == nil {
			providerName := strings.ToLower(strings.TrimSpace(cfg.PollingAuth.Provider))
			authCfg := config.AuthConfig{
				Provider: providerName,
				Epic:     cfg.PollingAuth.Epic,
				Steam:    cfg.PollingAuth.Steam,
			}

			// Credential presence validation
			switch providerName {
			case "epic":
				if strings.TrimSpace(authCfg.Epic.RefreshToken) == "" && strings.TrimSpace(authCfg.Epic.AuthCode) == "" {
					logger.Warn("polling_auth enabled for epic but missing credentials; falling back to NoOpRankClient")
					return NewNoOpRankClient(), nil
				}
			case "steam":
				if strings.TrimSpace(authCfg.Steam.SessionTicket) == "" || strings.TrimSpace(authCfg.Steam.SteamID64) == "" {
					logger.Warn("polling_auth enabled for steam but missing credentials; falling back to NoOpRankClient")
					return NewNoOpRankClient(), nil
				}
			default:
				logger.Warn("polling_auth has invalid or unsupported provider; falling back to NoOpRankClient",
					slog.String("provider", cfg.PollingAuth.Provider),
				)
				return NewNoOpRankClient(), nil
			}

			// Note: pass store: nil for complete isolation from primary auth state
			var err error
			authProvider, err = auth.NewProvider(authCfg, nil)
			if err != nil {
				logger.Warn("failed to instantiate polling auth provider; falling back to NoOpRankClient",
					slog.Any("error", err),
				)
				return NewNoOpRankClient(), nil
			}
		}

		supplier = NewAuthProviderSupplier(authProvider)
	}

	client, err := NewPsyNetRankClient(RankClientConfig{
		PrimaryAuth:         cfg.PrimaryAuth,
		PollingAuth:         cfg.PollingAuth,
		CredentialsSupplier: supplier,
		Logger:              logger,
		PsyNet:              cfg.PsyNet,
		RPCFactory:          cfg.RPCFactory,
	})
	if err != nil {
		logger.Warn("failed to initialize PsyNetRankClient; falling back to NoOpRankClient", slog.Any("error", err))
		return NewNoOpRankClient(), nil
	}

	return client, nil
}

// GetPlayersSkills retrieves skill data across all playlists for the requested player IDs.
func (c *PsyNetRankClient) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if len(playerIDs) == 0 {
		return nil, nil
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrRankClientClosed
	}

	// 1. Ensure connected
	if c.rpc == nil || !c.rpc.IsConnected() {
		if err := c.connectLocked(ctx); err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("failed to connect to psynet: %w", err)
		}
	}
	activeRPC := c.rpc
	c.mu.Unlock()

	// 2. Execute RPC query
	results, err := activeRPC.GetPlayersSkills(ctx, playerIDs)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Handle connection drop: transparent reconnect and single retry
		if errors.Is(err, rlapi.ErrConnectionClosed) || isNetworkOrEOF(err) {
			c.logger.Warn("psynet connection dropped during rank query; attempting transparent reconnect", slog.Any("err", err))

			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return nil, ErrRankClientClosed
			}
			if recErr := c.connectLocked(ctx); recErr != nil {
				c.mu.Unlock()
				return nil, fmt.Errorf("rank query failed (%v) and reconnect failed: %w", err, recErr)
			}
			retryRPC := c.rpc
			c.mu.Unlock()

			results, err = retryRPC.GetPlayersSkills(ctx, playerIDs)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, fmt.Errorf("rank query failed after reconnect: %w", err)
			}
		} else {
			return nil, fmt.Errorf("psynet GetPlayersSkills failed: %w", err)
		}
	}

	return results, nil
}

// IsEnabled returns true if the client is active and not closed.
func (c *PsyNetRankClient) IsEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

// Close gracefully closes the underlying WebSocket RPC connection.
func (c *PsyNetRankClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	var err error
	if c.rpc != nil {
		err = c.rpc.Close()
		c.rpc = nil
	}
	return err
}

func (c *PsyNetRankClient) connectLocked(ctx context.Context) error {
	if c.closed {
		return ErrRankClientClosed
	}

	if c.rpc != nil {
		_ = c.rpc.Close()
		c.rpc = nil
	}

	creds, err := c.supplier.GetCredentials(ctx)
	if err != nil {
		return fmt.Errorf("resolving polling credentials: %w", err)
	}

	// Runtime Error 67 collision check
	primaryID := c.cfg.PrimaryAuth.Epic.AccountID
	if strings.EqualFold(c.cfg.PrimaryAuth.Provider, "steam") {
		primaryID = c.cfg.PrimaryAuth.Steam.SteamID64
	}
	if primaryID != "" && creds.AccountID == primaryID {
		return ErrCredentialCollision
	}

	// Use RPCFactory if provided (for tests and mocks)
	if c.rpcFactory != nil {
		rpc, err := c.rpcFactory(ctx, creds)
		if err != nil {
			return err
		}
		c.rpc = rpc
		return nil
	}

	// Establish live rlapi.PsyNetRPC
	var rpc *rlapi.PsyNetRPC
	platform := strings.ToLower(strings.TrimSpace(creds.Platform))
	switch platform {
	case "epic", "":
		rpc, err = c.psyNet.AuthPlayer(creds.AuthToken, creds.AccountID, creds.DisplayName)
		if err != nil {
			return fmt.Errorf("epic auth player failed: %w", err)
		}
	case "steam":
		if creds.SteamAccountID == "" {
			return errors.New("steam authentication requires SteamAccountID")
		}
		rpc, err = c.psyNet.AuthPlayerSteam(creds.AuthToken, creds.AccountID, creds.SteamAccountID, creds.DisplayName)
		if err != nil {
			return fmt.Errorf("steam auth player failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported platform %q", creds.Platform)
	}

	c.rpc = rpc
	c.logger.Info("connected secondary polling account to psynet rpc",
		slog.String("platform", creds.Platform),
		slog.String("account_id", creds.AccountID),
	)
	return nil
}

func isNetworkOrEOF(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "closed") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe")
}

// ============================================================================
// Canonical 23-Tier & 4-Division Ranking System
// ============================================================================

var tierNames = [...]string{
	0:  "Unranked",
	1:  "Bronze I",
	2:  "Bronze II",
	3:  "Bronze III",
	4:  "Silver I",
	5:  "Silver II",
	6:  "Silver III",
	7:  "Gold I",
	8:  "Gold II",
	9:  "Gold III",
	10: "Platinum I",
	11: "Platinum II",
	12: "Platinum III",
	13: "Diamond I",
	14: "Diamond II",
	15: "Diamond III",
	16: "Champion I",
	17: "Champion II",
	18: "Champion III",
	19: "Grand Champion I",
	20: "Grand Champion II",
	21: "Grand Champion III",
	22: "Supersonic Legend",
}

var divisionNames = [...]string{
	0: "Division I",
	1: "Division II",
	2: "Division III",
	3: "Division IV",
}

// TierName returns the base name of a competitive tier, or "Unknown" if out of bounds.
func TierName(tier int) string {
	if tier < 0 || tier >= len(tierNames) {
		return "Unknown"
	}
	return tierNames[tier]
}

// DivisionName returns the formatted division string (e.g. "Division I"), or "" if out of bounds.
func DivisionName(division int) string {
	if division < 0 || division >= len(divisionNames) {
		return ""
	}
	return divisionNames[division]
}

// FormatRank formats a tier and division into a human-readable rank name.
// Special rules:
// - Tier 0 (Unranked) and Tier 22 (Supersonic Legend) never display a division.
// - Tiers 1-21 display "<TierName> Division <Numeral>" if division is 0..3, or "<TierName>" if division is out of bounds.
// - Tiers < 0 or > 22 return "Unknown".
func FormatRank(tier, division int) string {
	if tier < 0 || tier >= len(tierNames) {
		return "Unknown"
	}

	baseName := tierNames[tier]

	// Rule: Unranked (0) and Supersonic Legend (22) do not display divisions.
	if tier == 0 || tier == 22 {
		return baseName
	}

	// Standard tiers (1-21): append division if valid
	if division >= 0 && division < len(divisionNames) {
		return baseName + " " + divisionNames[division]
	}

	// Out-of-bounds division: degrade gracefully to base tier name
	return baseName
}

// ============================================================================
// Canonical Playlist Mapping
// ============================================================================

var canonicalPlaylists = map[int]string{
	1:  "Casual Duel (1v1)",
	2:  "Casual Doubles (2v2)",
	3:  "Casual Standard (3v3)",
	4:  "Casual Chaos (4v4)",
	10: "Ranked Duel (1v1)",
	11: "Ranked Doubles (2v2)",
	12: "Ranked Solo Standard (3v3)",
	13: "Ranked Standard (3v3)",
	27: "Ranked Hoops",
	28: "Ranked Rumble",
	29: "Ranked Dropshot",
	30: "Ranked Snow Day",
	34: "Competitive Tournaments",
}

// FormatPlaylist returns the canonical human-readable name of a Rocket League playlist.
// If the playlist ID is not recognized, it returns "Playlist <id>".
func FormatPlaylist(playlistID int) string {
	if name, exists := canonicalPlaylists[playlistID]; exists {
		return name
	}
	return fmt.Sprintf("Playlist %d", playlistID)
}

// IsRankedPlaylist returns true if the playlist ID belongs to a competitive or tournament playlist.
func IsRankedPlaylist(playlistID int) bool {
	switch playlistID {
	case 10, 11, 12, 13, 27, 28, 29, 30, 34:
		return true
	default:
		return false
	}
}

// IsExtraMode returns true if the playlist ID represents an Extra Mode (Hoops, Rumble, Dropshot, Snow Day).
func IsExtraMode(playlistID int) bool {
	switch playlistID {
	case 27, 28, 29, 30:
		return true
	default:
		return false
	}
}

// ============================================================================
// Serialization: ranks_json
// ============================================================================

// PlayerPlaylistRank stores the normalized rank and skill metrics for a specific playlist.
type PlayerPlaylistRank struct {
	PlaylistID    int     `json:"playlist_id"`
	PlaylistName  string  `json:"playlist_name"`
	Tier          int     `json:"tier"`
	Division      int     `json:"division"`
	RankName      string  `json:"rank_name"`
	MMR           float64 `json:"mmr"`
	MatchesPlayed int     `json:"matches_played"`
}

// PlayerRanksSnapshot maps string-formatted playlist IDs (e.g. "11", "13") to PlayerPlaylistRank.
type PlayerRanksSnapshot map[string]PlayerPlaylistRank

// GetRank returns the PlayerPlaylistRank for a specific playlist ID, or false if not present.
func (s PlayerRanksSnapshot) GetRank(playlistID int) (PlayerPlaylistRank, bool) {
	if s == nil {
		return PlayerPlaylistRank{}, false
	}
	rank, ok := s[strconv.Itoa(playlistID)]
	return rank, ok
}

// FromSkill constructs a PlayerPlaylistRank from an rlapi.Skill telemetry entry.
func FromSkill(s rlapi.Skill) PlayerPlaylistRank {
	return PlayerPlaylistRank{
		PlaylistID:    s.Playlist,
		PlaylistName:  FormatPlaylist(s.Playlist),
		Tier:          s.Tier,
		Division:      s.Division,
		RankName:      FormatRank(s.Tier, s.Division),
		MMR:           s.MMR,
		MatchesPlayed: s.MatchesPlayed,
	}
}

// BuildRanksSnapshot constructs a PlayerRanksSnapshot from a slice of rlapi.Skill.
func BuildRanksSnapshot(skills []rlapi.Skill) PlayerRanksSnapshot {
	snapshot := make(PlayerRanksSnapshot, len(skills))
	for _, s := range skills {
		key := strconv.Itoa(s.Playlist)
		snapshot[key] = FromSkill(s)
	}
	return snapshot
}

// SerializeRanksJSON serializes a slice of rlapi.Skill into a JSON string.
// If skills is nil or empty, it returns "{}" without error.
func SerializeRanksJSON(skills []rlapi.Skill) (string, error) {
	if len(skills) == 0 {
		return "{}", nil
	}
	snapshot := BuildRanksSnapshot(skills)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "{}", fmt.Errorf("failed to serialize ranks to JSON: %w", err)
	}
	return string(data), nil
}

// ParseRanksJSON parses a stored ranks_json string into a PlayerRanksSnapshot.
// Tolerates empty string or "{}" by returning an empty, non-nil map.
// Backfills missing playlist_id, playlist_name, and rank_name if parsing legacy or partial data.
func ParseRanksJSON(raw string) (PlayerRanksSnapshot, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return make(PlayerRanksSnapshot), nil
	}

	var snapshot PlayerRanksSnapshot
	if err := json.Unmarshal([]byte(trimmed), &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ranks_json: %w", err)
	}

	// Normalization & backfill pass for legacy or partial data
	for key, rank := range snapshot {
		changed := false
		if rank.PlaylistID == 0 {
			if pid, err := strconv.Atoi(key); err == nil {
				rank.PlaylistID = pid
				changed = true
			}
		}
		if rank.PlaylistName == "" && rank.PlaylistID != 0 {
			rank.PlaylistName = FormatPlaylist(rank.PlaylistID)
			changed = true
		}
		if rank.RankName == "" {
			rank.RankName = FormatRank(rank.Tier, rank.Division)
			changed = true
		}
		if changed {
			snapshot[key] = rank
		}
	}

	return snapshot, nil
}

// ============================================================================
// MockSkillFetcher for Testing & Downstream Packages
// ============================================================================

// MockSkillFetcher provides an in-memory double of SkillFetcher for unit tests and downstream packages.
type MockSkillFetcher struct {
	mu        sync.Mutex
	SkillsMap map[rlapi.PlayerID][]rlapi.Skill
	Enabled   bool
	Err       error
	Calls     [][]rlapi.PlayerID
	Closed    bool
}

// NewMockSkillFetcher creates a new initialized MockSkillFetcher.
func NewMockSkillFetcher() *MockSkillFetcher {
	return &MockSkillFetcher{
		SkillsMap: make(map[rlapi.PlayerID][]rlapi.Skill),
		Enabled:   true,
	}
}

// GetPlayersSkills simulates retrieving player skills from an in-memory map.
func (m *MockSkillFetcher) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Closed {
		return nil, ErrRankClientClosed
	}

	m.Calls = append(m.Calls, playerIDs)
	if m.Err != nil {
		return nil, m.Err
	}

	var res []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		if skills, exists := m.SkillsMap[pid]; exists {
			res = append(res, rlapi.PlayerWithSkills{
				PlayerID: pid,
				Skills:   skills,
			})
		}
	}
	return res, nil
}

// IsEnabled returns true if the mock fetcher is enabled and not closed.
func (m *MockSkillFetcher) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Enabled && !m.Closed
}

// Close marks the mock fetcher as closed.
func (m *MockSkillFetcher) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Closed = true
	return nil
}
