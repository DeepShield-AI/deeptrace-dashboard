package transport

import (
	"compress/gzip"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// RegisterStatic adds static file serving + SPA fallback.
// Must be registered last as it catches "/".
func RegisterStatic(mux *http.ServeMux, staticDir string) {
	mux.HandleFunc("/", handleStatic(staticDir))
}

func handleStatic(staticDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		filePath := filepath.Join(staticDir, path)
		// Prevent path traversal: r.URL.Path may contain encoded ".." segments
		// (e.g. /%2e%2e/%2e%2e/etc/passwd) that bypass ServeMux's cleanPath.
		// Reject any resolved path that escapes staticDir.
		if rel, err := filepath.Rel(staticDir, filePath); err != nil ||
			rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			// No cache for HTML files.
			if strings.HasSuffix(path, ".html") || path == "/" {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=31536000")
			}

			// Gzip compress JS/CSS files.
			ext := filepath.Ext(path)
			if (ext == ".js" || ext == ".css") && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Type", map[string]string{".js": "application/javascript", ".css": "text/css"}[ext])
				w.Header().Set("Vary", "Accept-Encoding")
				gz := gzip.NewWriter(w)
				defer gz.Close()
				data, _ := os.ReadFile(filePath)
				w.WriteHeader(200)
				gz.Write(data)
				return
			}
			http.ServeFile(w, r, filePath)
			return
		}

		// SPA fallback.
		spaExt := filepath.Ext(path)
		if spaExt != "" && spaExt != ".html" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
	}
}
