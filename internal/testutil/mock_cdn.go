package testutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Magic header prefix found in standard Rocket League .replay files
var ReplayMagicBytes = []byte("TAGAME\x00\x00\x00\x00")

// MockCDNServer simulates a pre-signed replay CDN server serving .replay binary payloads.
type MockCDNServer struct {
	server             *httptest.Server
	mu                 sync.RWMutex
	customPayloads     map[string][]byte
	statusCodes        map[string]int
	globalStatusCode   int
	truncateStreams    map[string]bool
	downloadCounts     map[string]int
	totalDownloads     int
	defaultPayloadSize int
}

// NewMockCDNServer initializes and starts a new MockCDNServer.
func NewMockCDNServer() *MockCDNServer {
	mock := &MockCDNServer{
		customPayloads:     make(map[string][]byte),
		statusCodes:        make(map[string]int),
		truncateStreams:    make(map[string]bool),
		downloadCounts:     make(map[string]int),
		defaultPayloadSize: 4096, // 4KB valid replay payload by default
	}

	mock.server = httptest.NewServer(http.HandlerFunc(mock.handleRequest))
	return mock
}

// URL returns the base URL of the mock CDN server.
func (m *MockCDNServer) URL() string {
	return m.server.URL
}

// ReplayURL returns the full downloadable URL for a given match GUID.
func (m *MockCDNServer) ReplayURL(guid string) string {
	return fmt.Sprintf("%s/replays/%s.replay", m.server.URL, guid)
}

// Close shuts down the underlying HTTP server.
func (m *MockCDNServer) Close() {
	m.server.Close()
}

// SetDefaultPayloadSize overrides the size of dynamically generated replays.
func (m *MockCDNServer) SetDefaultPayloadSize(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultPayloadSize = size
}

// SetReplayPayload configures a specific payload for a match GUID.
func (m *MockCDNServer) SetReplayPayload(guid string, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customPayloads[guid] = payload
}

// SetStatusCode configures a specific HTTP status code (e.g. 403, 404, 500) for a match GUID.
func (m *MockCDNServer) SetStatusCode(guid string, code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCodes[guid] = code
}

// SetGlobalStatusCode sets a status code returned for all requests (0 resets to 200).
func (m *MockCDNServer) SetGlobalStatusCode(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globalStatusCode = code
}

// SetTruncateStream configures premature stream truncation for testing network drops.
func (m *MockCDNServer) SetTruncateStream(guid string, truncate bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.truncateStreams[guid] = truncate
}

// GetDownloadCount returns the number of times a match GUID was requested.
func (m *MockCDNServer) GetDownloadCount(guid string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.downloadCounts[guid]
}

// GetTotalDownloads returns the total number of downloads served.
func (m *MockCDNServer) GetTotalDownloads() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalDownloads
}

// ResetCounters clears download metrics.
func (m *MockCDNServer) ResetCounters() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.downloadCounts = make(map[string]int)
	m.totalDownloads = 0
}

// GenerateValidReplay produces a byte slice starting with TAGAME header padded to size.
func GenerateValidReplay(guid string, size int) []byte {
	if size < len(ReplayMagicBytes) {
		size = 2048
	}
	buf := make([]byte, size)
	copy(buf, ReplayMagicBytes)
	// Inject match GUID into replay metadata section
	guidBytes := []byte(guid)
	if len(buf) > len(ReplayMagicBytes)+len(guidBytes) {
		copy(buf[len(ReplayMagicBytes):], guidBytes)
	}
	return buf
}

func (m *MockCDNServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.totalDownloads++

	// Extract GUID from path: e.g. /replays/{guid}.replay
	path := r.URL.Path
	guid := strings.TrimPrefix(path, "/replays/")
	guid = strings.TrimSuffix(guid, ".replay")
	m.downloadCounts[guid]++

	// Check global status code override
	if m.globalStatusCode != 0 && m.globalStatusCode != http.StatusOK {
		code := m.globalStatusCode
		m.mu.Unlock()
		http.Error(w, fmt.Sprintf("Mock CDN Global Error: %d", code), code)
		return
	}

	// Check per-guid status code override
	if code, exists := m.statusCodes[guid]; exists && code != http.StatusOK {
		m.mu.Unlock()
		http.Error(w, fmt.Sprintf("Mock CDN Error for %s: %d", guid, code), code)
		return
	}

	// Retrieve or generate payload
	var payload []byte
	if custom, exists := m.customPayloads[guid]; exists {
		payload = custom
	} else {
		payload = GenerateValidReplay(guid, m.defaultPayloadSize)
	}

	truncate := m.truncateStreams[guid]
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.replay"`, guid))

	if truncate {
		// Truncate stream: send partial content and close immediately
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		if len(payload) > 128 {
			w.Write(payload[:128])
		}
		// Force connection close without sending full body
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			if conn != nil {
				conn.Close()
			}
		}
		return
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
	w.WriteHeader(http.StatusOK)
	w.Write(payload)
}
