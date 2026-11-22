package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/storage"
)

// ReplayOrganizationStore is an optional StateStore capability for persisting daily replay
// autonumbers, session autonumbers, canonical replay filenames, and Ballchasing group mappings.
// Satisfied by *storage.SQLiteStore and *storage.JSONStore.
type ReplayOrganizationStore interface {
	GetDailyCounter(ctx context.Context, counterType, dateKey string) (int, error)
	AllocateMatchReplayNaming(ctx context.Context, matchGUID, dateKey, gameMode, result string, minNextNum int) (*storage.ReplayNamingAssignment, error)
	UpdateMatchReplayFile(ctx context.Context, matchGUID, localPath, replayName, gameMode string) error
	UpdateMatchGrouping(ctx context.Context, matchGUID, sessionID, gameMode, ballchasingGroupID string) error
	GetOrAllocateSessionSequence(ctx context.Context, sessionID, dateKey string) (*storage.SessionSequenceRecord, error)
	GetBallchasingGroup(ctx context.Context, sessionID, gameMode string) (*storage.BallchasingGroupRecord, error)
	SaveBallchasingGroup(ctx context.Context, rec *storage.BallchasingGroupRecord) error
	FindSessionForMatch(ctx context.Context, matchGUID string, matchTime time.Time) (*storage.ResolvedMatchSession, error)
}

// ReplayUploaderWithOptions is an optional uploader capability supporting per-upload
// multipart filenames and dynamic Ballchasing group assignment.
type ReplayUploaderWithOptions interface {
	UploadReplayWithOptions(ctx context.Context, matchGUID, filePath string, opts ballchasing.UploadOptions) (*UploadResult, error)
}

// BallchasingGroupClient is an optional uploader capability supporting Ballchasing replay
// group creation and post-upload replay metadata patching (title and group assignment).
type BallchasingGroupClient interface {
	CreateGroup(ctx context.Context, req ballchasing.CreateGroupRequest) (*ballchasing.CreateGroupResponse, error)
	PatchReplay(ctx context.Context, replayID string, req ballchasing.PatchReplayRequest) error
}

type defaultGroupProvider interface {
	DefaultGroup() string
}

type localPlayerIDsReader interface {
	GetLocalPlayerIDs(ctx context.Context) ([]string, error)
}

type matchPlayersReader interface {
	GetMatchPlayers(ctx context.Context, matchGUID string) ([]*storage.MatchPlayerRecord, error)
}

type parsedLocalReplayInfo struct {
	TeamSize  int
	Result    string // "WIN", "LOSS", or ""
	MatchTime time.Time
	HasDate   bool
}

type resolvedUploadGroup struct {
	SessionID string
	GameMode  string
	GroupID   string
	GroupName string
}

func (s *Syncer) location() *time.Location {
	if s.timeLoc != nil {
		return s.timeLoc
	}
	return time.Local
}

func (s *Syncer) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func (s *Syncer) resolveMatchTime(rec *storage.MatchRecord, parsed *parsedLocalReplayInfo) time.Time {
	loc := s.location()
	if rec != nil && rec.RecordStartTimestamp > 0 {
		return time.Unix(rec.RecordStartTimestamp, 0).In(loc)
	}
	if parsed != nil && parsed.HasDate && !parsed.MatchTime.IsZero() {
		return parsed.MatchTime.In(loc)
	}
	return s.now().In(loc)
}

// organizeDownloadedReplay allocates (or reuses) a daily game autonumber, formats the canonical
// replay filename YYYY_MM_DD_XXX_GameMode_Result.replay, atomically renames the file on disk
// (when present), and persists the naming and session metadata in the store.
func (s *Syncer) organizeDownloadedReplay(
	ctx context.Context,
	rec *storage.MatchRecord,
	localPath string,
	parsed *parsedLocalReplayInfo,
) (string, error) {
	orgStore, ok := s.store.(ReplayOrganizationStore)
	if !ok || rec == nil || strings.TrimSpace(localPath) == "" {
		return localPath, nil
	}

	s.orgMu.Lock()
	defer s.orgMu.Unlock()

	if err := ctx.Err(); err != nil {
		return localPath, err
	}

	matchTime := s.resolveMatchTime(rec, parsed)
	dateKey := matchTime.Format("2006_01_02")

	resolvedSess, sessErr := orgStore.FindSessionForMatch(ctx, rec.MatchGUID, matchTime)
	if sessErr != nil && !isContextCanceled(ctx, sessErr) {
		s.logger.Debug("session lookup during replay rename failed", slog.String("guid", rec.MatchGUID), slog.Any("error", sessErr))
	}

	playlistID := rec.Playlist
	if playlistID <= 0 && resolvedSess != nil && resolvedSess.PlaylistID > 0 {
		playlistID = resolvedSess.PlaylistID
	}
	teamSize := 0
	if parsed != nil {
		teamSize = parsed.TeamSize
	}
	gameMode := domain.GameModeCategory(playlistID, teamSize)

	result := s.resolveReplayOutcome(ctx, rec, parsed, resolvedSess)

	dir := filepath.Dir(localPath)
	minNextNum := highestDailyReplaySeqOnDisk(dir, dateKey) + 1

	assign, err := orgStore.AllocateMatchReplayNaming(ctx, rec.MatchGUID, dateKey, gameMode, result, minNextNum)
	if err != nil {
		return localPath, fmt.Errorf("allocating replay naming: %w", err)
	}

	finalPath := localPath
	targetPath := filepath.Join(dir, assign.ReplayName)
	if _, statErr := os.Stat(localPath); statErr == nil {
		if !sameCleanPath(localPath, targetPath) {
			if err := atomicRenameWithRetry(ctx, localPath, targetPath, 5); err != nil {
				// If a concurrent worker already renamed the file into targetPath, adopt targetPath cleanly.
				if _, targetStatErr := os.Stat(targetPath); targetStatErr != nil {
					return localPath, fmt.Errorf("renaming downloaded replay to %s: %w", assign.ReplayName, err)
				}
			}
		}
		finalPath = targetPath
		if err := orgStore.UpdateMatchReplayFile(ctx, rec.MatchGUID, finalPath, assign.ReplayName, assign.GameMode); err != nil {
			s.logger.Warn("failed to persist renamed replay path", slog.String("guid", rec.MatchGUID), slog.Any("error", err))
		}
	} else if _, targetStatErr := os.Stat(targetPath); targetStatErr == nil {
		// Another concurrent syncer already renamed this match to targetPath.
		finalPath = targetPath
		if err := orgStore.UpdateMatchReplayFile(ctx, rec.MatchGUID, finalPath, assign.ReplayName, assign.GameMode); err != nil {
			s.logger.Warn("failed to persist existing renamed replay path", slog.String("guid", rec.MatchGUID), slog.Any("error", err))
		}
	}

	if resolvedSess != nil && resolvedSess.SessionID != "" {
		_ = orgStore.UpdateMatchGrouping(ctx, rec.MatchGUID, resolvedSess.SessionID, assign.GameMode, "")
	}

	s.logger.Info("assigned canonical replay name",
		slog.String("guid", rec.MatchGUID),
		slog.String("replay_name", assign.ReplayName),
		slog.String("game_mode", assign.GameMode),
		slog.Int("daily_game_num", assign.DailyGameNum),
		slog.Bool("reused", assign.Reused),
	)
	return finalPath, nil
}

func (s *Syncer) resolveReplayOutcome(
	ctx context.Context,
	rec *storage.MatchRecord,
	parsed *parsedLocalReplayInfo,
	resolvedSess *storage.ResolvedMatchSession,
) string {
	if parsed != nil && (parsed.Result == domain.ReplayResultWin || parsed.Result == domain.ReplayResultLoss) {
		return parsed.Result
	}
	if resolvedSess != nil && (resolvedSess.MatchResult == domain.ReplayResultWin || resolvedSess.MatchResult == domain.ReplayResultLoss) {
		return resolvedSess.MatchResult
	}
	if rec == nil {
		return domain.ReplayResultUnknown
	}
	if mpReader, ok := s.store.(matchPlayersReader); ok {
		if players, err := mpReader.GetMatchPlayers(ctx, rec.MatchGUID); err == nil {
			for _, p := range players {
				if p == nil || !p.IsLocal {
					continue
				}
				if p.Won != nil {
					if *p.Won {
						return domain.ReplayResultWin
					}
					return domain.ReplayResultLoss
				}
				if rec.WinnerTeam != nil {
					if p.TeamNum == *rec.WinnerTeam {
						return domain.ReplayResultWin
					}
					return domain.ReplayResultLoss
				}
			}
		}
	}
	return domain.ReplayResultUnknown
}

// resolveOrCreateUploadGroup resolves the match's play session and game mode, checks SQLite
// for an existing Ballchasing group, and creates + persists a new YYYY_MM_DD_SessionNum_GameMode
// group via the Ballchasing API if one has not yet been created for that (session, game_mode).
func (s *Syncer) resolveOrCreateUploadGroup(ctx context.Context, rec *storage.MatchRecord) (*resolvedUploadGroup, error) {
	orgStore, hasOrg := s.store.(ReplayOrganizationStore)
	groupClient, hasGroupAPI := s.uploader.(BallchasingGroupClient)
	if !hasOrg || !hasGroupAPI || rec == nil {
		return nil, nil
	}

	s.orgMu.Lock()
	defer s.orgMu.Unlock()

	loc := s.location()
	matchTime := s.resolveMatchTime(rec, nil)
	matchDateKey := matchTime.In(loc).Format("2006_01_02")

	gameMode := strings.TrimSpace(rec.GameMode)
	if gameMode == "" {
		gameMode = domain.GameModeCategory(rec.Playlist, 0)
	} else {
		gameMode = domain.NormalizeGameModeCategory(gameMode)
	}

	sessionID := ""
	sessionDateKey := matchDateKey
	if resolvedSess, err := orgStore.FindSessionForMatch(ctx, rec.MatchGUID, matchTime); err == nil && resolvedSess != nil {
		sessionID = resolvedSess.SessionID
		if !resolvedSess.StartedAt.IsZero() {
			sessionDateKey = resolvedSess.StartedAt.In(loc).Format("2006_01_02")
		}
		if rec.Playlist <= 0 && resolvedSess.PlaylistID > 0 && strings.TrimSpace(rec.GameMode) == "" {
			gameMode = domain.GameModeCategory(resolvedSess.PlaylistID, 0)
		}
	} else if strings.TrimSpace(rec.SessionID) != "" {
		sessionID = strings.TrimSpace(rec.SessionID)
		if strings.TrimSpace(rec.ReplayDateKey) != "" {
			sessionDateKey = strings.TrimSpace(rec.ReplayDateKey)
		}
	} else {
		sessionID = "daily:" + matchDateKey
	}

	existing, err := orgStore.GetBallchasingGroup(ctx, sessionID, gameMode)
	if err == nil && existing != nil && strings.TrimSpace(existing.BallchasingGroupID) != "" {
		return &resolvedUploadGroup{
			SessionID: sessionID,
			GameMode:  gameMode,
			GroupID:   existing.BallchasingGroupID,
			GroupName: existing.GroupName,
		}, nil
	}
	if err != nil && !errors.Is(err, storage.ErrGroupNotFound) {
		return nil, fmt.Errorf("looking up persisted ballchasing group for %s/%s: %w", sessionID, gameMode, err)
	}

	seq, err := orgStore.GetOrAllocateSessionSequence(ctx, sessionID, sessionDateKey)
	if err != nil {
		return nil, fmt.Errorf("allocating daily session sequence for %s: %w", sessionID, err)
	}
	groupName := domain.FormatBallchasingGroupName(seq.DateKey, seq.SessionNum, gameMode)

	parentGroup := ""
	if p, ok := s.uploader.(defaultGroupProvider); ok {
		parentGroup = strings.TrimSpace(p.DefaultGroup())
	}

	created, err := groupClient.CreateGroup(ctx, ballchasing.CreateGroupRequest{
		Name:                 groupName,
		Parent:               parentGroup,
		PlayerIdentification: "by-id",
		TeamIdentification:   "by-distinct-players",
	})
	if err != nil {
		return nil, fmt.Errorf("creating ballchasing group %q: %w", groupName, err)
	}

	groupRec := &storage.BallchasingGroupRecord{
		SessionID:           sessionID,
		GameMode:            gameMode,
		DateKey:             seq.DateKey,
		SessionNum:          seq.SessionNum,
		GroupName:           groupName,
		BallchasingGroupID:  created.ID,
		BallchasingGroupURL: created.Location,
		CreatedAt:           s.now().UTC(),
	}
	if err := orgStore.SaveBallchasingGroup(ctx, groupRec); err != nil {
		return nil, fmt.Errorf("persisting ballchasing group %q (%s): %w", groupName, created.ID, err)
	}

	s.logger.Info("created ballchasing replay group",
		slog.String("session_id", sessionID),
		slog.String("game_mode", gameMode),
		slog.String("group_name", groupName),
		slog.String("ballchasing_group_id", created.ID),
	)

	return &resolvedUploadGroup{
		SessionID: sessionID,
		GameMode:  gameMode,
		GroupID:   created.ID,
		GroupName: groupName,
	}, nil
}

func (s *Syncer) patchUploadedReplayMetadata(ctx context.Context, matchGUID, replayID, uploadFilename, groupID string) {
	groupClient, ok := s.uploader.(BallchasingGroupClient)
	if !ok || strings.TrimSpace(replayID) == "" {
		return
	}
	title := strings.TrimSuffix(strings.TrimSpace(uploadFilename), ".replay")
	groupID = strings.TrimSpace(groupID)
	if title == "" && groupID == "" {
		return
	}
	if err := groupClient.PatchReplay(ctx, replayID, ballchasing.PatchReplayRequest{
		Title: title,
		Group: groupID,
	}); err != nil && !isContextCanceled(ctx, err) {
		s.logger.Warn("failed to patch ballchasing replay metadata (non-fatal)",
			slog.String("guid", matchGUID),
			slog.String("ballchasing_id", replayID),
			slog.String("title", title),
			slog.String("group_id", groupID),
			slog.Any("error", err),
		)
	}
}

func highestDailyReplaySeqOnDisk(dir, dateKey string) int {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(dateKey) == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	prefix := dateKey + "_"
	highest := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".replay") {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		sepIdx := strings.IndexByte(rest, '_')
		if sepIdx <= 0 {
			continue
		}
		if seq, convErr := strconv.Atoi(rest[:sepIdx]); convErr == nil && seq > highest {
			highest = seq
		}
	}
	return highest
}

func sameCleanPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func atomicRenameWithRetry(ctx context.Context, oldPath, newPath string, maxRetries int) error {
	if maxRetries <= 0 {
		maxRetries = 1
	}
	var err error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		err = os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		if attempt < maxRetries-1 {
			timer := time.NewTimer(time.Duration((attempt+1)*5) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return err
}
