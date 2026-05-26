package websocket

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	neturl "net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
)

// WebSocket GUID as per RFC 6455
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	maxControlPayloadSize = 125
	maxFramePayloadSize   = 1 << 20
	maxMessagePayloadSize = 4 << 20
)

// Opcode constants
const (
	OpcodeContinuation = 0x0
	OpcodeText         = 0x1
	OpcodeBinary       = 0x2
	OpcodeClose        = 0x8
	OpcodePing         = 0x9
	OpcodePong         = 0xA
)

// Close codes
const (
	CloseNormalClosure           = 1000
	CloseGoingAway               = 1001
	CloseProtocolError           = 1002
	CloseUnsupportedData         = 1003
	CloseInvalidFramePayloadData = 1007
	ClosePolicyViolation         = 1008
	CloseMessageTooBig           = 1009
	CloseMandatoryExtension      = 1010
	CloseInternalServerErr       = 1011
)

// Frame represents a WebSocket frame
type Frame struct {
	Fin     bool
	Opcode  byte
	Masked  bool
	Payload []byte
}

// WebSocket represents a WebSocket connection
type WebSocket struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer

	readDeadline  time.Time
	writeDeadline time.Time

	closed atomic.Bool
	mu     sync.Mutex
	wmu    sync.Mutex // Separate mutex for writing
}

// Upgrade upgrades an HTTP connection to WebSocket
func Upgrade(w response.ResponseWriter, r *request.Request) (*WebSocket, error) {
	if r.Method != "GET" {
		return nil, errors.New("websocket upgrade requires GET")
	}

	// Validate upgrade headers
	if !strings.EqualFold(r.GetHeader("Upgrade"), "websocket") {
		return nil, errors.New("missing Upgrade: websocket header")
	}

	if !headerContainsToken(r.GetHeader("Connection"), "upgrade") {
		return nil, errors.New("missing Connection: Upgrade header")
	}

	key := r.GetHeader("Sec-WebSocket-Key")
	if !isValidWebSocketKey(key) {
		return nil, errors.New("invalid Sec-WebSocket-Key header")
	}

	version := r.GetHeader("Sec-WebSocket-Version")
	if version != "13" {
		return nil, errors.New("unsupported WebSocket version")
	}
	if !isAllowedWebSocketOrigin(r) {
		return nil, errors.New("websocket origin not allowed")
	}

	// Compute accept key
	acceptKey := computeAcceptKey(key)

	// Send 101 Switching Protocols response
	w.Header()["Upgrade"] = []string{"websocket"}
	w.Header()["Connection"] = []string{"Upgrade"}
	w.Header()["Sec-WebSocket-Accept"] = []string{acceptKey}
	w.WriteHeader(response.StatusSwitchingProtocols)
	w.Flush()

	// Get underlying connection
	// We need to access the underlying net.Conn
	writer, ok := response.Unwrap(w).(response.ConnProvider)
	if !ok {
		return nil, errors.New("cannot get underlying connection")
	}

	conn := writer.UnderlyingConn()

	// Create WebSocket
	ws := &WebSocket{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	return ws, nil
}

// computeAcceptKey computes the Sec-WebSocket-Accept key
func computeAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ReadFrame reads a WebSocket frame
func (ws *WebSocket) ReadFrame() (*Frame, error) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.closed.Load() {
		return nil, errors.New("websocket is closed")
	}

	// Set read deadline
	if !ws.readDeadline.IsZero() {
		ws.conn.SetReadDeadline(ws.readDeadline)
	}

	// Read first byte (FIN, RSV, Opcode)
	b, err := ws.reader.ReadByte()
	if err != nil {
		return nil, err
	}

	fin := (b & 0x80) != 0
	if b&0x70 != 0 {
		return nil, errors.New("websocket extensions are not supported")
	}
	opcode := b & 0x0F
	if !isSupportedOpcode(opcode) {
		return nil, fmt.Errorf("unsupported opcode: %d", opcode)
	}

	// Read second byte (Mask, Payload Length)
	b, err = ws.reader.ReadByte()
	if err != nil {
		return nil, err
	}

	masked := (b & 0x80) != 0
	if !masked {
		return nil, errors.New("client frames must be masked")
	}
	payloadLen := uint64(b & 0x7F)

	// Extended payload length
	if payloadLen == 126 {
		var buf [2]byte
		if _, err := io.ReadFull(ws.reader, buf[:]); err != nil {
			return nil, err
		}
		payloadLen = uint64(binary.BigEndian.Uint16(buf[:]))
	} else if payloadLen == 127 {
		var buf [8]byte
		if _, err := io.ReadFull(ws.reader, buf[:]); err != nil {
			return nil, err
		}
		payloadLen = binary.BigEndian.Uint64(buf[:])
	}
	if payloadLen > maxFramePayloadSize {
		return nil, fmt.Errorf("frame payload exceeds limit: %d", payloadLen)
	}
	if isControlOpcode(opcode) {
		if !fin {
			return nil, errors.New("control frames must not be fragmented")
		}
		if payloadLen > maxControlPayloadSize {
			return nil, errors.New("control frame payload too large")
		}
	}

	// Read masking key
	var maskKey [4]byte
	if _, err := io.ReadFull(ws.reader, maskKey[:]); err != nil {
		return nil, err
	}

	// Read payload
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(ws.reader, payload); err != nil {
		return nil, err
	}

	// Unmask payload
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	return &Frame{
		Fin:     fin,
		Opcode:  opcode,
		Masked:  masked,
		Payload: payload,
	}, nil
}

// WriteFrame writes a WebSocket frame
func (ws *WebSocket) WriteFrame(frame *Frame) error {
	if ws.closed.Load() {
		return errors.New("websocket is closed")
	}

	return ws.writeFrame(frame)
}

// ReadMessage reads a complete message (handles fragmentation)
func (ws *WebSocket) ReadMessage() ([]byte, error) {
	var message []byte
	var messageType byte
	var inFragmentedMessage bool

	for {
		frame, err := ws.ReadFrame()
		if err != nil {
			return nil, err
		}

		// Handle control frames
		switch frame.Opcode {
		case OpcodePing:
			// Respond with pong
			if err := ws.WriteFrame(&Frame{
				Fin:     true,
				Opcode:  OpcodePong,
				Payload: frame.Payload,
			}); err != nil {
				return nil, err
			}
			continue

		case OpcodePong:
			// Ignore pong frames
			continue

		case OpcodeClose:
			// Handle close frame
			ws.Close()
			return nil, io.EOF
		}

		// First frame - remember message type
		if len(message) == 0 {
			if frame.Opcode == OpcodeContinuation {
				return nil, errors.New("unexpected continuation frame")
			}
			messageType = frame.Opcode
			if messageType != OpcodeText && messageType != OpcodeBinary {
				return nil, fmt.Errorf("invalid message type: %d", messageType)
			}
		} else if frame.Opcode != OpcodeContinuation {
			return nil, errors.New("expected continuation frame")
		}

		if len(message)+len(frame.Payload) > maxMessagePayloadSize {
			return nil, errors.New("websocket message too large")
		}

		// Append payload
		message = append(message, frame.Payload...)
		inFragmentedMessage = !frame.Fin

		// If FIN is set, message is complete
		if frame.Fin {
			break
		}
	}

	if inFragmentedMessage {
		return nil, errors.New("incomplete fragmented message")
	}

	return message, nil
}

// WriteMessage writes a complete message
func (ws *WebSocket) WriteMessage(opcode byte, data []byte) error {
	return ws.WriteFrame(&Frame{
		Fin:     true,
		Opcode:  opcode,
		Payload: data,
	})
}

// WriteText writes a text message
func (ws *WebSocket) WriteText(text string) error {
	return ws.WriteMessage(OpcodeText, []byte(text))
}

// WriteBinary writes a binary message
func (ws *WebSocket) WriteBinary(data []byte) error {
	return ws.WriteMessage(OpcodeBinary, data)
}

// Ping sends a ping frame
func (ws *WebSocket) Ping(data []byte) error {
	return ws.WriteFrame(&Frame{
		Fin:     true,
		Opcode:  OpcodePing,
		Payload: data,
	})
}

// Close closes the WebSocket connection
func (ws *WebSocket) Close() error {
	if !ws.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Send close frame
	closeFrame := &Frame{
		Fin:    true,
		Opcode: OpcodeClose,
		Payload: []byte{
			byte(CloseNormalClosure >> 8),
			byte(CloseNormalClosure & 0xFF),
		},
	}
	_ = ws.writeFrame(closeFrame)

	// Close underlying connection
	return ws.conn.Close()
}

// CloseWithCode closes the WebSocket with a specific code
func (ws *WebSocket) CloseWithCode(code uint16, reason string) error {
	if !ws.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Build close payload
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload[:2], code)
	copy(payload[2:], reason)

	// Send close frame
	closeFrame := &Frame{
		Fin:     true,
		Opcode:  OpcodeClose,
		Payload: payload,
	}
	_ = ws.writeFrame(closeFrame)

	// Close underlying connection
	return ws.conn.Close()
}

// SetReadDeadline sets the read deadline
func (ws *WebSocket) SetReadDeadline(t time.Time) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.readDeadline = t
}

// SetWriteDeadline sets the write deadline
func (ws *WebSocket) SetWriteDeadline(t time.Time) {
	ws.wmu.Lock()
	defer ws.wmu.Unlock()
	ws.writeDeadline = t
}

// IsClosed returns whether the WebSocket is closed
func (ws *WebSocket) IsClosed() bool {
	return ws.closed.Load()
}

func (ws *WebSocket) writeFrame(frame *Frame) error {
	ws.wmu.Lock()
	defer ws.wmu.Unlock()

	if frame == nil {
		return errors.New("frame cannot be nil")
	}
	if !isSupportedOpcode(frame.Opcode) {
		return fmt.Errorf("unsupported opcode: %d", frame.Opcode)
	}
	if isControlOpcode(frame.Opcode) {
		if !frame.Fin {
			return errors.New("control frames must not be fragmented")
		}
		if len(frame.Payload) > maxControlPayloadSize {
			return errors.New("control frame payload too large")
		}
	}
	if len(frame.Payload) > maxMessagePayloadSize {
		return errors.New("frame payload exceeds limit")
	}

	if !ws.writeDeadline.IsZero() {
		ws.conn.SetWriteDeadline(ws.writeDeadline)
	}

	// First byte (FIN, RSV, Opcode)
	b := byte(frame.Opcode)
	if frame.Fin {
		b |= 0x80
	}
	if err := ws.writer.WriteByte(b); err != nil {
		return err
	}

	// Payload length
	payloadLen := len(frame.Payload)
	if payloadLen < 126 {
		if err := ws.writer.WriteByte(byte(payloadLen)); err != nil {
			return err
		}
	} else if payloadLen <= 0xFFFF {
		if err := ws.writer.WriteByte(126); err != nil {
			return err
		}
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(payloadLen))
		if _, err := ws.writer.Write(buf[:]); err != nil {
			return err
		}
	} else {
		if err := ws.writer.WriteByte(127); err != nil {
			return err
		}
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(payloadLen))
		if _, err := ws.writer.Write(buf[:]); err != nil {
			return err
		}
	}

	if len(frame.Payload) > 0 {
		if _, err := ws.writer.Write(frame.Payload); err != nil {
			return err
		}
	}

	return ws.writer.Flush()
}

func headerContainsToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func isSupportedOpcode(opcode byte) bool {
	switch opcode {
	case OpcodeContinuation, OpcodeText, OpcodeBinary, OpcodeClose, OpcodePing, OpcodePong:
		return true
	default:
		return false
	}
}

func isControlOpcode(opcode byte) bool {
	return opcode == OpcodeClose || opcode == OpcodePing || opcode == OpcodePong
}

func isValidWebSocketKey(key string) bool {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	return err == nil && len(decoded) == 16
}

func isAllowedWebSocketOrigin(r *request.Request) bool {
	origin := strings.TrimSpace(r.GetHeader("Origin"))
	if origin == "" {
		return true
	}

	originURL, err := neturl.Parse(origin)
	if err != nil {
		return false
	}
	if originURL.Scheme != "http" && originURL.Scheme != "https" {
		return false
	}

	requestHost := r.Host
	if requestHost == "" && r.URL != nil {
		requestHost = r.URL.Host
	}
	if requestHost == "" {
		return false
	}

	return sameOriginAuthority(originURL, requestHost)
}

func sameOriginAuthority(originURL *neturl.URL, requestHost string) bool {
	originAuthority := canonicalAuthority(originURL.Host, defaultPortForScheme(originURL.Scheme))
	requestAuthority := canonicalAuthority(requestHost, defaultPortForScheme(originURL.Scheme))
	return originAuthority != "" && originAuthority == requestAuthority
}

func canonicalAuthority(authority, defaultPort string) string {
	host := strings.TrimSpace(strings.ToLower(authority))
	if host == "" {
		return ""
	}

	if parsedHost, port, err := net.SplitHostPort(host); err == nil {
		if port == "" {
			port = defaultPort
		}
		return parsedHost + ":" + port
	}

	if strings.Count(host, ":") > 1 && !strings.HasPrefix(host, "[") {
		return host + ":" + defaultPort
	}

	return host + ":" + defaultPort
}

func defaultPortForScheme(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}
