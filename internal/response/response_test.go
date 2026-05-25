package response

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type testConn struct {
	readBuf  bytes.Buffer
	writeBuf bytes.Buffer
}

func (c *testConn) Read(p []byte) (int, error)       { return c.readBuf.Read(p) }
func (c *testConn) Write(p []byte) (int, error)      { return c.writeBuf.Write(p) }
func (c *testConn) Close() error                     { return nil }
func (c *testConn) LocalAddr() net.Addr              { return nil }
func (c *testConn) RemoteAddr() net.Addr             { return nil }
func (c *testConn) SetDeadline(time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(time.Time) error { return nil }

func TestWriterFlushDoesNotFinalizeChunkedResponse(t *testing.T) {
	conn := &testConn{}
	writer := NewWriter(conn)

	if _, err := writer.WriteString("hello"); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if _, err := writer.WriteString("world"); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	response := conn.writeBuf.String()
	if !strings.Contains(response, "5\r\nhello\r\n5\r\nworld\r\n0\r\n\r\n") {
		t.Fatalf("unexpected chunked response framing: %q", response)
	}
	if strings.Count(response, "0\r\n\r\n") != 1 {
		t.Fatalf("expected exactly one final chunk, got %d", strings.Count(response, "0\r\n\r\n"))
	}
}

func TestWriterCompressionFinalizesOnlyOnce(t *testing.T) {
	conn := &testConn{}
	writer := NewWriter(conn)

	if err := writer.EnableCompression(6); err != nil {
		t.Fatalf("enable compression failed: %v", err)
	}
	if _, err := writer.WriteString("compressed "); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	flushedLen := conn.writeBuf.Len()
	if flushedLen == 0 {
		t.Fatal("expected compressed bytes to be emitted on flush before close")
	}
	if _, err := writer.WriteString("payload"); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	firstLen := conn.writeBuf.Len()
	if err := writer.Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
	if conn.writeBuf.Len() != firstLen {
		t.Fatalf("writer emitted duplicate bytes after second close")
	}

	raw := conn.writeBuf.Bytes()
	if !bytes.Contains(raw, []byte("Transfer-Encoding: chunked")) {
		t.Fatalf("expected chunked transfer for streaming compression: %q", string(raw))
	}
	headerEnd := bytes.Index(raw, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		t.Fatalf("missing header terminator")
	}

	compressedBody := decodeChunkedBody(t, raw[headerEnd+4:])
	gzipReader, err := gzip.NewReader(bufio.NewReader(bytes.NewReader(compressedBody)))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gzipReader.Close()

	body, err := io.ReadAll(gzipReader)
	if err != nil {
		t.Fatalf("failed to read gzip body: %v", err)
	}
	if string(body) != "compressed payload" {
		t.Fatalf("expected compressed payload, got %q", string(body))
	}
}

func TestWriterFlushHeaderOnlyResponse(t *testing.T) {
	conn := &testConn{}
	writer := NewWriter(conn)
	writer.WriteHeader(StatusSwitchingProtocols)

	if err := writer.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	response := conn.writeBuf.String()
	if !strings.Contains(response, "HTTP/1.1 101 Switching Protocols\r\n") {
		t.Fatalf("unexpected response: %q", response)
	}
	if strings.Contains(response, "0\r\n\r\n") {
		t.Fatalf("header-only response should not emit chunk terminator: %q", response)
	}
}

func decodeChunkedBody(t *testing.T, data []byte) []byte {
	t.Helper()

	reader := bufio.NewReader(bytes.NewReader(data))
	var decoded bytes.Buffer

	for {
		sizeLine, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read chunk size: %v", err)
		}
		sizeLine = strings.TrimSpace(sizeLine)
		var size int
		if _, err := fmt.Sscanf(sizeLine, "%x", &size); err != nil {
			t.Fatalf("failed to parse chunk size %q: %v", sizeLine, err)
		}
		if size == 0 {
			if _, err := reader.ReadString('\n'); err != nil {
				t.Fatalf("failed to read final CRLF: %v", err)
			}
			break
		}

		chunk := make([]byte, size)
		if _, err := io.ReadFull(reader, chunk); err != nil {
			t.Fatalf("failed to read chunk: %v", err)
		}
		decoded.Write(chunk)

		crlf := make([]byte, 2)
		if _, err := io.ReadFull(reader, crlf); err != nil {
			t.Fatalf("failed to read chunk terminator: %v", err)
		}
	}

	return decoded.Bytes()
}
