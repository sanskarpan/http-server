package main

import (
	"fmt"
	"log"

	"github.com/sanskarpan/http-server/pkg/httpserver"
)

func main() {
	// Create server
	server := httpserver.New()

	// Add middleware
	server.Use(httpserver.Logger())
	server.Use(httpserver.Recovery())

	// Simple GET route
	server.GET("/", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		_, _ = w.WriteString("Hello, World!\n")
	})

	// Route with path parameter
	server.GET("/hello/:name", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		name := r.PathParams["name"]
		_, _ = w.WriteString(fmt.Sprintf("Hello, %s!\n", name))
	})

	// POST route
	server.POST("/echo", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)

		w.WriteHeader(httpserver.StatusOK)
		_, _ = w.Write(body)
	})

	// JSON route
	server.GET("/json", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		json := `{"message": "Hello, JSON!", "status": "success"}`
		_ = httpserver.WriteJSON(w, httpserver.StatusOK, []byte(json))
	})

	// HTML route
	server.GET("/html", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		html := `
<!DOCTYPE html>
<html>
<head>
    <title>HTTP Server</title>
</head>
<body>
    <h1>Welcome to HTTP Server!</h1>
    <p>This is a production-grade HTTP server built from scratch.</p>
</body>
</html>`
		_ = httpserver.WriteHTML(w, httpserver.StatusOK, html)
	})

	log.Println("Server starting on :8080...")
	log.Fatal(server.Listen(":8080"))
}
