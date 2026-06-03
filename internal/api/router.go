package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NewRouter creates the HTTP handler with API routes and static file serving.
func NewRouter(traceHandler *TraceHandler, metricsHandler *MetricsHandler) http.Handler {
	mux := http.NewServeMux()

	// API routes.
	mux.HandleFunc("/api/v1/traces/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/traces")
		if path == "" || path == "/" {
			traceHandler.ListTraces(w, r)
			return
		}
		traceIDHex := strings.TrimPrefix(path, "/")
		traceHandler.GetTrace(w, r, traceIDHex)
	})
	mux.HandleFunc("/api/v1/services", traceHandler.GetServices)

	// API routes — metrics (Prometheus API).
	if metricsHandler != nil {
		mux.HandleFunc("/api/v1/query", metricsHandler.InstantQuery)
		mux.HandleFunc("/api/v1/query_range", metricsHandler.RangeQuery)
		mux.HandleFunc("/api/v1/labels", metricsHandler.Labels)
		mux.HandleFunc("/api/v1/label/", func(w http.ResponseWriter, r *http.Request) {
			// Path: /api/v1/label/{name}/values
			path := strings.TrimPrefix(r.URL.Path, "/api/v1/label/")
			name := strings.TrimSuffix(path, "/values")
			metricsHandler.LabelValues(w, r, name)
		})
		mux.HandleFunc("/api/v1/metadata", metricsHandler.Metadata)
	}

	// Health check.
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Serve Vue SPA from filesystem. In production, web/dist contains the built frontend.
	// In development, the Vite dev server proxies /api requests.
	distPath := filepath.Join("web", "dist")
	if _, err := os.Stat(distPath); err == nil {
		spa := spaHandler{staticDir: distPath}
		mux.Handle("/", spa)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(devFallbackHTML))
		})
	}

	return mux
}

// spaHandler serves the Vue SPA from a directory on disk.
type spaHandler struct {
	staticDir string
}

func (s spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	fullPath := filepath.Join(s.staticDir, path)
	if _, err := os.Stat(fullPath); err == nil {
		http.ServeFile(w, r, fullPath)
		return
	}

	// Fallback: serve index.html for SPA client-side routing.
	indexPath := filepath.Join(s.staticDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

// devFallbackHTML is shown when the frontend hasn't been built yet.
const devFallbackHTML = `<!DOCTYPE html>
<html>
<head><title>Labubu (Dev Mode)</title></head>
<body style="font-family: sans-serif; max-width: 600px; margin: 80px auto; text-align: center;">
  <h1>Labubu</h1>
  <p>Frontend not built. In development, run the Vite dev server separately:</p>
  <pre>cd web && npm run dev</pre>
  <p>Then visit <a href="http://localhost:3001">http://localhost:3001</a></p>
</body>
</html>`
