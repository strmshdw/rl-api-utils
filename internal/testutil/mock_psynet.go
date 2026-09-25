package testutil

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// MockMatchEntry models the GetMatchHistory item structure returned by PsyNet.
type MockMatchEntry struct {
	ReplayURL string    `json:"ReplayUrl"`
	Match     MockMatch `json:"Match"`
}

// MockMatch contains core match metadata fields.
type MockMatch struct {
	MatchGUID            string   `json:"MatchGUID"`
	RecordStartTimestamp int64    `json:"RecordStartTimestamp"`
	MapName              string   `json:"MapName"`
	Playlist             int      `json:"Playlist"`
	SecondsPlayed        float64  `json:"SecondsPlayed"`
	WinningTeam          int      `json:"WinningTeam"`
	Team0Score           int      `json:"Team0Score"`
	Team1Score           int      `json:"Team1Score"`
	Mutators             []string `json:"Mutators,omitempty"`
}

// MockAuthPlayerRequest captures the incoming HTTP authentication body.
type MockAuthPlayerRequest struct {
	Platform       string `json:"Platform"`
	PlayerName     string `json:"PlayerName"`
	PlayerID       string `json:"PlayerID"`
	AuthTicket     string `json:"AuthTicket"`
	EpicAuthTicket string `json:"EpicAuthTicket"`
	EpicAccountID  string `json:"EpicAccountID"`
}

// NewMockMatchEntry is a constructor helper for building a valid mock match entry.
func NewMockMatchEntry(guid, replayURL, mapName string, playlist int) MockMatchEntry {
	if mapName == "" {
		mapName = "Wasteland_P"
	}
	if playlist == 0 {
		playlist = 2 // 2v2 Competitive
	}
	return MockMatchEntry{
		ReplayURL: replayURL,
		Match: MockMatch{
			MatchGUID:            guid,
			RecordStartTimestamp: time.Now().Unix(),
			MapName:              mapName,
			Playlist:             playlist,
			SecondsPlayed:        300.0,
			WinningTeam:          0,
			Team0Score:           3,
			Team1Score:           1,
		},
	}
}

// MockPsyNetServer simulates the Rocket League PsyNet HTTP bootstrap and WebSocket RPC server.
type MockPsyNetServer struct {
	server *httptest.Server
	mu     sync.RWMutex

	matches             []MockMatchEntry
	authRequests        []MockAuthPlayerRequest
	historyRequestCount int
	pingCount           int
	activeConns         []net.Conn
}

// NewMockPsyNetServer creates and starts a new MockPsyNetServer.
func NewMockPsyNetServer() *MockPsyNetServer {
	mock := &MockPsyNetServer{
		matches:      make([]MockMatchEntry, 0),
		authRequests: make([]MockAuthPlayerRequest, 0),
		activeConns:  make([]net.Conn, 0),
	}

	mock.server = httptest.NewServer(http.HandlerFunc(mock.handleHTTP))
	return mock
}

// URL returns the HTTP base URL of the mock server.
func (m *MockPsyNetServer) URL() string {
	return m.server.URL
}

// WebSocketURL returns the WebSocket URL corresponding to the /ws endpoint.
func (m *MockPsyNetServer) WebSocketURL() string {
	wsBase := "ws" + strings.TrimPrefix(m.server.URL, "http")
	return wsBase + "/ws"
}

// Close closes all active sockets and stops the HTTP server.
func (m *MockPsyNetServer) Close() {
	m.mu.Lock()
	for _, conn := range m.activeConns {
		_ = conn.Close()
	}
	m.activeConns = nil
	m.mu.Unlock()
	m.server.Close()
}

// SetMatches dynamically replaces the current match history list.
func (m *MockPsyNetServer) SetMatches(matches []MockMatchEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matches = make([]MockMatchEntry, len(matches))
	copy(m.matches, matches)
}

// AddMatch appends a single match to the dynamic match list.
func (m *MockPsyNetServer) AddMatch(match MockMatchEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matches = append(m.matches, match)
}

// ClearMatches empties the match list.
func (m *MockPsyNetServer) ClearMatches() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matches = make([]MockMatchEntry, 0)
}

// GetMatches returns a copy of current configured matches.
func (m *MockPsyNetServer) GetMatches() []MockMatchEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]MockMatchEntry, len(m.matches))
	copy(res, m.matches)
	return res
}

// GetAuthRequests returns all received HTTP AuthPlayer requests.
func (m *MockPsyNetServer) GetAuthRequests() []MockAuthPlayerRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]MockAuthPlayerRequest, len(m.authRequests))
	copy(res, m.authRequests)
	return res
}

// GetHistoryRequestCount returns the number of times GetMatchHistory RPC was invoked.
func (m *MockPsyNetServer) GetHistoryRequestCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.historyRequestCount
}

// GetPingCount returns the number of PsyPing frames received.
func (m *MockPsyNetServer) GetPingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pingCount
}

// Reset clears recorded metrics and match lists.
func (m *MockPsyNetServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matches = make([]MockMatchEntry, 0)
	m.authRequests = make([]MockAuthPlayerRequest, 0)
	m.historyRequestCount = 0
	m.pingCount = 0
}

func (m *MockPsyNetServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. WebSocket upgrade endpoint: /ws or Upgrade: websocket header
	if r.URL.Path == "/ws" || strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		m.handleWebSocketUpgrade(w, r)
		return
	}

	// 2. HTTP Player Auth endpoint: /rpc/Auth/AuthPlayer/v2 or suffix AuthPlayer/v2
	if strings.HasSuffix(r.URL.Path, "AuthPlayer/v2") {
		m.handleAuthPlayer(w, r)
		return
	}

	http.NotFound(w, r)
}

func (m *MockPsyNetServer) handleAuthPlayer(w http.ResponseWriter, r *http.Request) {
	var authReq MockAuthPlayerRequest
	bodyBytes, err := io.ReadAll(r.Body)
	if err == nil && len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &authReq)
	}

	m.mu.Lock()
	m.authRequests = append(m.authRequests, authReq)
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	resp := map[string]any{
		"SessionID":           "mock-session-id-" + authReq.PlayerID,
		"VerifiedPlayerName":  authReq.PlayerName,
		"UseWebSocket":        true,
		"PerConURL":           m.WebSocketURL(),
		"PerConURLv2":         m.WebSocketURL(),
		"PsyToken":            "mock-psy-token-" + authReq.PlayerID,
		"CountryRestrictions": []string{},
	}

	// rlapi.postJSON expects {"Result": {...}}, while direct mock tests may read top-level keys.
	// Providing both guarantees dual compatibility.
	wrappedResp := map[string]any{
		"Result":              resp,
		"SessionID":           resp["SessionID"],
		"VerifiedPlayerName":  resp["VerifiedPlayerName"],
		"UseWebSocket":        resp["UseWebSocket"],
		"PerConURL":           resp["PerConURL"],
		"PerConURLv2":         resp["PerConURLv2"],
		"PsyToken":            resp["PsyToken"],
		"CountryRestrictions": resp["CountryRestrictions"],
	}
	_ = json.NewEncoder(w).Encode(wrappedResp)
}

// RFC 6455 WebSocket Handshake and Frame Processor (zero external dependencies)
const websocketMagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func (m *MockPsyNetServer) handleWebSocketUpgrade(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return
	}

	// Compute Sec-WebSocket-Accept
	h := sha1.New()
	h.Write([]byte(key + websocketMagicGUID))
	acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "server does not support hijacking", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return
	}

	// Send 101 Switching Protocols
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"
	if _, err := bufrw.WriteString(response); err != nil {
		conn.Close()
		return
	}
	if err := bufrw.Flush(); err != nil {
		conn.Close()
		return
	}

	m.mu.Lock()
	m.activeConns = append(m.activeConns, conn)
	m.mu.Unlock()

	go m.serveWebSocketConn(conn, bufrw.Reader)
}

func (m *MockPsyNetServer) serveWebSocketConn(conn net.Conn, reader *bufio.Reader) {
	defer conn.Close()

	for {
		msg, err := readRFC6455TextMessage(reader)
		if err != nil {
			return
		}

		// Handle PsyPing heartbeat
		if strings.Contains(msg, "PsyPing:") {
			m.mu.Lock()
			m.pingCount++
			m.mu.Unlock()

			pongFrame := "PsyPong: \r\n\r\n"
			if err := writeRFC6455TextMessage(conn, pongFrame); err != nil {
				return
			}
			continue
		}

		// Handle RPC Requests
		if strings.Contains(msg, "PsyRequestID:") {
			reqID := ""
			lines := strings.Split(msg, "\r\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "PsyRequestID:") {
					reqID = strings.TrimSpace(strings.TrimPrefix(line, "PsyRequestID:"))
					break
				}
			}

			if strings.Contains(msg, "Matches/GetMatchHistory") {
				m.mu.Lock()
				m.historyRequestCount++
				currentMatches := make([]MockMatchEntry, len(m.matches))
				copy(currentMatches, m.matches)
				m.mu.Unlock()

				resultJSON, _ := json.Marshal(map[string]any{
					"Result": map[string]any{
						"Matches": currentMatches,
					},
				})

				responseMsg := fmt.Sprintf("PsyTime: %d\r\nPsySig: mock_sig\r\nPsyResponseID: %s\r\n\r\n%s",
					time.Now().Unix(), reqID, string(resultJSON))

				if err := writeRFC6455TextMessage(conn, responseMsg); err != nil {
					return
				}
			}
		}
	}
}

func readRFC6455TextMessage(r *bufio.Reader) (string, error) {
	// Read first 2 bytes: FIN/Opcode and Mask/Length
	b0, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	b1, err := r.ReadByte()
	if err != nil {
		return "", err
	}

	opcode := b0 & 0x0f
	if opcode == 0x08 { // Close frame
		return "", io.EOF
	}

	isMasked := (b1 & 0x80) != 0
	var payloadLen uint64 = uint64(b1 & 0x7f)

	if payloadLen == 126 {
		var extendedLen uint16
		if err := binary.Read(r, binary.BigEndian, &extendedLen); err != nil {
			return "", err
		}
		payloadLen = uint64(extendedLen)
	} else if payloadLen == 127 {
		if err := binary.Read(r, binary.BigEndian, &payloadLen); err != nil {
			return "", err
		}
	}

	var maskKey [4]byte
	if isMasked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return "", err
		}
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return "", err
	}

	if isMasked {
		for i := uint64(0); i < payloadLen; i++ {
			payload[i] ^= maskKey[i%4]
		}
	}

	return string(payload), nil
}

func writeRFC6455TextMessage(w io.Writer, msg string) error {
	payload := []byte(msg)
	payloadLen := len(payload)

	var header []byte
	header = append(header, 0x81) // FIN + Text opcode

	if payloadLen <= 125 {
		header = append(header, byte(payloadLen))
	} else if payloadLen <= 65535 {
		header = append(header, 126)
		lenBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBytes, uint16(payloadLen))
		header = append(header, lenBytes...)
	} else {
		header = append(header, 127)
		lenBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(lenBytes, uint64(payloadLen))
		header = append(header, lenBytes...)
	}

	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// In-Memory MockMatchHistoryProvider for direct syncer domain integration
type InMemoryMatchHistoryProvider struct {
	mu           sync.Mutex
	matches      []MockMatchEntry
	historyCalls int
	closed       bool
}

// NewInMemoryMatchHistoryProvider creates a provider that returns injected matches.
func NewInMemoryMatchHistoryProvider(initialMatches ...MockMatchEntry) *InMemoryMatchHistoryProvider {
	return &InMemoryMatchHistoryProvider{
		matches: initialMatches,
	}
}

// SetMatches updates the matches returned by the provider.
func (p *InMemoryMatchHistoryProvider) SetMatches(matches []MockMatchEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.matches = make([]MockMatchEntry, len(matches))
	copy(p.matches, matches)
}

// AddMatch adds a match to the provider.
func (p *InMemoryMatchHistoryProvider) AddMatch(match MockMatchEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.matches = append(p.matches, match)
}

// GetHistoryCalls returns the count of GetRecentMatches invocations.
func (p *InMemoryMatchHistoryProvider) GetHistoryCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.historyCalls
}

// GetRecentMatches returns the current matches mapped to DiscoveredMatch interface types.
func (p *InMemoryMatchHistoryProvider) GetRecentMatches(ctx context.Context) ([]MockMatchEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.historyCalls++
	res := make([]MockMatchEntry, len(p.matches))
	copy(res, p.matches)
	return res, nil
}

// Close marks the provider closed.
func (p *InMemoryMatchHistoryProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}
