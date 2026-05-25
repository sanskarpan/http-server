package main

import (
	"log"
	"sync"

	"github.com/sanskar/http-server/pkg/httpserver"
)

type ChatRoom struct {
	clients map[*httpserver.WebSocket]bool
	mu      sync.RWMutex
}

func NewChatRoom() *ChatRoom {
	return &ChatRoom{
		clients: make(map[*httpserver.WebSocket]bool),
	}
}

func (c *ChatRoom) Add(ws *httpserver.WebSocket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients[ws] = true
	log.Printf("Client connected. Total: %d", len(c.clients))
}

func (c *ChatRoom) Remove(ws *httpserver.WebSocket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.clients, ws)
	log.Printf("Client disconnected. Total: %d", len(c.clients))
}

func (c *ChatRoom) Broadcast(message []byte) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for client := range c.clients {
		if err := client.WriteBinary(message); err != nil {
			log.Printf("Error broadcasting: %v", err)
		}
	}
}

func main() {
	server := httpserver.New()

	server.Use(httpserver.Logger())
	server.Use(httpserver.Recovery())

	room := NewChatRoom()

	server.GET("/", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		html := `
<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Chat</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        #messages { border: 1px solid #ccc; height: 400px; overflow-y: scroll; padding: 10px; margin-bottom: 10px; }
        #input { width: 80%; padding: 5px; }
        #send { padding: 5px 15px; }
        .message { margin: 5px 0; }
    </style>
</head>
<body>
    <h1>WebSocket Chat</h1>
    <div id="messages"></div>
    <input type="text" id="input" placeholder="Type a message...">
    <button id="send">Send</button>

    <script>
        const ws = new WebSocket('ws://localhost:8080/ws');
        const messages = document.getElementById('messages');
        const input = document.getElementById('input');
        const send = document.getElementById('send');

        ws.onopen = () => {
            console.log('Connected to chat');
            addMessage('System: Connected to chat');
        };

        ws.onmessage = (event) => {
            addMessage('User: ' + event.data);
        };

        ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            addMessage('System: Connection error');
        };

        ws.onclose = () => {
            console.log('Disconnected from chat');
            addMessage('System: Disconnected from chat');
        };

        send.onclick = () => {
            const message = input.value;
            if (message) {
                ws.send(message);
                addMessage('You: ' + message);
                input.value = '';
            }
        };

        input.onkeypress = (e) => {
            if (e.key === 'Enter') {
                send.click();
            }
        };

        function addMessage(message) {
            const div = document.createElement('div');
            div.className = 'message';
            div.textContent = message;
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        }
    </script>
</body>
</html>`
		_ = httpserver.WriteHTML(w, httpserver.StatusOK, html)
	})

	server.GET("/ws", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		ws, err := httpserver.UpgradeWebSocket(w, r)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			return
		}

		room.Add(ws)
		defer room.Remove(ws)
		defer ws.Close()

		for {
			message, err := ws.ReadMessage()
			if err != nil {
				log.Printf("Error reading message: %v", err)
				break
			}

			log.Printf("Received: %s", message)
			room.Broadcast(message)
		}
	})

	log.Println("WebSocket chat server starting on :8080...")
	log.Println("Open http://localhost:8080 in multiple browser windows")
	log.Fatal(server.Listen(":8080"))
}
