package integration

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
	"github.com/sanskar/http-server/internal/router"
	"github.com/sanskar/http-server/internal/server"
	"github.com/sanskar/http-server/pkg/httpserver"
)

func TestBasicHTTPRequest(t *testing.T) {
	// Create router
	r := router.New()

	r.GET("/", router.HandlerFunc(func(w response.ResponseWriter, req *request.Request) {
		w.WriteString("Hello, World!")
	}))

	// Create server
	config := server.DefaultConfig()
	config.Addr = ":9999"

	srv, err := server.New(config, r)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start server in background
	go func() {
		srv.Start()
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Make request
	conn, err := net.Dial("tcp", "localhost:9999")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send HTTP request
	request := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read status line: %v", err)
	}

	if !strings.Contains(statusLine, "200 OK") {
		t.Errorf("Expected 200 OK, got %s", statusLine)
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func TestParametricRoute(t *testing.T) {
	// Create router
	r := router.New()

	var capturedID string
	r.GET("/users/:id", router.HandlerFunc(func(w response.ResponseWriter, req *request.Request) {
		capturedID = req.PathParams["id"]
		w.WriteString(fmt.Sprintf("User: %s", capturedID))
	}))

	// Create server
	config := server.DefaultConfig()
	config.Addr = ":10000"

	srv, err := server.New(config, r)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start server
	go srv.Start()
	time.Sleep(100 * time.Millisecond)

	// Make request
	conn, err := net.Dial("tcp", "localhost:10000")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	request := "GET /users/123 HTTP/1.1\r\nHost: localhost\r\n\r\n"
	conn.Write([]byte(request))

	reader := bufio.NewReader(conn)
	statusLine, _ := reader.ReadString('\n')

	if !strings.Contains(statusLine, "200") {
		t.Errorf("Expected 200, got %s", statusLine)
	}

	if capturedID != "123" {
		t.Errorf("Expected captured ID=123, got %s", capturedID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func TestPOSTWithBody(t *testing.T) {
	r := router.New()

	var receivedBody string
	r.POST("/echo", router.HandlerFunc(func(w response.ResponseWriter, req *request.Request) {
		body := make([]byte, req.ContentLength)
		req.Body.Read(body)
		receivedBody = string(body)
		w.WriteString(receivedBody)
	}))

	config := server.DefaultConfig()
	config.Addr = ":10001"

	srv, _ := server.New(config, r)
	go srv.Start()
	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", "localhost:10001")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	testBody := "test=data"
	request := fmt.Sprintf("POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\n\r\n%s", len(testBody), testBody)
	conn.Write([]byte(request))

	reader := bufio.NewReader(conn)
	statusLine, _ := reader.ReadString('\n')

	if !strings.Contains(statusLine, "200") {
		t.Errorf("Expected 200, got %s", statusLine)
	}

	if receivedBody != testBody {
		t.Errorf("Expected body %s, got %s", testBody, receivedBody)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func TestMiddleware(t *testing.T) {
	r := router.New()

	var middlewareCalled bool
	testMiddleware := func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, req *request.Request) {
			middlewareCalled = true
			next.ServeHTTP(w, req)
		})
	}

	r.Use(testMiddleware)
	r.GET("/test", router.HandlerFunc(func(w response.ResponseWriter, req *request.Request) {
		w.WriteString("OK")
	}))

	config := server.DefaultConfig()
	config.Addr = ":10002"

	srv, _ := server.New(config, r)
	go srv.Start()
	time.Sleep(100 * time.Millisecond)

	conn, _ := net.Dial("tcp", "localhost:10002")
	defer conn.Close()

	request := "GET /test HTTP/1.1\r\nHost: localhost\r\n\r\n"
	conn.Write([]byte(request))

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	if !middlewareCalled {
		t.Error("Middleware was not called")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func TestNoContentResponse(t *testing.T) {
	addr, cleanup := reserveAddr(t)
	defer cleanup()

	srv := httpserver.New()
	srv.DELETE("/resource", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		w.WriteHeader(httpserver.StatusNoContent)
	})

	go func() {
		_ = srv.Listen(addr)
	}()
	waitForServer(t, addr)

	req, err := http.NewRequest(http.MethodDelete, "http://"+addr+"/resource", nil)
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("Expected empty body for 204, got %q", string(body))
	}

	shutdownServer(t, srv)
}

func TestStaticMountServesFilesFromRoot(t *testing.T) {
	addr, cleanupAddr := reserveAddr(t)
	defer cleanupAddr()

	rootDir := t.TempDir()
	filePath := filepath.Join(rootDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("static content"), 0644); err != nil {
		t.Fatalf("Failed to create static test file: %v", err)
	}

	srv := httpserver.New()
	srv.Static("/static", rootDir)

	go func() {
		_ = srv.Listen(addr)
	}()
	waitForServer(t, addr)

	resp, err := http.Get("http://" + addr + "/static/hello.txt")
	if err != nil {
		t.Fatalf("Failed to GET static file: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read static response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	if string(body) != "static content" {
		t.Fatalf("Expected static content, got %q", string(body))
	}

	shutdownServer(t, srv)
}

func TestGzipResponseWithAcceptEncoding(t *testing.T) {
	addr, cleanup := reserveAddr(t)
	defer cleanup()

	srv := httpserver.New()
	srv.GET("/gzip", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		_, _ = w.WriteString("compressed payload")
	})

	go func() {
		_ = srv.Listen(addr)
	}()
	waitForServer(t, addr)

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/gzip", nil)
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	client := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute gzip request: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Expected gzip encoding, got %q", resp.Header.Get("Content-Encoding"))
	}

	compressed, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read compressed body: %v", err)
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzipReader.Close()

	body, err := io.ReadAll(gzipReader)
	if err != nil {
		t.Fatalf("Failed to decompress body: %v", err)
	}
	if string(body) != "compressed payload" {
		t.Fatalf("Expected decompressed payload, got %q", string(body))
	}

	shutdownServer(t, srv)
}

func TestWebSocketUpgradeWithLoggerMiddleware(t *testing.T) {
	addr, cleanup := reserveAddr(t)
	defer cleanup()

	srv := httpserver.New()
	srv.Use(httpserver.Logger())
	srv.GET("/ws", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		ws, err := httpserver.UpgradeWebSocket(w, r)
		if err != nil {
			t.Errorf("Upgrade failed: %v", err)
			return
		}
		_ = ws.Close()
	})

	go func() {
		_ = srv.Listen(addr)
	}()
	waitForServer(t, addr)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	handshake := "" +
		"GET /ws HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: keep-alive, Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatalf("Failed to write handshake: %v", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read handshake response: %v", err)
	}
	if !strings.Contains(statusLine, "101 Switching Protocols") {
		t.Fatalf("Expected 101 Switching Protocols, got %s", statusLine)
	}

	shutdownServer(t, srv)
}

func TestHEADUsesGETHandlerWithoutBody(t *testing.T) {
	addr, cleanup := reserveAddr(t)
	defer cleanup()

	srv := httpserver.New()
	srv.GET("/head", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		w.Header()["Content-Type"] = []string{"text/plain"}
		_, _ = w.WriteString("body should be suppressed")
	})

	go func() {
		_ = srv.Listen(addr)
	}()
	waitForServer(t, addr)

	req, err := http.NewRequest(http.MethodHead, "http://"+addr+"/head", nil)
	if err != nil {
		t.Fatalf("Failed to build HEAD request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute HEAD request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain" {
		t.Fatalf("Expected Content-Type text/plain, got %q", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read HEAD body: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("Expected empty HEAD body, got %q", string(body))
	}

	shutdownServer(t, srv)
}

func reserveAddr(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to reserve address: %v", err)
	}

	addr := listener.Addr().String()
	_ = listener.Close()
	return addr, func() {}
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("Server did not start listening on %s", addr)
}

func shutdownServer(t *testing.T, srv *httpserver.Server) {
	t.Helper()

	if err := srv.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}
