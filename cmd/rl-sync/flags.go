package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
)

type cliOptions struct {
	flags                  config.CLIFlags
	syncBallchasingHistory bool
}

func parseCLIFlags(args []string, stdout, stderr io.Writer) (*cliOptions, int, bool) {
	fs := flag.NewFlagSet("rl-sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		configPath             string
		configShort            string
		once                   bool
		dryRun                 bool
		logLevel               string
		logFormat              string
		pollInterval           time.Duration
		replayDir              string
		dbPath                 string
		provider               string
		triggerThreshold       int
		forceSyncOnTrigger     bool
		statsAPIEnabled        bool
		playerTrackingEnabled  bool
		localPlayerID          string
		localPlayerName        string
		autoFetchRanks         bool
		sessionContinueWindow  time.Duration
		pollingAuthEnabled     bool
		pollingAuthProvider    string
		webHost                string
		webPort                int
		webEnabled             bool
		syncBallchasingHistory bool
		showVersion            bool
		showVersionV           bool
		showHelp               bool
		showHelpH              bool
	)

	fs.StringVar(&configPath, "config", "", "Path to configuration file (YAML or JSON)")
	fs.StringVar(&configShort, "c", "", "Path to configuration file (shorthand)")
	fs.BoolVar(&once, "once", false, "Execute a single synchronization cycle and exit")
	fs.BoolVar(&dryRun, "dry-run", false, "Simulate sync cycle without downloading or uploading replays")
	fs.StringVar(&logLevel, "log-level", "", "Logging level (debug, info, warn, error)")
	fs.StringVar(&logFormat, "log-format", "", "Logging format (text, json)")
	fs.DurationVar(&pollInterval, "poll-interval", 0, "Polling interval (e.g. 5m, 1m, 30s)")
	fs.StringVar(&replayDir, "replay-dir", "", "Directory to store downloaded replays")
	fs.StringVar(&dbPath, "db-path", "", "Path to SQLite database or JSON state store")
	fs.StringVar(&provider, "provider", "", "Authentication provider override ('epic' or 'steam')")
	fs.IntVar(&triggerThreshold, "trigger-threshold", 0, "Threshold of un-downloaded matches to fire notification/trigger (default 15)")
	fs.BoolVar(&forceSyncOnTrigger, "force-sync", false, "Force immediate PsyNet sync when trigger threshold is reached")
	fs.BoolVar(&statsAPIEnabled, "stats-api", true, "Enable Rocket League Stats API event tracking")
	fs.BoolVar(&playerTrackingEnabled, "player-tracking", true, "Enable player tracking and live lobby analysis")
	fs.StringVar(&localPlayerID, "local-player-id", "", "Override local player ID (e.g. 'Epic|<id>|0' or 'Steam|<id>|0')")
	fs.StringVar(&localPlayerName, "local-player-name", "", "Override local player display name")
	fs.BoolVar(&autoFetchRanks, "auto-fetch-ranks", true, "Automatically fetch competitive ranks via secondary account")
	fs.DurationVar(&sessionContinueWindow, "session-continue-window", 0, "Maximum time since last match to allow continuing a previous play session (default 2h, 0s to disable)")
	fs.BoolVar(&pollingAuthEnabled, "polling-auth", false, "Enable secondary account authentication for rank retrieval")
	fs.StringVar(&pollingAuthProvider, "polling-provider", "", "Authentication provider for secondary account ('epic' or 'steam')")
	fs.StringVar(&webHost, "web-host", "", "HTTP host binding for web dashboard (default '127.0.0.1')")
	fs.IntVar(&webPort, "web-port", 0, "HTTP port binding for web dashboard (default 49125)")
	fs.BoolVar(&webEnabled, "web-enabled", true, "Enable embedded web dashboard and API server")
	fs.BoolVar(&syncBallchasingHistory, "sync-ballchasing-history", false, "Synchronize all historical replays and player stats from Ballchasing.com and exit")
	fs.BoolVar(&showVersion, "version", false, "Display application version and exit")
	fs.BoolVar(&showVersionV, "v", false, "Display application version (shorthand)")
	fs.BoolVar(&showHelp, "help", false, "Display usage help and exit")
	fs.BoolVar(&showHelpH, "h", false, "Display usage help (shorthand)")

	fs.Usage = func() {
		fmt.Fprintf(stdout, "Usage: rl-sync [flags]\n\nFlags:\n")
		fs.SetOutput(stdout)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return nil, 0, true
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return nil, 1, true
	}

	if showHelp || showHelpH {
		fs.Usage()
		return nil, 0, true
	}

	if showVersion || showVersionV {
		fmt.Fprintf(stdout, "rl-sync %s\n", Version)
		return nil, 0, true
	}

	cli := config.CLIFlags{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config":
			cli.ConfigPath = configPath
		case "c":
			if cli.ConfigPath == "" {
				cli.ConfigPath = configShort
			}
		case "once":
			cli.Once = &once
		case "dry-run":
			cli.DryRun = &dryRun
		case "log-level":
			cli.LogLevel = &logLevel
		case "log-format":
			cli.LogFormat = &logFormat
		case "poll-interval":
			cli.PollInterval = &pollInterval
		case "replay-dir":
			cli.ReplayDir = &replayDir
		case "db-path":
			cli.DBPath = &dbPath
		case "provider":
			cli.Provider = &provider
		case "trigger-threshold":
			cli.TriggerThreshold = &triggerThreshold
		case "force-sync":
			cli.ForceSyncOnTrigger = &forceSyncOnTrigger
		case "stats-api":
			cli.StatsAPIEnabled = &statsAPIEnabled
		case "player-tracking":
			cli.PlayerTrackingEnabled = &playerTrackingEnabled
		case "local-player-id":
			cli.LocalPlayerID = &localPlayerID
		case "local-player-name":
			cli.LocalPlayerName = &localPlayerName
		case "auto-fetch-ranks":
			cli.AutoFetchRanks = &autoFetchRanks
		case "session-continue-window":
			cli.SessionContinueWindow = &sessionContinueWindow
		case "polling-auth":
			cli.PollingAuthEnabled = &pollingAuthEnabled
		case "polling-provider":
			cli.PollingAuthProvider = &pollingAuthProvider
		case "web-host":
			cli.WebHost = &webHost
		case "web-port":
			cli.WebPort = &webPort
		case "web-enabled":
			cli.WebEnabled = &webEnabled
		}
	})

	return &cliOptions{
		flags:                  cli,
		syncBallchasingHistory: syncBallchasingHistory,
	}, 0, false
}
