package router

// nodeType represents the type of router node
type nodeType uint8

const (
	static   nodeType = iota // Static path segment
	param                    // Parameter path segment (:id)
	wildcard                 // Wildcard path segment (*path)
)

// node represents a node in the radix tree
type node struct {
	path      string   // Path segment
	indices   string   // First character of each child
	children  []*node  // Child nodes
	handler   Handler  // Handler for this path
	nodeType  nodeType // Type of node
	paramName string   // Name of parameter (:id -> "id")
	priority  uint32   // Priority (number of children)
}

// addRoute adds a route to the tree
func (n *node) addRoute(path string, handler Handler) {
	fullPath := path
	n.priority++

	// Empty tree
	if n.path == "" && len(n.children) == 0 {
		n.insertChild(path, fullPath, handler)
		n.nodeType = static
		return
	}

	// Walk the tree
walk:
	for {
		// Find longest common prefix
		i := longestCommonPrefix(path, n.path)

		// Split edge
		if i < len(n.path) {
			child := &node{
				path:      n.path[i:],
				children:  n.children,
				handler:   n.handler,
				indices:   n.indices,
				priority:  n.priority - 1,
				nodeType:  n.nodeType,
				paramName: n.paramName,
			}

			n.children = []*node{child}
			n.indices = string([]byte{n.path[i]})
			n.path = path[:i]
			n.handler = nil
			n.nodeType = static
			n.paramName = ""
		}

		// Make new node a child of this node
		if i < len(path) {
			path = path[i:]

			// Parameter or wildcard
			if path[0] == ':' || path[0] == '*' {
				// Check if a similar child exists
				for _, child := range n.children {
					if child.nodeType != static {
						n = child
						continue walk
					}
				}

				// Create new parameter/wildcard node
				if path[0] == '*' {
					n.insertChild(path, fullPath, handler)
					return
				}

				n.insertChild(path, fullPath, handler)
				return
			}

			// Static segment
			c := path[0]

			// Check if child with this prefix character exists
			for i, index := range []byte(n.indices) {
				if c == index {
					n = n.children[i]
					continue walk
				}
			}

			// No child found, create new one
			n.indices += string([]byte{c})
			child := &node{}
			n.children = append(n.children, child)
			n.incrementChildPrio(len(n.children) - 1)
			n = child
			n.insertChild(path, fullPath, handler)
			return
		}

		// Exact match - set handler
		if n.handler != nil {
			panic("handler already exists for path: " + fullPath)
		}
		n.handler = handler
		return
	}
}

// insertChild inserts a child node
func (n *node) insertChild(path, fullPath string, handler Handler) {
	for {
		tokenIndex := -1
		var token byte
		for i := 0; i < len(path); i++ {
			if path[i] == ':' || path[i] == '*' {
				tokenIndex = i
				token = path[i]
				break
			}
		}

		if tokenIndex == -1 {
			n.path = path
			n.handler = handler
			n.nodeType = static
			return
		}

		end := tokenIndex + 1
		for end < len(path) && path[end] != '/' {
			end++
		}

		if end-tokenIndex <= 1 {
			panic("empty parameter name in path: " + fullPath)
		}

		if token == '*' && end < len(path) {
			panic("wildcard must be at the end of path: " + fullPath)
		}

		if tokenIndex > 0 {
			n.path = path[:tokenIndex]
			path = path[tokenIndex:]
			end -= tokenIndex
		}

		child := &node{
			nodeType:  param,
			paramName: path[1:end],
			priority:  1,
		}
		if token == '*' {
			child.nodeType = wildcard
		}

		n.children = []*node{child}
		n.indices = string(path[0])
		n = child

		if end == len(path) {
			n.path = path
			n.handler = handler
			return
		}

		n.path = path[:end]
		path = path[end:]

		next := &node{priority: 1}
		n.children = []*node{next}
		n = next
	}
}

// getValue searches for a handler and path parameters
func (n *node) getValue(path string) (handler Handler, params map[string]string) {
	params = make(map[string]string)

walk:
	for {
		// If this is a parameter node, extract the value
		if n.nodeType == param {
			// Find end of parameter value
			end := 0
			for end < len(path) && path[end] != '/' {
				end++
			}

			// Store parameter value
			params[n.paramName] = path[:end]
			path = path[end:]

			// If there's more path, continue with children
			if len(path) > 0 && len(n.children) > 0 {
				n = n.children[0]
				continue walk
			}

			// No more path - return handler
			return n.handler, params
		}

		// If this is a wildcard node, capture rest of path
		if n.nodeType == wildcard {
			params[n.paramName] = path
			return n.handler, params
		}

		// Static node - match prefix
		if len(path) >= len(n.path) && path[:len(n.path)] == n.path {
			path = path[len(n.path):]

			// If path is fully consumed, return handler
			if len(path) == 0 {
				return n.handler, params
			}

			// Look for matching child
			c := path[0]
			for i, index := range []byte(n.indices) {
				if c == index {
					n = n.children[i]
					continue walk
				}
			}

			// No matching static child, check for parameter/wildcard children
			for _, child := range n.children {
				if child.nodeType == param || child.nodeType == wildcard {
					n = child
					continue walk
				}
			}
		}

		return nil, nil
	}
}

// incrementChildPrio increments child priority
func (n *node) incrementChildPrio(pos int) int {
	cs := n.children
	cs[pos].priority++
	prio := cs[pos].priority

	// Adjust position (higher priority first)
	newPos := pos
	for newPos > 0 && cs[newPos-1].priority < prio {
		cs[newPos-1], cs[newPos] = cs[newPos], cs[newPos-1]
		newPos--
	}

	// Update indices
	if newPos != pos {
		n.indices = n.indices[:newPos] +
			n.indices[pos:pos+1] +
			n.indices[newPos:pos] +
			n.indices[pos+1:]
	}

	return newPos
}

// longestCommonPrefix finds the longest common prefix
func longestCommonPrefix(a, b string) int {
	i := 0
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for i < max && a[i] == b[i] {
		i++
	}
	return i
}
