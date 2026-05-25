package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/sanskar/http-server/pkg/httpserver"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type UserStore struct {
	users  map[int]*User
	nextID int
	mu     sync.RWMutex
}

func NewUserStore() *UserStore {
	return &UserStore{
		users:  make(map[int]*User),
		nextID: 1,
	}
}

func (s *UserStore) Create(user *User) *User {
	s.mu.Lock()
	defer s.mu.Unlock()

	user.ID = s.nextID
	s.nextID++
	s.users[user.ID] = user

	return user
}

func (s *UserStore) Get(id int) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[id]
	return user, ok
}

func (s *UserStore) GetAll() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}

	return users
}

func (s *UserStore) Update(id int, user *User) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.users[id]; !ok {
		return false
	}

	user.ID = id
	s.users[id] = user
	return true
}

func (s *UserStore) Delete(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.users[id]; !ok {
		return false
	}

	delete(s.users, id)
	return true
}

func main() {
	server := httpserver.New()

	server.Use(httpserver.Logger())
	server.Use(httpserver.Recovery())
	server.Use(httpserver.CORS())
	server.Use(httpserver.RateLimit(10, 20))

	store := NewUserStore()
	api := server.Group("/api/v1")

	api.GET("/users", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		users := store.GetAll()
		data, err := json.Marshal(users)
		if err != nil {
			_ = httpserver.WriteError(w, httpserver.StatusInternalServerError, "Failed to marshal users")
			return
		}
		_ = httpserver.WriteJSON(w, httpserver.StatusOK, data)
	})

	api.GET("/users/:id", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		var id int
		fmt.Sscanf(r.PathParams["id"], "%d", &id)

		user, ok := store.Get(id)
		if !ok {
			_ = httpserver.WriteError(w, httpserver.StatusNotFound, "User not found")
			return
		}

		data, err := json.Marshal(user)
		if err != nil {
			_ = httpserver.WriteError(w, httpserver.StatusInternalServerError, "Failed to marshal user")
			return
		}

		_ = httpserver.WriteJSON(w, httpserver.StatusOK, data)
	})

	api.POST("/users", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		var user User

		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)

		if err := json.Unmarshal(body, &user); err != nil {
			_ = httpserver.WriteError(w, httpserver.StatusBadRequest, "Invalid JSON")
			return
		}

		created := store.Create(&user)
		data, err := json.Marshal(created)
		if err != nil {
			_ = httpserver.WriteError(w, httpserver.StatusInternalServerError, "Failed to marshal user")
			return
		}

		_ = httpserver.WriteJSON(w, httpserver.StatusCreated, data)
	})

	api.PUT("/users/:id", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		var id int
		fmt.Sscanf(r.PathParams["id"], "%d", &id)

		var user User
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)

		if err := json.Unmarshal(body, &user); err != nil {
			_ = httpserver.WriteError(w, httpserver.StatusBadRequest, "Invalid JSON")
			return
		}

		if !store.Update(id, &user) {
			_ = httpserver.WriteError(w, httpserver.StatusNotFound, "User not found")
			return
		}

		data, err := json.Marshal(&user)
		if err != nil {
			_ = httpserver.WriteError(w, httpserver.StatusInternalServerError, "Failed to marshal user")
			return
		}

		_ = httpserver.WriteJSON(w, httpserver.StatusOK, data)
	})

	api.DELETE("/users/:id", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		var id int
		fmt.Sscanf(r.PathParams["id"], "%d", &id)

		if !store.Delete(id) {
			_ = httpserver.WriteError(w, httpserver.StatusNotFound, "User not found")
			return
		}

		w.WriteHeader(httpserver.StatusNoContent)
	})

	server.GET("/health", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		_, _ = w.WriteString("OK\n")
	})

	log.Println("REST API server starting on :8080...")
	log.Println("Try: curl http://localhost:8080/api/v1/users")
	log.Fatal(server.Listen(":8080"))
}
