package syncer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/dank/rl-api-utils/internal/ballchasing"
)

type mockHistoryProvider struct {
	mu      sync.Mutex
	matches []DiscoveredMatch
	calls   int
	err     error
	closed  bool
}

func newMockHistoryProvider(matches ...DiscoveredMatch) *mockHistoryProvider {
	return &mockHistoryProvider{matches: matches}
}

func (p *mockHistoryProvider) GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	res := make([]DiscoveredMatch, len(p.matches))
	copy(res, p.matches)
	return res, nil
}

func (p *mockHistoryProvider) SetMatches(matches []DiscoveredMatch) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.matches = matches
}

func (p *mockHistoryProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

type mockDownloader struct {
	mu           sync.Mutex
	downloaded   map[string]string
	failForGUIDs map[string]error
	calls        int
	blockChan    chan struct{}
}

func newMockDownloader() *mockDownloader {
	return &mockDownloader{
		downloaded:   make(map[string]string),
		failForGUIDs: make(map[string]error),
	}
}

func (d *mockDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	d.mu.Lock()
	d.calls++
	block := d.blockChan
	if err, ok := d.failForGUIDs[matchGUID]; ok {
		d.mu.Unlock()
		return "", err
	}
	d.mu.Unlock()

	if block != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-block:
		}
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	p := filepath.Join(destDir, fmt.Sprintf("%s.replay", matchGUID))
	d.mu.Lock()
	d.downloaded[matchGUID] = p
	d.mu.Unlock()
	return p, nil
}

type mockUploader struct {
	mu           sync.Mutex
	uploaded     map[string]string
	failForGUIDs map[string]error
	duplicates   map[string]bool
	calls        int
	blockChan    chan struct{}
}

func newMockUploader() *mockUploader {
	return &mockUploader{
		uploaded:     make(map[string]string),
		failForGUIDs: make(map[string]error),
		duplicates:   make(map[string]bool),
	}
}

func (u *mockUploader) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	u.mu.Lock()
	u.calls++
	block := u.blockChan
	if err, ok := u.failForGUIDs[matchGUID]; ok {
		u.mu.Unlock()
		return nil, err
	}
	isDup := u.duplicates[matchGUID]
	u.mu.Unlock()

	if block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-block:
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	u.mu.Lock()
	u.uploaded[matchGUID] = filePath
	u.mu.Unlock()

	return &UploadResult{
		ID:          fmt.Sprintf("bc-%s", matchGUID),
		Location:    fmt.Sprintf("https://ballchasing.com/replay/bc-%s", matchGUID),
		IsDuplicate: isDup,
	}, nil
}

type mockStatsFetcher struct {
	mu        sync.Mutex
	replays   map[string]*ballchasing.ReplayItem
	failForID map[string]error
	calls     map[string]int
	blockChan chan struct{}
}

func newMockStatsFetcher() *mockStatsFetcher {
	return &mockStatsFetcher{
		replays:   make(map[string]*ballchasing.ReplayItem),
		failForID: make(map[string]error),
		calls:     make(map[string]int),
	}
}

func (f *mockStatsFetcher) GetReplay(ctx context.Context, id string) (*ballchasing.ReplayItem, error) {
	f.mu.Lock()
	f.calls[id]++
	block := f.blockChan
	failErr, hasFail := f.failForID[id]
	item, hasItem := f.replays[id]
	f.mu.Unlock()

	if block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-block:
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if hasFail {
		return nil, failErr
	}
	if hasItem {
		return item, nil
	}
	return nil, errors.New("mock replay not found")
}

type selectiveBlockingDownloader struct {
	inner        *mockDownloader
	blockForGUID string
	onBlock      func()
}

func (d *selectiveBlockingDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	if matchGUID == d.blockForGUID {
		if d.onBlock != nil {
			d.onBlock()
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	return d.inner.DownloadReplay(ctx, matchGUID, replayURL, destDir)
}

type selectiveBlockingUploader struct {
	inner        *mockUploader
	blockForGUID string
	onBlock      func()
}

func (u *selectiveBlockingUploader) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	if matchGUID == u.blockForGUID {
		if u.onBlock != nil {
			u.onBlock()
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return u.inner.UploadReplay(ctx, matchGUID, filePath)
}
