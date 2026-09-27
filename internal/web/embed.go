package web

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed dist/*
var distFS embed.FS

func init() {
	// Ensure standard MIME types are explicitly registered across all operating systems,
	// preventing Windows registry quirks where .js defaults to text/plain.
	_ = mime.AddExtensionType(".js", "application/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".mjs", "application/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
	_ = mime.AddExtensionType(".html", "text/html; charset=utf-8")
	_ = mime.AddExtensionType(".json", "application/json; charset=utf-8")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".png", "image/png")
	_ = mime.AddExtensionType(".jpg", "image/jpeg")
	_ = mime.AddExtensionType(".jpeg", "image/jpeg")
	_ = mime.AddExtensionType(".ico", "image/x-icon")
	_ = mime.AddExtensionType(".woff2", "font/woff2")
	_ = mime.AddExtensionType(".woff", "font/woff")
	_ = mime.AddExtensionType(".ttf", "font/ttf")
}

// DistFS returns the embedded filesystem rooted at the "dist" directory.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

func hasPathTraversal(r *http.Request) bool {
	p := r.URL.Path
	if strings.Contains(p, "..") || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return true
	}
	raw := r.URL.RawPath
	if raw != "" {
		if strings.Contains(raw, "..") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
			return true
		}
	}
	reqURI := r.RequestURI
	if strings.Contains(reqURI, "..") || strings.HasPrefix(reqURI, "//") {
		return true
	}
	lowerURI := strings.ToLower(reqURI)
	if strings.Contains(lowerURI, "%2e") || strings.Contains(lowerURI, "%5c") {
		return true
	}
	cleaned := path.Clean(r.URL.Path)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return true
	}
	return false
}

// DistHandler returns an http.Handler that serves embedded static assets from dist/
// with SPA fallback to index.html for client-side routing.
func DistHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		sub = distFS
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET and HEAD methods for static files
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// API routes must not be intercepted by SPA fallback
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Reject path traversal and directory escape attempts
		if hasPathTraversal(r) {
			http.NotFound(w, r)
			return
		}

		// Clean the requested path using URL-safe path package (forward slashes)
		cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if cleanPath == "" || cleanPath == "." {
			cleanPath = "index.html"
		}

		// Direct request for index.html
		if cleanPath == "index.html" {
			serveIndex(w, r, sub)
			return
		}

		// Check if file exists on disk in embedded filesystem
		f, err := sub.Open(cleanPath)
		if err == nil {
			stat, statErr := f.Stat()
			f.Close()
			if statErr == nil && !stat.IsDir() {
				// Caching headers: hashed assets in /assets/ are immutable and long-term cacheable
				if strings.HasPrefix(cleanPath, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA Fallback: Requested path is not an existing file on disk.
		// Serve index.html with 200 OK so client-side router (e.g. /session, /players/123, /?mode=overlay) can handle it.
		serveIndex(w, r, sub)
	})
}

// serveIndex writes the embedded index.html file with no-cache headers.
func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	f, err := sub.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	rs, ok := f.(io.ReadSeeker)
	if ok {
		http.ServeContent(w, r, "index.html", stat.ModTime(), rs)
		return
	}

	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, "index.html", stat.ModTime(), bytes.NewReader(data))
}
