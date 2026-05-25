package router

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
)

// Handler is the interface for handling HTTP requests
type Handler interface {
	ServeHTTP(w response.ResponseWriter, r *request.Request)
}

// HandlerFunc is an adapter to allow ordinary functions to be used as handlers
type HandlerFunc func(w response.ResponseWriter, r *request.Request)

// ServeHTTP calls f(w, r)
func (f HandlerFunc) ServeHTTP(w response.ResponseWriter, r *request.Request) {
	f(w, r)
}

// Router is an HTTP request router
type Router struct {
	trees      map[string]*node // HTTP method -> root node
	middleware []Middleware
	notFound   Handler
	mu         sync.RWMutex
}

// Middleware is a function that wraps a handler
type Middleware func(Handler) Handler

// New creates a new router
func New() *Router {
	return &Router{
		trees: make(map[string]*node),
		notFound: HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			response.WriteError(w, response.StatusNotFound, "404 Not Found")
		}),
	}
}

// Use adds middleware to the router
func (r *Router) Use(middleware Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middleware = append(r.middleware, middleware)
}

// Handle registers a handler for a method and path
func (r *Router) Handle(method, path string, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if path == "" {
		panic("path cannot be empty")
	}

	if handler == nil {
		panic("handler cannot be nil")
	}

	// Normalize path
	if path[0] != '/' {
		path = "/" + path
	}

	// Get or create tree for method
	root := r.trees[method]
	if root == nil {
		root = &node{}
		r.trees[method] = root
	}

	// Add route to tree
	root.addRoute(path, handler)
}

// GET registers a GET handler
func (r *Router) GET(path string, handler Handler) {
	r.Handle("GET", path, handler)
}

// POST registers a POST handler
func (r *Router) POST(path string, handler Handler) {
	r.Handle("POST", path, handler)
}

// PUT registers a PUT handler
func (r *Router) PUT(path string, handler Handler) {
	r.Handle("PUT", path, handler)
}

// DELETE registers a DELETE handler
func (r *Router) DELETE(path string, handler Handler) {
	r.Handle("DELETE", path, handler)
}

// PATCH registers a PATCH handler
func (r *Router) PATCH(path string, handler Handler) {
	r.Handle("PATCH", path, handler)
}

// HEAD registers a HEAD handler
func (r *Router) HEAD(path string, handler Handler) {
	r.Handle("HEAD", path, handler)
}

// OPTIONS registers an OPTIONS handler
func (r *Router) OPTIONS(path string, handler Handler) {
	r.Handle("OPTIONS", path, handler)
}

// SetNotFound sets the handler for 404 Not Found
func (r *Router) SetNotFound(handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notFound = handler
}

// ServeHTTP implements the Handler interface
func (r *Router) ServeHTTP(w response.ResponseWriter, req *request.Request) {
	r.mu.RLock()
	root := r.trees[req.Method]
	if req.Method == "HEAD" && root == nil {
		root = r.trees["GET"]
	}
	r.mu.RUnlock()

	if root == nil {
		if allowed := r.allowedMethods(req.URL.Path, req.Method); len(allowed) > 0 {
			w.Header()["Allow"] = []string{strings.Join(allowed, ", ")}
			response.WriteError(w, response.StatusMethodNotAllowed, "405 Method Not Allowed")
			return
		}

		r.notFound.ServeHTTP(w, req)
		return
	}

	// Find handler
	handler, params := root.getValue(req.URL.Path)
	if handler == nil && req.Method == "HEAD" {
		r.mu.RLock()
		getRoot := r.trees["GET"]
		r.mu.RUnlock()
		if getRoot != nil && getRoot != root {
			handler, params = getRoot.getValue(req.URL.Path)
		}
	}
	if handler == nil {
		if allowed := r.allowedMethods(req.URL.Path, req.Method); len(allowed) > 0 {
			w.Header()["Allow"] = []string{strings.Join(allowed, ", ")}
			response.WriteError(w, response.StatusMethodNotAllowed, "405 Method Not Allowed")
			return
		}

		r.notFound.ServeHTTP(w, req)
		return
	}

	// Set path parameters in request
	if len(params) > 0 {
		req.PathParams = params
	}

	// Apply middleware
	for i := len(r.middleware) - 1; i >= 0; i-- {
		handler = wrapHandler(r.middleware[i], handler)
	}

	// Serve request
	handler.ServeHTTP(w, req)
}

func (r *Router) allowedMethods(path, currentMethod string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	allowed := make([]string, 0)
	for method, root := range r.trees {
		if method == currentMethod || root == nil {
			continue
		}

		handler, _ := root.getValue(path)
		if handler != nil {
			allowed = append(allowed, method)
		}
	}

	sort.Strings(allowed)
	return allowed
}

// wrapHandler wraps a Handler with middleware
func wrapHandler(middleware Middleware, handler Handler) Handler {
	return middleware(handler)
}

// Group creates a route group with a common prefix
type Group struct {
	router     *Router
	prefix     string
	middleware []Middleware
}

// NewGroup creates a new route group
func (r *Router) Group(prefix string) *Group {
	return &Group{
		router: r,
		prefix: prefix,
	}
}

// Use adds middleware to the group
func (g *Group) Use(middleware Middleware) {
	g.middleware = append(g.middleware, middleware)
}

// Handle registers a handler for a method and path in the group
func (g *Group) Handle(method, path string, handler Handler) {
	fullPath := g.prefix + path

	// Apply group middleware
	for i := len(g.middleware) - 1; i >= 0; i-- {
		handler = wrapHandler(g.middleware[i], handler)
	}

	g.router.Handle(method, fullPath, handler)
}

// GET registers a GET handler in the group
func (g *Group) GET(path string, handler Handler) {
	g.Handle("GET", path, handler)
}

// POST registers a POST handler in the group
func (g *Group) POST(path string, handler Handler) {
	g.Handle("POST", path, handler)
}

// PUT registers a PUT handler in the group
func (g *Group) PUT(path string, handler Handler) {
	g.Handle("PUT", path, handler)
}

// DELETE registers a DELETE handler in the group
func (g *Group) DELETE(path string, handler Handler) {
	g.Handle("DELETE", path, handler)
}

// PATCH registers a PATCH handler in the group
func (g *Group) PATCH(path string, handler Handler) {
	g.Handle("PATCH", path, handler)
}

// HEAD registers a HEAD handler in the group
func (g *Group) HEAD(path string, handler Handler) {
	g.Handle("HEAD", path, handler)
}

// OPTIONS registers an OPTIONS handler in the group
func (g *Group) OPTIONS(path string, handler Handler) {
	g.Handle("OPTIONS", path, handler)
}

// Group creates a sub-group
func (g *Group) Group(prefix string) *Group {
	return &Group{
		router:     g.router,
		prefix:     g.prefix + prefix,
		middleware: append([]Middleware{}, g.middleware...),
	}
}

// PrintRoutes prints all registered routes (for debugging)
func (r *Router) PrintRoutes() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for method, root := range r.trees {
		fmt.Printf("\nMethod: %s\n", method)
		printNode(root, "")
	}
}

// printNode recursively prints nodes
func printNode(n *node, prefix string) {
	if n == nil {
		return
	}

	fmt.Printf("%s%s", prefix, n.path)
	if n.handler != nil {
		fmt.Printf(" [HANDLER]")
	}
	if n.nodeType == param {
		fmt.Printf(" [PARAM:%s]", n.paramName)
	} else if n.nodeType == wildcard {
		fmt.Printf(" [WILDCARD:%s]", n.paramName)
	}
	fmt.Println()

	for _, child := range n.children {
		printNode(child, prefix+"  ")
	}
}
