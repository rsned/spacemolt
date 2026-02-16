package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// spaFileServer serves static files with SPA fallback: any request for a path
// that doesn't match an existing file returns index.html.
type spaFileServer struct {
	root string
}

func newSPAFileServer(root string) http.Handler {
	return &spaFileServer{root: root}
}

func (s *spaFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the path.
	p := filepath.Clean(r.URL.Path)
	if p == "." {
		p = "/"
	}

	// Try to serve the file directly.
	fullPath := filepath.Join(s.root, p)
	info, err := os.Stat(fullPath)
	if err == nil && !info.IsDir() {
		http.ServeFile(w, r, fullPath)
		return
	}

	// For API and WebSocket paths, don't fall back.
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/ws" {
		http.NotFound(w, r)
		return
	}

	// SPA fallback: serve index.html.
	indexPath := filepath.Join(s.root, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, indexPath)
}
