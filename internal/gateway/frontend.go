package gateway

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static static/*
var embeddedStaticFS embed.FS

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	requestPath := path.Clean(strings.TrimSpace(r.URL.Path))
	if requestPath == "." {
		requestPath = "/"
	}
	if strings.HasPrefix(requestPath, "/api/") || strings.HasPrefix(requestPath, "/t/") {
		http.NotFound(w, r)
		return
	}

	frontendFS, err := fs.Sub(embeddedStaticFS, "static")
	if err != nil {
		http.Error(w, "frontend not available", http.StatusInternalServerError)
		return
	}

	if requestPath == "/" {
		serveEmbeddedFile(w, r, frontendFS, "index.html")
		return
	}

	clean := strings.TrimPrefix(requestPath, "/")
	if hasEmbeddedFile(frontendFS, clean) {
		serveEmbeddedFile(w, r, frontendFS, clean)
		return
	}

	// SPA fallback.
	serveEmbeddedFile(w, r, frontendFS, "index.html")
}

func hasEmbeddedFile(fsys fs.FS, filename string) bool {
	info, err := fs.Stat(fsys, filename)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func serveEmbeddedFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, filename string) {
	content, err := fs.ReadFile(fsys, filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch {
	case strings.HasSuffix(filename, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(filename, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(filename, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(filename, ".json"):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}

	_, _ = w.Write(content)
}
