package websocket

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
)

// Mock connection for testing
type mockConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
	mu       sync.Mutex
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuf:  &bytes.Buffer{},
		writeBuf: &bytes.Buffer{},
	}
}

func (m *mockConn) Read(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readBuf.Read(b)
}

func (m *mockConn) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.Write(b)
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// Mock response writer for upgrade testing
type mockResponseWriter struct {
	headers    map[string][]string
	statusCode int
	body       *bytes.Buffer
	Conn       net.Conn
	flushed    bool
}

func newMockResponseWriter(conn net.Conn) *mockResponseWriter {
	return &mockResponseWriter{
		headers: make(map[string][]string),
		body:    &bytes.Buffer{},
		Conn:    conn,
	}
}

func (m *mockResponseWriter) Header() map[string][]string {
	return m.headers
}

func (m *mockResponseWriter) Write(p []byte) (int, error) {
	return m.body.Write(p)
}

func (m *mockResponseWriter) WriteHeader(code int) {
	m.statusCode = code
}

func (m *mockResponseWriter) WriteString(s string) (int, error) {
	return m.body.WriteString(s)
}

func (m *mockResponseWriter) Status() int {
	return m.statusCode
}

func (m *mockResponseWriter) Written() int64 {
	return int64(m.body.Len())
}

func (m *mockResponseWriter) Flush() error {
	m.flushed = true
	return nil
}

func (m *mockResponseWriter) UnderlyingConn() net.Conn {
	return m.Conn
}

func maskedFrame(opcode byte, fin bool, payload []byte) []byte {
	firstByte := opcode
	if fin {
		firstByte |= 0x80
	}

	maskKey := [4]byte{0x12, 0x34, 0x56, 0x78}
	frame := []byte{firstByte}

	switch payloadLen := len(payload); {
	case payloadLen < 126:
		frame = append(frame, byte(payloadLen)|0x80)
	case payloadLen <= 0xFFFF:
		frame = append(frame, 126|0x80)
		var lengthBytes [2]byte
		binary.BigEndian.PutUint16(lengthBytes[:], uint16(payloadLen))
		frame = append(frame, lengthBytes[:]...)
	default:
		frame = append(frame, 127|0x80)
		var lengthBytes [8]byte
		binary.BigEndian.PutUint64(lengthBytes[:], uint64(payloadLen))
		frame = append(frame, lengthBytes[:]...)
	}

	frame = append(frame, maskKey[:]...)
	for i, b := range payload {
		frame = append(frame, b^maskKey[i%4])
	}

	return frame
}

// UPGRADE HANDSHAKE TESTS

func TestUpgrade_SuccessfulHandshake(t *testing.T) {
	conn := newMockConn()
	w := newMockResponseWriter(conn)

	req := &request.Request{
		Method: "GET",
		Host:   "example.com",
		Headers: map[string][]string{
			"Upgrade":               {"websocket"},
			"Connection":            {"Upgrade"},
			"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="}, // Normalized form
			"Sec-Websocket-Version": {"13"},                       // Normalized form
		},
	}

	ws, err := Upgrade(w, req)
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}

	if ws == nil {
		t.Fatal("WebSocket is nil")
	}

	// Check response headers
	if w.headers["Upgrade"][0] != "websocket" {
		t.Error("Missing or incorrect Upgrade header")
	}

	if w.headers["Connection"][0] != "Upgrade" {
		t.Error("Missing or incorrect Connection header")
	}

	// Verify Sec-WebSocket-Accept calculation
	expectedAccept := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if w.headers["Sec-WebSocket-Accept"][0] != expectedAccept {
		t.Errorf("Expected Sec-WebSocket-Accept %q, got %q",
			expectedAccept, w.headers["Sec-WebSocket-Accept"][0])
	}

	// Check status code
	if w.statusCode != response.StatusSwitchingProtocols {
		t.Errorf("Expected status 101, got %d", w.statusCode)
	}

	// Check flushed
	if !w.flushed {
		t.Error("Response should be flushed after upgrade")
	}
}

func TestUpgrade_MissingUpgradeHeader(t *testing.T) {
	conn := newMockConn()
	w := newMockResponseWriter(conn)

	req := &request.Request{
		Method: "GET",
		Headers: map[string][]string{
			"Connection":            {"Upgrade"},
			"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
			"Sec-Websocket-Version": {"13"},
		},
	}

	_, err := Upgrade(w, req)
	if err == nil {
		t.Error("Should fail with missing Upgrade header")
	}

	if !strings.Contains(err.Error(), "Upgrade") {
		t.Errorf("Error should mention Upgrade header, got: %v", err)
	}
}

func TestUpgrade_MissingWebSocketKey(t *testing.T) {
	conn := newMockConn()
	w := newMockResponseWriter(conn)

	req := &request.Request{
		Method: "GET",
		Headers: map[string][]string{
			"Upgrade":               {"websocket"},
			"Connection":            {"Upgrade"},
			"Sec-Websocket-Version": {"13"},
		},
	}

	_, err := Upgrade(w, req)
	if err == nil {
		t.Error("Should fail with missing Sec-WebSocket-Key")
	}
}

func TestUpgrade_UnsupportedVersion(t *testing.T) {
	conn := newMockConn()
	w := newMockResponseWriter(conn)

	req := &request.Request{
		Method: "GET",
		Headers: map[string][]string{
			"Upgrade":               {"websocket"},
			"Connection":            {"Upgrade"},
			"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
			"Sec-Websocket-Version": {"12"}, // Unsupported
		},
	}

	_, err := Upgrade(w, req)
	if err == nil {
		t.Error("Should fail with unsupported WebSocket version")
	}
}

func TestUpgrade_RejectsCrossOrigin(t *testing.T) {
	conn := newMockConn()
	w := newMockResponseWriter(conn)

	req := &request.Request{
		Method: "GET",
		Host:   "example.com",
		Headers: map[string][]string{
			"Upgrade":               {"websocket"},
			"Connection":            {"Upgrade"},
			"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
			"Sec-Websocket-Version": {"13"},
			"Origin":                {"https://attacker.example"},
		},
	}

	if _, err := Upgrade(w, req); err == nil {
		t.Fatal("Expected cross-origin upgrade to fail")
	}
}

func TestUpgrade_RejectsInvalidKey(t *testing.T) {
	conn := newMockConn()
	w := newMockResponseWriter(conn)

	req := &request.Request{
		Method: "GET",
		Host:   "example.com",
		Headers: map[string][]string{
			"Upgrade":               {"websocket"},
			"Connection":            {"Upgrade"},
			"Sec-Websocket-Key":     {"invalid"},
			"Sec-Websocket-Version": {"13"},
		},
	}

	if _, err := Upgrade(w, req); err == nil {
		t.Fatal("Expected invalid websocket key to fail")
	}
}

// ACCEPT KEY COMPUTATION TEST

func TestComputeAcceptKey(t *testing.T) {
	testCases := []struct {
		key    string
		accept string
	}{
		{"dGhlIHNhbXBsZSBub25jZQ==", "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
		{"x3JJHMbDL1EzLkh9GBhXDw==", "HSmrc0sMlYUkAGmm5OPpG2HaGWk="},
	}

	for _, tc := range testCases {
		got := computeAcceptKey(tc.key)
		if got != tc.accept {
			t.Errorf("computeAcceptKey(%q) = %q, want %q", tc.key, got, tc.accept)
		}
	}
}

// FRAME WRITING TESTS

func TestWriteFrame_TextFrame(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	err := ws.WriteFrame(&Frame{
		Fin:     true,
		Opcode:  OpcodeText,
		Payload: []byte("Hello"),
	})

	if err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	// Check written bytes
	written := conn.writeBuf.Bytes()

	// First byte: FIN=1, Opcode=1
	if written[0] != 0x81 {
		t.Errorf("Expected first byte 0x81, got 0x%02x", written[0])
	}

	// Second byte: MASK=0, Length=5
	if written[1] != 0x05 {
		t.Errorf("Expected second byte 0x05, got 0x%02x", written[1])
	}

	// Payload
	payload := string(written[2:])
	if payload != "Hello" {
		t.Errorf("Expected payload 'Hello', got %q", payload)
	}
}

func TestWriteFrame_BinaryFrame(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	data := []byte{0x01, 0x02, 0x03, 0x04}
	err := ws.WriteFrame(&Frame{
		Fin:     true,
		Opcode:  OpcodeBinary,
		Payload: data,
	})

	if err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	written := conn.writeBuf.Bytes()

	// First byte: FIN=1, Opcode=2
	if written[0] != 0x82 {
		t.Errorf("Expected first byte 0x82, got 0x%02x", written[0])
	}

	// Payload
	if !bytes.Equal(written[2:], data) {
		t.Errorf("Payload mismatch")
	}
}

func TestWriteFrame_ExtendedLength126(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	// 200 byte payload
	payload := make([]byte, 200)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	err := ws.WriteFrame(&Frame{
		Fin:     true,
		Opcode:  OpcodeBinary,
		Payload: payload,
	})

	if err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	written := conn.writeBuf.Bytes()

	// Second byte should be 126 (extended length marker)
	if written[1] != 126 {
		t.Errorf("Expected length marker 126, got %d", written[1])
	}

	// Next 2 bytes encode length
	length := binary.BigEndian.Uint16(written[2:4])
	if length != 200 {
		t.Errorf("Expected length 200, got %d", length)
	}
}

func TestWriteFrame_ExtendedLength127(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	// 70000 byte payload
	payload := make([]byte, 70000)

	err := ws.WriteFrame(&Frame{
		Fin:     true,
		Opcode:  OpcodeBinary,
		Payload: payload,
	})

	if err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	written := conn.writeBuf.Bytes()

	// Second byte should be 127 (64-bit length marker)
	if written[1] != 127 {
		t.Errorf("Expected length marker 127, got %d", written[1])
	}

	// Next 8 bytes encode length
	length := binary.BigEndian.Uint64(written[2:10])
	if length != 70000 {
		t.Errorf("Expected length 70000, got %d", length)
	}
}

// FRAME READING TESTS

func TestReadFrame_TextFrame(t *testing.T) {
	conn := newMockConn()
	conn.readBuf.Write(maskedFrame(OpcodeText, true, []byte("Test")))

	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	frame, err := ws.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}

	if !frame.Fin {
		t.Error("Expected FIN=true")
	}

	if frame.Opcode != OpcodeText {
		t.Errorf("Expected opcode TEXT (1), got %d", frame.Opcode)
	}

	if string(frame.Payload) != "Test" {
		t.Errorf("Expected payload 'Test', got %q", string(frame.Payload))
	}
}

func TestReadFrame_UnmaskedFrameRejected(t *testing.T) {
	conn := newMockConn()

	// Unmasked text frame
	frameData := []byte{
		0x81, // FIN=1, Opcode=1
		0x05, // MASK=0, Length=5
		'H', 'e', 'l', 'l', 'o',
	}
	conn.readBuf.Write(frameData)

	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	if _, err := ws.ReadFrame(); err == nil {
		t.Fatal("Expected unmasked client frame to be rejected")
	}
}

func TestReadFrame_ExtendedLength126(t *testing.T) {
	conn := newMockConn()

	// Create 200-byte payload
	payload := make([]byte, 200)
	for i := range payload {
		payload[i] = byte('A' + (i % 26))
	}

	conn.readBuf.Write(maskedFrame(OpcodeBinary, true, payload))

	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	frame, err := ws.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}

	if len(frame.Payload) != 200 {
		t.Errorf("Expected 200 bytes, got %d", len(frame.Payload))
	}
}

// MESSAGE TESTS

func TestWriteMessage_Text(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	err := ws.WriteText("Hello, WebSocket!")
	if err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}

	written := conn.writeBuf.Bytes()

	// Should be a complete text frame
	if written[0] != 0x81 { // FIN=1, Opcode=1
		t.Errorf("Expected text frame, got 0x%02x", written[0])
	}
}

func TestWriteMessage_Binary(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	err := ws.WriteBinary(data)
	if err != nil {
		t.Fatalf("WriteBinary failed: %v", err)
	}

	written := conn.writeBuf.Bytes()

	// Should be a binary frame
	if written[0] != 0x82 { // FIN=1, Opcode=2
		t.Errorf("Expected binary frame, got 0x%02x", written[0])
	}
}

// PING/PONG TESTS

func TestPing(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	err := ws.Ping([]byte("ping"))
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	written := conn.writeBuf.Bytes()

	// Check opcode is Ping (9)
	if written[0] != 0x89 { // FIN=1, Opcode=9
		t.Errorf("Expected ping frame 0x89, got 0x%02x", written[0])
	}

	// Check payload
	if string(written[2:6]) != "ping" {
		t.Error("Ping payload incorrect")
	}
}

// CLOSE TESTS

func TestClose(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	err := ws.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !ws.IsClosed() {
		t.Error("WebSocket should be marked as closed")
	}

	conn.mu.Lock()
	written := conn.writeBuf.Bytes()
	conn.mu.Unlock()

	if len(written) == 0 {
		t.Fatal("No data written to connection")
	}

	// Should send close frame (opcode 8)
	if written[0] != 0x88 { // FIN=1, Opcode=8
		t.Errorf("Expected close frame 0x88, got 0x%02x", written[0])
	}
}

func TestCloseWithCode(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	err := ws.CloseWithCode(CloseGoingAway, "Going away")
	if err != nil {
		t.Fatalf("CloseWithCode failed: %v", err)
	}

	written := conn.writeBuf.Bytes()

	// Close opcode
	if written[0] != 0x88 {
		t.Error("Expected close frame")
	}

	// Extract close code from payload
	payload := written[2:] // Skip opcode and length
	code := binary.BigEndian.Uint16(payload[:2])

	if code != CloseGoingAway {
		t.Errorf("Expected close code %d, got %d", CloseGoingAway, code)
	}

	reason := string(payload[2:])
	if reason != "Going away" {
		t.Errorf("Expected reason 'Going away', got %q", reason)
	}
}

func TestClose_MultipleCalls(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	ws.Close()
	err := ws.Close() // Second close

	// Should not error
	if err != nil {
		t.Errorf("Second close should not error, got: %v", err)
	}
}

func TestWriteAfterClose(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	ws.Close()

	err := ws.WriteText("Hello")
	if err == nil {
		t.Error("Writing after close should return error")
	}
}

func TestReadAfterClose(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	ws.Close()

	_, err := ws.ReadFrame()
	if err == nil {
		t.Error("Reading after close should return error")
	}
}

// CONCURRENCY TESTS

func TestConcurrentWrites(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// 100 concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := ws.WriteText("Message"); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent write error: %v", err)
	}
}

// EDGE CASES

func TestEmptyPayload(t *testing.T) {
	conn := newMockConn()
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	err := ws.WriteFrame(&Frame{
		Fin:     true,
		Opcode:  OpcodeText,
		Payload: []byte{},
	})

	if err != nil {
		t.Fatalf("Empty payload should be allowed: %v", err)
	}
}

func TestFragmentedMessage(t *testing.T) {
	conn := newMockConn()

	// First fragment: "Hel"
	frame1 := maskedFrame(OpcodeText, false, []byte("Hel"))

	// Continuation: "lo"
	frame2 := maskedFrame(OpcodeContinuation, true, []byte("lo"))

	conn.readBuf.Write(frame1)
	conn.readBuf.Write(frame2)

	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if string(msg) != "Hello" {
		t.Errorf("Expected 'Hello' from fragmented message, got %q", string(msg))
	}
}

func TestReadFrame_RejectsOversizedPayload(t *testing.T) {
	conn := newMockConn()
	frameData := []byte{0x82, 127 | 0x80}
	var lengthBytes [8]byte
	binary.BigEndian.PutUint64(lengthBytes[:], maxFramePayloadSize+1)
	frameData = append(frameData, lengthBytes[:]...)
	frameData = append(frameData, 0x12, 0x34, 0x56, 0x78)
	conn.readBuf.Write(frameData)

	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	if _, err := ws.ReadFrame(); err == nil {
		t.Fatal("Expected oversized payload to be rejected")
	}
}
