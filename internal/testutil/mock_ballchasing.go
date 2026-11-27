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

// CreatedGroup captures a POST /groups request received by MockBallchasingServer.
type CreatedGroup struct {
	ID                   string
	Name                 string
	Parent               string
	PlayerIdentification string
	TeamIdentification   string
	AuthHeader           string
	Timestamp            time.Time
}

// ReplayPatch captures a PATCH /replays/{id} request received by MockBallchasingServer.
type ReplayPatch struct {
	ReplayID   string
	Title      string
	Group      string
	Visibility string
	AuthHeader string
	Timestamp  time.Time
}

// MockBallchasingServer simulates the Ballchasing.com REST API (POST /v2/upload, POST /groups, PATCH /replays/{id}, and GET /).
type MockBallchasingServer struct {
	server *httptest.Server
	mu     sync.RWMutex

	expectedToken      string
	uploads            []UploadedReplay
	uploadsByGUID      map[string]*UploadedReplay
	groups             []CreatedGroup
	patches            []ReplayPatch
	patchesByReplayID  map[string]*ReplayPatch
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

	// Replay file downloads and search fixtures
	replayFiles map[string][]byte
	replayList  []map[string]any
}

// NewMockBallchasingServer creates and starts a new MockBallchasingServer.
func NewMockBallchasingServer() *MockBallchasingServer {
	mock := &MockBallchasingServer{
		expectedToken:     "test-ballchasing-token",
		uploads:           make([]UploadedReplay, 0),
		uploadsByGUID:     make(map[string]*UploadedReplay),
		groups:            make([]CreatedGroup, 0),
		patches:           make([]ReplayPatch, 0),
		patchesByReplayID: make(map[string]*ReplayPatch),
		duplicateGUIDs:    make(map[string]string),
		retryAfterSeconds: 1,
		replayFiles:       make(map[string][]byte),
		replayList:        make([]map[string]any, 0),
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

// SetReplayFile registers a raw .replay payload served by GET /replays/{id}/file.
func (m *MockBallchasingServer) SetReplayFile(replayID string, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.replayFiles[replayID] = cp
}

// SetReplayList configures the JSON replay items returned by GET /replays.
func (m *MockBallchasingServer) SetReplayList(items []map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replayList = items
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

// GetGroups returns a copy of all groups created via POST /groups.
func (m *MockBallchasingServer) GetGroups() []CreatedGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]CreatedGroup, len(m.groups))
	copy(res, m.groups)
	return res
}

// GetGroupCount returns the number of groups created via POST /groups.
func (m *MockBallchasingServer) GetGroupCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.groups)
}

// GetPatches returns a copy of all PATCH /replays/{id} requests received.
func (m *MockBallchasingServer) GetPatches() []ReplayPatch {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]ReplayPatch, len(m.patches))
	copy(res, m.patches)
	return res
}

// GetReplayPatch returns the most recent PATCH received for replayID, if any.
func (m *MockBallchasingServer) GetReplayPatch(replayID string) *ReplayPatch {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.patchesByReplayID[replayID]; ok {
		cp := *p
		return &cp
	}
	return nil
}

// Reset clears all recorded uploads, groups, patches, and fault configurations.
func (m *MockBallchasingServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploads = make([]UploadedReplay, 0)
	m.uploadsByGUID = make(map[string]*UploadedReplay)
	m.groups = make([]CreatedGroup, 0)
	m.patches = make([]ReplayPatch, 0)
	m.patchesByReplayID = make(map[string]*ReplayPatch)
	m.duplicateGUIDs = make(map[string]string)
	m.alwaysDuplicate = false
	m.alwaysUnauthorized = false
	m.rateLimitFailuresRemaining = 0
	m.retryAfterSeconds = 1
	m.customRetryAfterHeader = ""
	m.forcedStatusCode = 0
	m.forcedErrorMsg = ""
	m.replayFiles = make(map[string][]byte)
	m.replayList = make([]map[string]any, 0)
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

	// 3. Handle Group Creation: POST /groups or POST /api/groups
	if r.Method == http.MethodPost && (r.URL.Path == "/groups" || r.URL.Path == "/api/groups") {
		m.handleCreateGroup(w, r)
		return
	}

	// 4. Handle Replay Metadata Patch: PATCH /replays/{id} or PATCH /api/replays/{id}
	if r.Method == http.MethodPatch && (strings.HasPrefix(r.URL.Path, "/replays/") || strings.HasPrefix(r.URL.Path, "/api/replays/")) {
		m.handlePatchReplay(w, r)
		return
	}

	// 5. Handle Replay Binary Download: GET /replays/{id}/file or GET /api/replays/{id}/file
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/file") &&
		(strings.HasPrefix(r.URL.Path, "/replays/") || strings.HasPrefix(r.URL.Path, "/api/replays/")) {
		m.handleDownloadReplayFile(w, r)
		return
	}

	// 6. Handle Replay Search / List: GET /replays or GET /api/replays
	if r.Method == http.MethodGet && (r.URL.Path == "/replays" || r.URL.Path == "/api/replays") {
		m.handleListReplays(w, r)
		return
	}

	http.NotFound(w, r)
}

func (m *MockBallchasingServer) handleDownloadReplayFile(w http.ResponseWriter, r *http.Request) {
	if _, ok := m.checkMutateGates(w, r); !ok {
		return
	}
	replayID := strings.TrimPrefix(r.URL.Path, "/api/replays/")
	replayID = strings.TrimPrefix(replayID, "/replays/")
	replayID = strings.TrimSuffix(replayID, "/file")
	replayID = strings.Trim(replayID, "/")

	m.mu.RLock()
	payload, exists := m.replayFiles[replayID]
	m.mu.RUnlock()
	if !exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "replay not found"})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (m *MockBallchasingServer) handleListReplays(w http.ResponseWriter, r *http.Request) {
	if _, ok := m.checkMutateGates(w, r); !ok {
		return
	}
	m.mu.RLock()
	items := m.replayList
	m.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"count": len(items),
		"list":  items,
		"next":  "",
	})
}

func (m *MockBallchasingServer) checkMutateGates(w http.ResponseWriter, r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	m.mu.Lock()
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
		return "", false
	}
	if m.alwaysUnauthorized || auth == "" || strings.HasPrefix(auth, "Bearer ") || (m.expectedToken != "" && auth != m.expectedToken) {
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid API key."})
		return "", false
	}
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
		return "", false
	}
	m.mu.Unlock()
	return auth, true
}

func (m *MockBallchasingServer) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	auth, ok := m.checkMutateGates(w, r)
	if !ok {
		return
	}
	var req struct {
		Name                 string `json:"name"`
		Parent               string `json:"parent"`
		PlayerIdentification string `json:"player_identification"`
		TeamIdentification   string `json:"team_identification"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json payload"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group name is required"})
		return
	}

	groupID := fmt.Sprintf("%s-%s", strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Name), " ", "-")), generateUUID()[:8])
	location := fmt.Sprintf("https://ballchasing.com/api/groups/%s", groupID)

	m.mu.Lock()
	m.groups = append(m.groups, CreatedGroup{
		ID:                   groupID,
		Name:                 req.Name,
		Parent:               req.Parent,
		PlayerIdentification: req.PlayerIdentification,
		TeamIdentification:   req.TeamIdentification,
		AuthHeader:           auth,
		Timestamp:            time.Now(),
	})
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":       groupID,
		"location": location,
	})
}

func (m *MockBallchasingServer) handlePatchReplay(w http.ResponseWriter, r *http.Request) {
	auth, ok := m.checkMutateGates(w, r)
	if !ok {
		return
	}
	replayID := strings.TrimPrefix(r.URL.Path, "/api/replays/")
	replayID = strings.TrimPrefix(replayID, "/replays/")
	replayID = strings.TrimSpace(replayID)
	if replayID == "" {
		http.NotFound(w, r)
		return
	}

	var req struct {
		Title      string `json:"title"`
		Group      string `json:"group"`
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json payload"})
		return
	}

	patch := ReplayPatch{
		ReplayID:   replayID,
		Title:      req.Title,
		Group:      req.Group,
		Visibility: req.Visibility,
		AuthHeader: auth,
		Timestamp:  time.Now(),
	}
	m.mu.Lock()
	m.patches = append(m.patches, patch)
	m.patchesByReplayID[replayID] = &patch
	m.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
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

	// Check duplicate conditions (HTTP 409) by filename GUID or payload-embedded GUID.
	existingID, isDuplicate := m.duplicateGUIDs[guid]
	if !isDuplicate && len(m.duplicateGUIDs) > 0 && len(payloadBytes) > 0 {
		for dupKey, dupID := range m.duplicateGUIDs {
			if dupKey != "" && strings.Contains(string(payloadBytes), dupKey) {
				existingID = dupID
				isDuplicate = true
				break
			}
		}
	}
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
