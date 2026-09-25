package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMockCDNServer(t *testing.T) {
	cdn := NewMockCDNServer()
	defer cdn.Close()

	guid := "test-guid-12345"

	// 1. Download valid replay
	resp, err := http.Get(cdn.ReplayURL(guid))
	if err != nil {
		t.Fatalf("failed to GET from CDN: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if len(body) < 1024 {
		t.Fatalf("expected body >= 1024 bytes, got %d", len(body))
	}
	if !bytes.HasPrefix(body, ReplayMagicBytes) {
		t.Fatalf("expected body to start with TAGAME header, got %q", body[:10])
	}
	if cdn.GetDownloadCount(guid) != 1 {
		t.Fatalf("expected download count 1, got %d", cdn.GetDownloadCount(guid))
	}

	// 2. Custom status code
	cdn.SetStatusCode(guid, http.StatusForbidden)
	resp2, err := http.Get(cdn.ReplayURL(guid))
	if err != nil {
		t.Fatalf("failed to GET: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp2.StatusCode)
	}

	// 3. Reset counters
	cdn.ResetCounters()
	if cdn.GetTotalDownloads() != 0 {
		t.Fatalf("expected 0 total downloads after reset, got %d", cdn.GetTotalDownloads())
	}
}

func TestMockBallchasingServer(t *testing.T) {
	bc := NewMockBallchasingServer()
	defer bc.Close()

	expectedKey := "test-token-xyz"
	bc.SetExpectedToken(expectedKey)

	// 1. Ping with missing token -> 401
	resp, err := http.Get(bc.URL() + "/")
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// 2. Ping with "Bearer " prefix -> 401
	req, _ := http.NewRequest(http.MethodGet, bc.URL()+"/", nil)
	req.Header.Set("Authorization", "Bearer "+expectedKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for Bearer prefix, got %d", resp.StatusCode)
	}

	// 3. Ping with valid raw token -> 200
	req, _ = http.NewRequest(http.MethodGet, bc.URL()+"/", nil)
	req.Header.Set("Authorization", expectedKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Helper for multipart upload
	uploadReplay := func(guid, visibility string, fileData []byte) (int, map[string]any, error) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", guid+".replay")
		if err != nil {
			return 0, nil, err
		}
		if _, err := part.Write(fileData); err != nil {
			return 0, nil, err
		}
		if err := writer.Close(); err != nil {
			return 0, nil, err
		}

		url := fmt.Sprintf("%s/v2/upload?visibility=%s", bc.URL(), visibility)
		postReq, err := http.NewRequest(http.MethodPost, url, body)
		if err != nil {
			return 0, nil, err
		}
		postReq.Header.Set("Authorization", expectedKey)
		postReq.Header.Set("Content-Type", writer.FormDataContentType())

		postResp, err := http.DefaultClient.Do(postReq)
		if err != nil {
			return 0, nil, err
		}
		defer postResp.Body.Close()

		var result map[string]any
		jsonBytes, _ := io.ReadAll(postResp.Body)
		if len(jsonBytes) > 0 {
			_ = json.Unmarshal(jsonBytes, &result)
		}
		return postResp.StatusCode, result, nil
	}

	// 4. Successful upload -> 201 Created
	dummyData := GenerateValidReplay("guid-1", 2048)
	status, res, err := uploadReplay("guid-1", "public", dummyData)
	if err != nil {
		t.Fatalf("upload error: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", status)
	}
	if res["id"] == nil || res["id"] == "" {
		t.Fatalf("expected replay id in response, got %v", res)
	}
	if bc.GetUploadCount() != 1 {
		t.Fatalf("expected 1 upload, got %d", bc.GetUploadCount())
	}

	// 5. Duplicate replay -> 409 Conflict
	bc.SetDuplicateGUID("guid-2", "existing-ballchasing-id-2")
	status, res, err = uploadReplay("guid-2", "unlisted", dummyData)
	if err != nil {
		t.Fatalf("upload error: %v", err)
	}
	if status != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d", status)
	}
	if res["id"] != "existing-ballchasing-id-2" {
		t.Fatalf("expected existing-ballchasing-id-2, got %v", res["id"])
	}

	// 6. Rate Limit (429) simulation -> 1 failure then success
	bc.SimulateRateLimit(1, 2)
	status, _, err = uploadReplay("guid-3", "private", dummyData)
	if err != nil {
		t.Fatalf("upload error: %v", err)
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", status)
	}
	// Subsequent upload succeeds
	status, _, err = uploadReplay("guid-3", "private", dummyData)
	if err != nil {
		t.Fatalf("upload error: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("expected 201 Created on retry, got %d", status)
	}
}

func TestMockPsyNetServer(t *testing.T) {
	psy := NewMockPsyNetServer()
	defer psy.Close()

	// 1. Test HTTP AuthPlayer/v2
	authBody := map[string]string{
		"Platform":       "Epic",
		"PlayerName":     "RocketTester",
		"PlayerID":       "epic-account-123",
		"AuthTicket":     "auth-ticket-abc",
		"EpicAuthTicket": "eos-token-xyz",
		"EpicAccountID":  "epic-account-123",
	}
	jsonBody, _ := json.Marshal(authBody)
	resp, err := http.Post(psy.URL()+"/rpc/Auth/AuthPlayer/v2", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("auth request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	var authResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		t.Fatalf("failed to decode auth response: %v", err)
	}
	if authResp["SessionID"] == nil || authResp["UseWebSocket"] != true {
		t.Fatalf("unexpected auth response: %v", authResp)
	}
	if len(psy.GetAuthRequests()) != 1 {
		t.Fatalf("expected 1 recorded auth request, got %d", len(psy.GetAuthRequests()))
	}

	// 2. Test Dynamic Match Management
	match1 := NewMockMatchEntry("match-1", "http://example.com/1.replay", "Wasteland_P", 2)
	match2 := NewMockMatchEntry("match-2", "http://example.com/2.replay", "Stadium_P", 3)
	psy.SetMatches([]MockMatchEntry{match1, match2})
	if len(psy.GetMatches()) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(psy.GetMatches()))
	}

	// 3. Test WebSocket Upgrade & RPC framing over raw TCP
	// Parse host:port from psy.server.URL
	serverHostPort := strings.TrimPrefix(psy.URL(), "http://")
	conn, err := net.Dial("tcp", serverHostPort)
	if err != nil {
		t.Fatalf("failed to dial raw TCP: %v", err)
	}
	defer conn.Close()

	// Perform WebSocket handshake
	handshake := fmt.Sprintf("GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", serverHostPort)
	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read handshake response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read handshake response: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "101 Switching Protocols") {
		t.Fatalf("expected 101 Switching Protocols, got: %s", string(buf[:n]))
	}

	// Send PsyPing text frame (unmasked for simplicity in test or masked as RFC 6455 client)
	// We can use writeRFC6455TextMessage helper (or masked)
	pingMsg := "PsyPing: \r\n\r\n"
	// Client frames MUST be masked according to RFC 6455
	maskedPing := makeMaskedFrame(pingMsg)
	if _, err := conn.Write(maskedPing); err != nil {
		t.Fatalf("failed to send PsyPing: %v", err)
	}

	// Read PsyPong
	pongN, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read pong: %v", err)
	}
	// Server responds with unmasked frame starting with 0x81
	if pongN > 2 && strings.Contains(string(buf[:pongN]), "PsyPong:") {
		// Pong successfully received!
	} else {
		t.Logf("Received response: %q", string(buf[:pongN]))
	}

	// Send Matches/GetMatchHistory v1 RPC request
	reqID := "PsyNetMessage_0_1"
	rpcMsg := fmt.Sprintf("PsyService: Matches/GetMatchHistory v1\r\nPsyRequestID: %s\r\n\r\n{\"PlayerID\":\"Epic|test|0\"}", reqID)
	maskedRPC := makeMaskedFrame(rpcMsg)
	if _, err := conn.Write(maskedRPC); err != nil {
		t.Fatalf("failed to send RPC request: %v", err)
	}

	// Read RPC response
	time.Sleep(50 * time.Millisecond)
	rpcN, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read RPC response: %v", err)
	}
	responseStr := string(buf[:rpcN])
	if !strings.Contains(responseStr, reqID) {
		t.Fatalf("expected response to contain %s, got: %s", reqID, responseStr)
	}
	if !strings.Contains(responseStr, "match-1") || !strings.Contains(responseStr, "match-2") {
		t.Fatalf("expected response to contain matches, got: %s", responseStr)
	}

	// 4. Test In-Memory Provider
	inMem := NewInMemoryMatchHistoryProvider(match1, match2)
	defer inMem.Close()
	matches, err := inMem.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("inMem error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

// makeMaskedFrame creates an RFC 6455 client frame with 4-byte mask
func makeMaskedFrame(msg string) []byte {
	payload := []byte(msg)
	mask := [4]byte{0x12, 0x34, 0x56, 0x78}

	var frame []byte
	frame = append(frame, 0x81) // FIN + text

	length := len(payload)
	if length <= 125 {
		frame = append(frame, byte(0x80|length))
	} else if length <= 65535 {
		frame = append(frame, byte(0x80|126))
		frame = append(frame, byte(length>>8), byte(length&0xff))
	}

	frame = append(frame, mask[:]...)

	maskedPayload := make([]byte, length)
	for i := 0; i < length; i++ {
		maskedPayload[i] = payload[i] ^ mask[i%4]
	}
	frame = append(frame, maskedPayload...)
	return frame
}
