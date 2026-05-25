package request

import (
	"bufio"
	"bytes"
	"io"
	"testing"
)

func FuzzParserParse(f *testing.F) {
	seeds := [][]byte{
		[]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"),
		[]byte("POST /upload HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\n\r\ntest"),
		[]byte("POST /chunk HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nWiki\r\n0\r\n\r\n"),
		[]byte("HEAD /health HTTP/1.1\r\nHost: localhost\r\n\r\n"),
		[]byte("BAD / HTTP/1.1\r\n\r\n"),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 16<<10 {
			t.Skip()
		}

		parser := NewParser()
		parser.SetMaxHeaderSize(8 << 10)
		parser.SetMaxBodySize(8 << 10)

		req, err := parser.Parse(bufio.NewReader(bytes.NewReader(data)), "127.0.0.1:1234")
		if err != nil {
			return
		}

		if req == nil {
			t.Fatal("parser returned nil request without error")
		}
		if req.URL == nil {
			t.Fatal("parser returned nil URL")
		}
		if req.Body != nil {
			_, _ = io.Copy(io.Discard, req.Body)
			_ = req.Body.Close()
		}
	})
}
