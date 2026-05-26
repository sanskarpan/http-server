package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
)

// StaticConfig contains configuration for static file serving
type StaticConfig struct {
	Root          string   // Root directory to serve from
	IndexFiles    []string // Index files (e.g., index.html)
	AllowDirList  bool     // Allow directory listing
	EnableCaching bool     // Enable ETag caching
	EnableRange   bool     // Enable range requests
	HiddenFiles   []string // Hidden file patterns
	MaxFileSize   int64    // Maximum file size to serve
}

// DefaultStaticConfig returns default static configuration
func DefaultStaticConfig(root string) *StaticConfig {
	return &StaticConfig{
		Root:          root,
		IndexFiles:    []string{"index.html", "index.htm"},
		AllowDirList:  false,
		EnableCaching: true,
		EnableRange:   true,
		HiddenFiles:   []string{".git", ".env", ".htaccess"},
		MaxFileSize:   100 << 20, // 100 MB
	}
}

// StaticFileServer returns a handler for serving static files
func StaticFileServer(config *StaticConfig) HandlerFunc {
	if config == nil {
		panic("static config cannot be nil")
	}

	// Normalize root path
	root, err := filepath.Abs(config.Root)
	if err != nil {
		panic(fmt.Sprintf("invalid root path: %v", err))
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		panic(fmt.Sprintf("invalid root path: %v", err))
	}

	return func(w response.ResponseWriter, r *request.Request) {
		// Only allow GET and HEAD
		if r.Method != "GET" && r.Method != "HEAD" {
			response.WriteError(w, response.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// Get requested path
		reqPath := r.URL.Path
		if filepathParam, ok := r.PathParams["filepath"]; ok {
			reqPath = "/" + filepathParam
		}

		// Security: validate path and prevent directory traversal
		safePath, err := validatePath(resolvedRoot, reqPath, config.HiddenFiles)
		if err != nil {
			response.WriteError(w, response.StatusForbidden, "Forbidden")
			return
		}

		// Get file info
		fileInfo, err := os.Stat(safePath)
		if err != nil {
			if os.IsNotExist(err) {
				response.WriteError(w, response.StatusNotFound, "File not found")
			} else {
				response.WriteError(w, response.StatusInternalServerError, "Internal error")
			}
			return
		}
		safePath, err = resolvePathWithinRoot(resolvedRoot, safePath, config.HiddenFiles)
		if err != nil {
			response.WriteError(w, response.StatusForbidden, "Forbidden")
			return
		}

		// Handle directories
		if fileInfo.IsDir() {
			// Try to serve index file
			indexServed := false
			for _, indexFile := range config.IndexFiles {
				indexPath := filepath.Join(safePath, indexFile)
				if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
					resolvedIndexPath, resolveErr := resolvePathWithinRoot(resolvedRoot, indexPath, config.HiddenFiles)
					if resolveErr != nil {
						response.WriteError(w, response.StatusForbidden, "Forbidden")
						return
					}
					safePath = resolvedIndexPath
					fileInfo = info
					indexServed = true
					break
				}
			}

			// If no index file and directory listing is disabled
			if !indexServed {
				if config.AllowDirList {
					serveDirListing(w, r, resolvedRoot, safePath, reqPath, config.HiddenFiles)
					return
				}
				response.WriteError(w, response.StatusForbidden, "Directory listing forbidden")
				return
			}
		}

		// Check file size
		if fileInfo.Size() > config.MaxFileSize {
			response.WriteError(w, response.StatusPayloadTooLarge, "File too large")
			return
		}

		// Serve file
		serveFile(w, r, safePath, fileInfo, config)
	}
}

// validatePath validates the requested path and prevents directory traversal
func validatePath(root, reqPath string, hiddenFiles []string) (string, error) {
	reqPath = strings.ReplaceAll(reqPath, "\\", "/")
	// Clean the request path
	reqPath = path.Clean("/" + reqPath)

	// Join with root
	fullPath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(reqPath, "/")))

	relPath, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", err
	}

	// Ensure the path is within root
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}

	// Check for hidden files
	if containsHiddenSegment(relPath, hiddenFiles) {
		return "", errors.New("hidden file access denied")
	}

	return fullPath, nil
}

func resolvePathWithinRoot(root, fullPath string, hiddenFiles []string) (string, error) {
	resolvedPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", err
	}

	relPath, err := filepath.Rel(root, resolvedPath)
	if err != nil {
		return "", err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", errors.New("symlink escapes root")
	}
	if containsHiddenSegment(relPath, hiddenFiles) {
		return "", errors.New("hidden file access denied")
	}

	return resolvedPath, nil
}

func containsHiddenSegment(relPath string, hiddenFiles []string) bool {
	segments := strings.Split(filepath.ToSlash(relPath), "/")
	for _, hidden := range hiddenFiles {
		for _, segment := range segments {
			if segment == hidden {
				return true
			}
		}
	}
	return false
}

// serveFile serves a single file
func serveFile(w response.ResponseWriter, r *request.Request, filePath string, fileInfo os.FileInfo, config *StaticConfig) {
	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		response.WriteError(w, response.StatusInternalServerError, "Cannot open file")
		return
	}
	defer file.Close()

	// Get file modification time
	modTime := fileInfo.ModTime()

	// Set Content-Type
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header()["Content-Type"] = []string{contentType}
	w.Header()["X-Content-Type-Options"] = []string{"nosniff"}

	// Handle caching (ETag and Last-Modified)
	if config.EnableCaching {
		// Generate ETag
		etag := generateETag(filePath, modTime, fileInfo.Size())
		w.Header()["ETag"] = []string{etag}
		w.Header()["Last-Modified"] = []string{modTime.UTC().Format(time.RFC1123)}
		w.Header()["Cache-Control"] = []string{"public, max-age=3600"}

		// Check If-None-Match (ETag)
		if inm := r.GetHeader("If-None-Match"); inm == etag {
			w.WriteHeader(response.StatusNotModified)
			return
		}

		// Check If-Modified-Since
		if ims := r.GetHeader("If-Modified-Since"); ims != "" {
			if t, err := time.Parse(time.RFC1123, ims); err == nil {
				if !modTime.After(t.Add(time.Second)) {
					w.WriteHeader(response.StatusNotModified)
					return
				}
			}
		}
	}

	// Handle range requests
	if config.EnableRange {
		rangeHeader := r.GetHeader("Range")
		if rangeHeader != "" {
			serveFileRange(w, r, file, fileInfo.Size(), rangeHeader)
			return
		}
	}

	// Serve entire file
	if writer, ok := response.Unwrap(w).(*response.Writer); ok {
		writer.SetContentLength(fileInfo.Size())
	}
	w.WriteHeader(response.StatusOK)

	// For HEAD requests, don't send body
	if r.Method == "HEAD" {
		return
	}

	// Copy file to response
	io.Copy(w, file)
}

// serveFileRange serves a file with range support
func serveFileRange(w response.ResponseWriter, r *request.Request, file *os.File, fileSize int64, rangeHeader string) {
	// Parse range header (e.g., "bytes=0-1023")
	ranges, err := parseRange(rangeHeader, fileSize)
	if err != nil || len(ranges) == 0 {
		response.WriteError(w, response.StatusRequestedRangeNotSatisfiable, "Invalid range")
		return
	}

	// Only support single range for now
	start, end := ranges[0][0], ranges[0][1]

	// Seek to start position
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		response.WriteError(w, response.StatusInternalServerError, "Seek error")
		return
	}

	// Set headers
	w.Header()["Content-Range"] = []string{fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize)}
	if writer, ok := response.Unwrap(w).(*response.Writer); ok {
		writer.SetContentLength(end - start + 1)
	}
	w.WriteHeader(response.StatusPartialContent)

	// For HEAD requests, don't send body
	if r.Method == "HEAD" {
		return
	}

	// Copy range to response
	io.CopyN(w, file, end-start+1)
}

// parseRange parses a Range header
func parseRange(rangeHeader string, fileSize int64) ([][2]int64, error) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return nil, errors.New("invalid range header")
	}

	rangeHeader = strings.TrimPrefix(rangeHeader, "bytes=")
	rangeParts := strings.Split(rangeHeader, ",")

	var ranges [][2]int64

	for _, part := range rangeParts {
		part = strings.TrimSpace(part)
		dashIndex := strings.Index(part, "-")
		if dashIndex == -1 {
			return nil, errors.New("invalid range format")
		}

		startStr := part[:dashIndex]
		endStr := part[dashIndex+1:]

		var start, end int64

		if startStr == "" {
			// Suffix range: -1000 (last 1000 bytes)
			if endStr == "" {
				return nil, errors.New("invalid range")
			}
			suffixLength, err := strconv.ParseInt(endStr, 10, 64)
			if err != nil {
				return nil, err
			}
			start = fileSize - suffixLength
			if start < 0 {
				start = 0
			}
			end = fileSize - 1
		} else {
			// Normal range: 0-1000 or 0-
			var err error
			start, err = strconv.ParseInt(startStr, 10, 64)
			if err != nil {
				return nil, err
			}

			if endStr == "" {
				end = fileSize - 1
			} else {
				end, err = strconv.ParseInt(endStr, 10, 64)
				if err != nil {
					return nil, err
				}
			}
		}

		// Validate range
		if start < 0 || start >= fileSize || end < start || end >= fileSize {
			return nil, errors.New("invalid range")
		}

		ranges = append(ranges, [2]int64{start, end})
	}

	return ranges, nil
}

// generateETag generates an ETag for a file
func generateETag(filePath string, modTime time.Time, size int64) string {
	// ETag based on path, mod time, and size
	data := fmt.Sprintf("%s-%d-%d", filePath, modTime.Unix(), size)
	hash := sha256.Sum256([]byte(data))
	return `"` + hex.EncodeToString(hash[:16]) + `"`
}

// serveDirListing serves a directory listing
func serveDirListing(w response.ResponseWriter, r *request.Request, root, dirPath, urlPath string, hiddenFiles []string) {
	// Read directory
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		response.WriteError(w, response.StatusInternalServerError, "Cannot read directory")
		return
	}

	// Build HTML
	escapedPath := html.EscapeString(urlPath)
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Index of %s</title>
    <style>
        body { font-family: monospace; margin: 20px; }
        h1 { font-size: 18px; }
        table { border-collapse: collapse; width: 100%%; }
        th, td { text-align: left; padding: 8px; border-bottom: 1px solid #ddd; }
        th { background-color: #f2f2f2; }
        a { text-decoration: none; color: #0066cc; }
        a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <h1>Index of %s</h1>
    <table>
        <tr>
            <th>Name</th>
            <th>Size</th>
            <th>Modified</th>
        </tr>
`, escapedPath, escapedPath)

	// Add parent directory link if not root
	if urlPath != "/" {
		htmlBody += `        <tr><td><a href="../">../</a></td><td>-</td><td>-</td></tr>` + "\n"
	}

	// Add entries
	for _, entry := range entries {
		if containsHiddenSegment(entry.Name(), hiddenFiles) {
			continue
		}

		entryPath := filepath.Join(dirPath, entry.Name())
		if _, err := resolvePathWithinRoot(root, entryPath, hiddenFiles); err != nil {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}

		size := "-"
		if !entry.IsDir() {
			size = formatSize(info.Size())
		}

		modTime := info.ModTime().Format("2006-01-02 15:04:05")
		displayName := html.EscapeString(name)
		href := directoryEntryHref(name)

		htmlBody += fmt.Sprintf(`        <tr><td><a href="%s">%s</a></td><td>%s</td><td>%s</td></tr>`+"\n",
			href, displayName, size, html.EscapeString(modTime))
	}

	htmlBody += `    </table>
</body>
</html>`

	w.Header()["X-Content-Type-Options"] = []string{"nosniff"}
	response.WriteHTML(w, response.StatusOK, htmlBody)
}

// formatSize formats a file size in human-readable format
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func directoryEntryHref(name string) string {
	isDir := strings.HasSuffix(name, "/")
	baseName := strings.TrimSuffix(name, "/")
	escaped := neturl.PathEscape(baseName)
	if isDir {
		return escaped + "/"
	}
	return escaped
}
