package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

type resolvedBallchasingLink struct {
	ID  string
	URL string
}

func (d *Daemon) getCachedBallchasingLink(canonicalGUID string) (string, string) {
	key := normalizeMatchGUIDKey(canonicalGUID)
	if key == "" {
		return "", ""
	}
	d.replayLinkMu.RLock()
	link, ok := d.replayLinkCache[key]
	d.replayLinkMu.RUnlock()
	if !ok {
		return "", ""
	}
	return link.ID, link.URL
}

func (d *Daemon) putCachedBallchasingLink(canonicalGUID, bcID, bcURL string) {
	key := normalizeMatchGUIDKey(canonicalGUID)
	bcID = strings.TrimSpace(bcID)
	if key == "" || bcID == "" {
		return
	}
	bcURL = storage.NormalizeBallchasingURL(bcID, bcURL)
	d.replayLinkMu.Lock()
	if d.replayLinkCache == nil {
		d.replayLinkCache = make(map[string]resolvedBallchasingLink)
	}
	d.replayLinkCache[key] = resolvedBallchasingLink{ID: bcID, URL: bcURL}
	d.replayLinkMu.Unlock()
}

func (d *Daemon) resolveBallchasingInfo(rec *storage.MatchRecord, canonicalGUID string) (string, string) {
	bcID, bcURL := extractBallchasingInfo(rec)
	if bcID != "" {
		d.putCachedBallchasingLink(canonicalGUID, bcID, bcURL)
		return bcID, bcURL
	}
	return d.getCachedBallchasingLink(canonicalGUID)
}

func (d *Daemon) lookupBallchasingFallback(
	ctx context.Context,
	canonicalGUID string,
	rec *storage.MatchRecord,
	sessMatch *session.SessionMatchDetail,
) (*ballchasing.ReplayItem, error) {
	query := d.buildReplaySearchQuery(ctx, canonicalGUID, rec, sessMatch)
	item, err := d.ballchasingClient.FindReplayByMetadata(ctx, query)
	if err != nil {
		return nil, err
	}
	if item != nil && strings.TrimSpace(item.ID) != "" {
		d.putCachedBallchasingLink(canonicalGUID, item.ID, item.Link)
		if rec != nil && d.stateStore != nil {
			if linker, ok := d.stateStore.(matchBallchasingLinker); ok {
				_ = linker.UpdateMatchBallchasingLink(ctx, rec.MatchGUID, item.ID, item.Link)
			}
		}
	}
	return item, nil
}

func (d *Daemon) buildReplaySearchQuery(
	ctx context.Context,
	canonicalGUID string,
	rec *storage.MatchRecord,
	sessMatch *session.SessionMatchDetail,
) ballchasing.ReplaySearchQuery {
	q := ballchasing.ReplaySearchQuery{
		MatchGUID: canonicalGUID,
	}
	seenPlayers := make(map[string]struct{})
	addPlayer := func(raw string) {
		name := strings.TrimSpace(raw)
		if name == "" {
			return
		}
		lower := strings.ToLower(name)
		if _, exists := seenPlayers[lower]; exists {
			return
		}
		seenPlayers[lower] = struct{}{}
		q.PlayerNames = append(q.PlayerNames, name)
	}

	if rec != nil {
		if rec.RecordStartTimestamp > 0 {
			q.Timestamp = time.Unix(rec.RecordStartTimestamp, 0).UTC()
		}
		q.PlaylistID = rec.Playlist
		q.MapName = rec.MapName
		q.BlueScore = rec.BlueScore
		q.OrangeScore = rec.OrangeScore
		if rec.ReplayStatsJSON != "" {
			var st storage.ReplayMatchStats
			if json.Unmarshal([]byte(rec.ReplayStatsJSON), &st) == nil {
				for _, p := range st.Players {
					addPlayer(p.Name)
				}
			}
		}
		if len(q.PlayerNames) == 0 && d.stateStore != nil {
			if mps, err := d.stateStore.GetMatchPlayers(ctx, rec.MatchGUID); err == nil {
				for _, mp := range mps {
					if name := strings.TrimSpace(mp.PlayerName); name != "" {
						addPlayer(name)
						continue
					}
					if strings.TrimSpace(mp.StatsJSON) != "" {
						var stPlayer struct {
							Name string `json:"name"`
						}
						if json.Unmarshal([]byte(mp.StatsJSON), &stPlayer) == nil {
							addPlayer(stPlayer.Name)
						}
					}
				}
			}
		}
	}
	if sessMatch != nil {
		if q.Timestamp.IsZero() && !sessMatch.StartedAt.IsZero() {
			q.Timestamp = sessMatch.StartedAt.UTC()
		}
		if q.PlaylistID == 0 {
			q.PlaylistID = sessMatch.PlaylistID
		}
		if q.BlueScore == 0 && q.OrangeScore == 0 {
			q.BlueScore = sessMatch.BlueScore
			q.OrangeScore = sessMatch.OrangeScore
		}
		if len(q.PlayerNames) == 0 {
			for _, p := range sessMatch.Players {
				addPlayer(p.Name)
			}
		}
	}
	if d.cfg != nil {
		addPlayer(d.cfg.PlayerTracking.LocalPlayerName)
	}
	return q
}

// mirrorSessionCacheAlias ensures a session-only cached replay can still be found on disk
// by canonicalGUID across daemon restarts before the match exists in the matches table.
func (d *Daemon) mirrorSessionCacheAlias(bcCachePath, canonicalGUID string) {
	safeGUID := sanitizeCacheKey(canonicalGUID)
	if safeGUID == "" {
		return
	}
	guidPath := filepath.Join(d.resolveReplayCacheDir(), safeGUID+".replay")
	if filepath.Clean(guidPath) == filepath.Clean(bcCachePath) {
		return
	}
	if _, ok := statRegularNonEmptyFile(guidPath); ok {
		return
	}
	if err := os.Link(bcCachePath, guidPath); err == nil {
		return
	}
	// Fallback atomic copy when hardlinking is unsupported on the target filesystem.
	src, err := os.Open(bcCachePath)
	if err != nil {
		return
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(guidPath), ".tmp-alias-*.replay")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, src); err != nil {
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Rename(tmpPath, guidPath); err == nil {
		committed = true
	}
}

func (d *Daemon) resolveMatchOrSession(ctx context.Context, guid string) (*storage.MatchRecord, *session.SessionMatchDetail, error) {
	if d.stateStore != nil {
		rec, err := d.stateStore.GetMatch(ctx, guid)
		if err == nil && rec != nil {
			return rec, nil, nil
		}
		if err != nil && !errors.Is(err, storage.ErrMatchNotFound) {
			return nil, nil, err
		}
	}

	if sm := d.findMatchInSessions(ctx, guid); sm != nil {
		return nil, sm, nil
	}
	return nil, nil, storage.ErrMatchNotFound
}

func (d *Daemon) findMatchInSessions(ctx context.Context, guid string) *session.SessionMatchDetail {
	normTarget := normalizeMatchGUIDKey(guid)
	if normTarget == "" {
		return nil
	}

	if d.sessionTracker != nil {
		if sum := d.sessionTracker.GetSessionSummary(); sum != nil {
			for _, m := range sum.Matches {
				if m != nil && normalizeMatchGUIDKey(m.MatchGUID) == normTarget {
					return m
				}
			}
			if sum.ContinuableSession != nil {
				for _, m := range sum.ContinuableSession.Matches {
					if m != nil && normalizeMatchGUIDKey(m.MatchGUID) == normTarget {
						return m
					}
				}
			}
		}
	}

	if d.stateStore != nil {
		recs, _, err := d.stateStore.ListSessions(ctx, replaySessionScanLimit, 0)
		if err == nil {
			for _, sRec := range recs {
				if sRec == nil || strings.TrimSpace(sRec.MatchesJSON) == "" || sRec.MatchesJSON == "[]" {
					continue
				}
				var matches []*session.SessionMatchDetail
				if json.Unmarshal([]byte(sRec.MatchesJSON), &matches) != nil {
					continue
				}
				for _, m := range matches {
					if m != nil && normalizeMatchGUIDKey(m.MatchGUID) == normTarget {
						return m
					}
				}
			}
		}
	}
	return nil
}

func (d *Daemon) findLocalReplayFile(rec *storage.MatchRecord, canonicalGUID, bcID string) (string, string, int64) {
	var replayDir string
	if d.cfg != nil {
		replayDir = strings.TrimSpace(d.cfg.Sync.ReplayDir)
	}

	var localCandidates []string
	if rec != nil {
		if lp := strings.TrimSpace(rec.LocalFilePath); lp != "" {
			localCandidates = append(localCandidates, lp)
		}
		if rn := strings.TrimSpace(rec.ReplayName); rn != "" && replayDir != "" {
			localCandidates = append(localCandidates, filepath.Join(replayDir, filepath.Base(rn)))
		}
	}
	if safeGUID := sanitizeCacheKey(canonicalGUID); safeGUID != "" && replayDir != "" {
		localCandidates = append(localCandidates, filepath.Join(replayDir, safeGUID+".replay"))
	}

	for _, cand := range localCandidates {
		if size, ok := statRegularNonEmptyFile(cand); ok {
			return cand, replaySourceLocal, size
		}
	}

	cacheDir := d.resolveReplayCacheDir()
	var cacheCandidates []string
	if safeBC := sanitizeCacheKey(bcID); safeBC != "" {
		cacheCandidates = append(cacheCandidates, filepath.Join(cacheDir, safeBC+".replay"))
	}
	if safeGUID := sanitizeCacheKey(canonicalGUID); safeGUID != "" {
		cacheCandidates = append(cacheCandidates, filepath.Join(cacheDir, safeGUID+".replay"))
	}
	for _, cand := range cacheCandidates {
		if size, ok := statRegularNonEmptyFile(cand); ok {
			return cand, replaySourceCache, size
		}
	}

	return "", "", 0
}

func (d *Daemon) resolveReplayCacheDir() string {
	if d.cfg != nil {
		if dir := strings.TrimSpace(d.cfg.Sync.ReplayDir); dir != "" {
			return filepath.Join(dir, ballchasingCacheSubdir)
		}
	}
	return filepath.Join(os.TempDir(), "rl-sync-"+ballchasingCacheSubdir)
}

func (d *Daemon) ballchasingCachePath(bcID, canonicalGUID string) string {
	key := sanitizeCacheKey(bcID)
	if key == "" {
		key = sanitizeCacheKey(canonicalGUID)
	}
	if key == "" {
		key = "replay"
	}
	return filepath.Join(d.resolveReplayCacheDir(), key+".replay")
}

func extractBallchasingInfo(rec *storage.MatchRecord) (string, string) {
	if rec == nil {
		return "", ""
	}
	id := strings.TrimSpace(rec.BallchasingID)
	rawURL := strings.TrimSpace(rec.BallchasingURL)

	if id == "" && rawURL != "" {
		id = parseBallchasingIDFromURL(rawURL)
	}
	if id == "" && strings.TrimSpace(rec.ReplayStatsJSON) != "" {
		var meta struct {
			BallchasingID  string `json:"ballchasing_id"`
			BallchasingURL string `json:"ballchasing_url"`
		}
		if json.Unmarshal([]byte(rec.ReplayStatsJSON), &meta) == nil {
			if strings.TrimSpace(meta.BallchasingID) != "" {
				id = strings.TrimSpace(meta.BallchasingID)
			} else if strings.TrimSpace(meta.BallchasingURL) != "" {
				id = parseBallchasingIDFromURL(meta.BallchasingURL)
			}
			if rawURL == "" && strings.TrimSpace(meta.BallchasingURL) != "" {
				rawURL = strings.TrimSpace(meta.BallchasingURL)
			}
		}
	}
	if id != "" {
		rawURL = storage.NormalizeBallchasingURL(id, rawURL)
	}
	return id, rawURL
}

func parseBallchasingIDFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	for _, prefix := range []string{"replay/", "api/replays/", "replays/"} {
		if strings.HasPrefix(p, prefix) {
			rest := strings.TrimPrefix(p, prefix)
			seg, _, _ := strings.Cut(rest, "/")
			seg = strings.TrimSpace(seg)
			if seg != "" {
				return seg
			}
		}
	}
	return ""
}

func resolveCanonicalMatchGUID(fallbackGUID string, rec *storage.MatchRecord, sessMatch *session.SessionMatchDetail) string {
	if rec != nil && strings.TrimSpace(rec.MatchGUID) != "" {
		return strings.TrimSpace(rec.MatchGUID)
	}
	if sessMatch != nil && strings.TrimSpace(sessMatch.MatchGUID) != "" {
		return strings.TrimSpace(sessMatch.MatchGUID)
	}
	return fallbackGUID
}

func resolveReplayFilename(rec *storage.MatchRecord, canonicalGUID string) string {
	if rec != nil {
		if rn := strings.TrimSpace(rec.ReplayName); rn != "" {
			return ensureReplayExt(filepath.Base(rn))
		}
		if lp := strings.TrimSpace(rec.LocalFilePath); lp != "" {
			return ensureReplayExt(filepath.Base(lp))
		}
	}
	return ensureReplayExt(canonicalGUID)
}

func ensureReplayExt(name string) string {
	clean := sanitizeHeaderFilename(name)
	if clean == "" {
		return "match.replay"
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".replay") {
		return clean + ".replay"
	}
	return clean
}

func sanitizeHeaderFilename(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' || r == '/' || r == ';' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func sanitizeCacheKey(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func normalizeMatchGUIDKey(guid string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(guid), "-", ""))
}

func statRegularNonEmptyFile(path string) (int64, bool) {
	if strings.TrimSpace(path) == "" {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() || info.Size() <= 0 {
		return 0, false
	}
	return info.Size(), true
}

func isTruthyQuery(raw string) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	return v == "1" || v == "true" || v == "yes"
}
