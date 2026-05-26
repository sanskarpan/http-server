package httpserver

import "testing"

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config == nil {
		t.Fatal("expected default config")
	}
	if config.Addr == "" {
		t.Fatal("expected default addr")
	}
}

func TestDefaultStaticConfig(t *testing.T) {
	config := DefaultStaticConfig("./public")
	if config == nil {
		t.Fatal("expected default static config")
	}
	if config.Root != "./public" {
		t.Fatalf("expected root to be preserved, got %q", config.Root)
	}
}

func TestDefaultCORSConfig(t *testing.T) {
	config := DefaultCORSConfig()
	if config == nil {
		t.Fatal("expected default CORS config")
	}
	if len(config.AllowMethods) == 0 {
		t.Fatal("expected default allow methods")
	}
}

func TestStatsBeforeStart(t *testing.T) {
	server := New()
	stats := server.Stats()
	if stats.ActiveConnections != 0 {
		t.Fatalf("expected zero active connections, got %d", stats.ActiveConnections)
	}
	if stats.TotalConnections != 0 {
		t.Fatalf("expected zero total connections, got %d", stats.TotalConnections)
	}
	if stats.TotalRequests != 0 {
		t.Fatalf("expected zero total requests, got %d", stats.TotalRequests)
	}
}
