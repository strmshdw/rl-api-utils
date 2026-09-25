package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UploadedReplay captures all metadata and content of an upload received by MockBallchasingServer.
type UploadedReplay struct {
	MatchGUID  string
	FileName   string
	FileSize   int64
	FileBytes  []byte
	Visibility string
	Group      string
	AuthHeader string
	Timestamp  time.Time
}

// MockBallchasingServer simulates the Ballchasing.com REST API (POST /v2/upload and GET /).
type MockBallchasingServer struct {
	server *httptest.Server
	mu     sync.RWMutex

	expectedToken      string
	uploads            []UploadedReplay
	uploadsByGUID      map[string]*UploadedReplay
	duplicateGUIDs     map[string]string // guid -> existing ballchasing ID
	alwaysDuplicate    bool
	alwaysUnauthorized bool

	// Rate limiting controls
	rateLimitFailuresRemaining int
	retryAfterSeconds          int
	customRetryAfterHeader     string

	// Fault injection
	forcedStatusCode int
	forcedErrorMsg   string
}

// NewMockBallchasingServer creates and starts a new MockBallchasingServer.
func NewMockBallchasingServer() *MockBallchasingServer {
	mock := &MockBallchasingServer{
		expectedToken:     "test-ballchasing-token",
		uploads:           make([]UploadedReplay, 0),
		uploadsByGUID:     make(map[string]*UploadedReplay),
		duplicateGUIDs:    make(map[string]string),
		retryAfterSeconds: 1,
	}

	mock.server = httptest.NewServer(http.HandlerFunc(mock.handleRequest))
	return mock
}

// URL returns the base URL of the mock Ballchasing server (e.g. http://127.0.0.1:xxxxx).
func (m *MockBallchasingServer) URL() string {
	return m.server.URL
}

// Close terminates the mock HTTP server.
func (m *MockBallchasingServer) Close() {
	m.server.Close()
}

// SetExpectedToken configures the expected API token in the Authorization header.
func (m *MockBallchasingServer) SetExpectedToken(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expectedToken = token
}

// SetDuplicateGUID marks a specific match GUID to return HTTP 409 with the specified ballchasing ID.
func (m *MockBallchasingServer) SetDuplicateGUID(guid, existingID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.duplicateGUIDs[guid] = existingID
}

// SetAlwaysDuplicate configures the server to return HTTP 409 Conflict for all uploads.
func (m *MockBallchasingServer) SetAlwaysDuplicate(always bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alwaysDuplicate = always
}

// SetAlwaysUnauthorized forces HTTP 401 Unauthorized for all requests.
func (m *MockBallchasingServer) SetAlwaysUnauthorized(unauth bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alwaysUnauthorized = unauth
}

// SimulateRateLimit configures the server to return HTTP 429 for the next `failures` requests.
func (m *MockBallchasingServer) SimulateRateLimit(failures int, retryAfterSec int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rateLimitFailuresRemaining = failures
	m.retryAfterSeconds = retryAfterSec
	m.customRetryAfterHeader = ""
}

// SetCustomRetryAfterHeader allows testing non-integer or malformed Retry-After headers.
func (m *MockBallchasingServer) SetCustomRetryAfterHeader(headerVal string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customRetryAfterHeader = headerVal
}

// SetForcedStatus sets a forced HTTP status code and error message. Set code to 0 to clear.
func (m *MockBallchasingServer) SetForcedStatus(code int, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forcedStatusCode = code
	m.forcedErrorMsg = errMsg
}

// GetUploadCount returns the total number of successful or accepted uploads.
func (m *MockBallchasingServer) GetUploadCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.uploads)
}

// GetUploads returns a copy of all recorded uploads.
func (m *MockBallchasingServer) GetUploads() []UploadedReplay {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]UploadedReplay, len(m.uploads))
	copy(res, m.uploads)
	return res
}

// GetUpload returns the uploaded replay for a specific match GUID, if present.
func (m *MockBallchasingServer) GetUpload(guid string) *UploadedReplay {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.uploadsByGUID[guid]
}

// Reset clears all recorded uploads and fault configurations.
func (m *MockBallchasingServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploads = make([]UploadedReplay, 0)
	m.uploadsByGUID = make(map[string]*UploadedReplay)
	m.duplicateGUIDs = make(map[string]string)
	m.alwaysDuplicate = false
	m.alwaysUnauthorized = false
	m.rateLimitFailuresRemaining = 0
	m.retryAfterSeconds = 1
	m.customRetryAfterHeader = ""
	m.forcedStatusCode = 0
	m.forcedErrorMsg = ""
}

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	hexStr := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32])
}

func (m *MockBallchasingServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// 1. Handle Ping / Verification endpoint: GET / or GET /api/
	if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/api" || r.URL.Path == "/api/") {
		m.handlePing(w, r)
		return
	}

	// 2. Handle Replay Upload: POST /v2/upload or POST /api/v2/upload
	if r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/v2/upload")) {
		m.handleUpload(w, r)
		return
	}

	http.NotFound(w, r)
}

func (m *MockBallchasingServer) handlePing(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	m.mu.RLock()
	expected := m.expectedToken
	unauth := m.alwaysUnauthorized
	m.mu.RUnlock()

	if unauth || auth == "" || (expected != "" && auth != expected) || strings.HasPrefix(auth, "Bearer ") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid API key."})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"chaser":   true,
		"type":     "regular",
		"name":     "MockUser",
		"steam_id": "76561198000000000",
	})
}

func (m *MockBallchasingServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")

	m.mu.Lock()
	// Check forced status code first
	if m.forcedStatusCode != 0 {
		code := m.forcedStatusCode
		msg := m.forcedErrorMsg
		if msg == "" {
			msg = "forced error"
		}
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}

	// Check authentication: strictly reject Bearer prefix and missing or mismatching keys
	if m.alwaysUnauthorized || auth == "" || strings.HasPrefix(auth, "Bearer ") || (m.expectedToken != "" && auth != m.expectedToken) {
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid API key."})
		return
	}

	// Check rate limiting
	if m.rateLimitFailuresRemaining > 0 {
		m.rateLimitFailuresRemaining--
		retryAfter := strconv.Itoa(m.retryAfterSeconds)
		if m.customRetryAfterHeader != "" {
			retryAfter = m.customRetryAfterHeader
		}
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", retryAfter)
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "too many requests"})
		return
	}

	m.mu.Unlock()

	// Validate query parameters
	visibility := r.URL.Query().Get("visibility")
	if visibility != "" && visibility != "public" && visibility != "unlisted" && visibility != "private" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid visibility: %s", visibility)})
		return
	}
	group := r.URL.Query().Get("group")

	// Parse multipart form
	err := r.ParseMultipartForm(32 << 20) // 32MB max in-memory
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to parse multipart form: %v", err)})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing 'file' form part"})
		return
	}
	defer file.Close()

	payloadBytes, err := io.ReadAll(file)
	if err != nil || len(payloadBytes) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "empty or unreadable replay file"})
		return
	}

	// Extract GUID from filename: e.g. <guid>.replay
	fileName := header.Filename
	guid := strings.TrimSuffix(fileName, ".replay")

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check duplicate conditions (HTTP 409)
	existingID, isDuplicate := m.duplicateGUIDs[guid]
	if m.alwaysDuplicate || isDuplicate {
		if existingID == "" {
			existingID = generateUUID()
		}
		record := UploadedReplay{
			MatchGUID:  guid,
			FileName:   fileName,
			FileSize:   int64(len(payloadBytes)),
			FileBytes:  payloadBytes,
			Visibility: visibility,
			Group:      group,
			AuthHeader: auth,
			Timestamp:  time.Now(),
		}
		m.uploads = append(m.uploads, record)
		m.uploadsByGUID[guid] = &record

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":    "duplicate replay",
			"id":       existingID,
			"location": fmt.Sprintf("https://ballchasing.com/replay/%s", existingID),
		})
		return
	}

	// Normal successful upload: HTTP 201 Created
	newReplayID := generateUUID()
	locationURL := fmt.Sprintf("https://ballchasing.com/replay/%s", newReplayID)

	record := UploadedReplay{
		MatchGUID:  guid,
		FileName:   fileName,
		FileSize:   int64(len(payloadBytes)),
		FileBytes:  payloadBytes,
		Visibility: visibility,
		Group:      group,
		AuthHeader: auth,
		Timestamp:  time.Now(),
	}
	m.uploads = append(m.uploads, record)
	m.uploadsByGUID[guid] = &record

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", locationURL)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":       newReplayID,
		"location": locationURL,
	})
}
