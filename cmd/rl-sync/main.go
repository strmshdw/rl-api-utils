package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// Version is populated at link time or defaults to "dev".
var Version = "dev"

func main() {
	// Root context trapping OS termination signals (SIGINT, SIGTERM)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := NewDefaultRunner(os.Stdout, os.Stderr)
	code := runner.Run(ctx, os.Args[1:])
	os.Exit(code)
}

// DaemonEngine abstracts the lifecycle execution of the daemon for testing.
type DaemonEngine interface {
	Start(ctx context.Context) error
}

// Runner encapsulates CLI flag parsing, layered configuration resolution,
// component lifecycle wiring, and graceful execution.
type Runner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Dependency injection hooks for unit testing
	LoadConfig       func(cli config.CLIFlags) (*config.Config, error)
	NewStore         func(path string) (storage.StateStore, error)
	NewAuth          func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error)
	AuthOptions      []auth.Option
	NewPsyNet        func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error)
	NewDownloader    func(timeout time.Duration) psynet.ReplayDownloader
	NewBallchasing   func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error)
	NewSyncer        func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer
	NewDaemon        func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error)
	NewPollingAuth   func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error)
	NewRankClient    func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error)
	NewPlayerTracker func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error)
}

// NewDefaultRunner constructs a Runner wired with production constructors.
func NewDefaultRunner(stdout, stderr io.Writer) *Runner {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	r := &Runner{
		Stdin:  os.Stdin,
		Stdout: stdout,
		Stderr: stderr,
		LoadConfig: func(cli config.CLIFlags) (*config.Config, error) {
			return config.Load(cli)
		},
		NewStore: func(path string) (storage.StateStore, error) {
			return storage.NewStore(path)
		},
		NewPsyNet: func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error) {
			return psynet.NewClient(cfg)
		},
		NewDownloader: func(timeout time.Duration) psynet.ReplayDownloader {
			return psynet.NewDownloader(psynet.WithTimeout(timeout))
		},
		NewBallchasing: func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
			return ballchasing.NewClient(cfg)
		},
		NewSyncer: func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
			s, err := syncer.NewWithConfig(store, p, d, u, cfg)
			if err != nil {
				panic(err)
			}
			return s
		},
		NewDaemon: func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
			return daemon.New(s, cfg, opts...)
		},
		NewPollingAuth: func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
			return auth.NewPollingProvider(cfg, opts...)
		},
		NewRankClient: func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error) {
			return playertrack.NewRankClient(cfg)
		},
		NewPlayerTracker: func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
			return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
		},
	}

	r.NewAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
		return auth.NewProvider(cfg, store, r.AuthOptions...)
	}

	return r
}

// authSupplier bridges auth.AuthProvider into psynet.CredentialsSupplier.
type authSupplier struct {
	provider auth.AuthProvider
}

func (s *authSupplier) GetCredentials(ctx context.Context) (*psynet.Credentials, error) {
	token := s.provider.TokenInfo()
	if token == nil || token.IsExpired() {
		var err error
		token, err = s.provider.Authenticate(ctx)
		if err != nil {
			return nil, fmt.Errorf("authenticating with %s: %w", s.provider.Name(), err)
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
		creds.SteamAccountID = token.AccountID // in steam auth, AccountID is SteamID64
	}
	if creds.AccountID == "" {
		creds.AccountID = token.AccountID
	}

	return creds, nil
}
