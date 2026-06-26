package api

import (
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/labubu/labubu/web"
)

// NewRouter creates the HTTP handler with API routes and static file serving.
func NewRouter(traceHandler *TraceHandler, metricsHandler *MetricsHandler, dashboardHandler *DashboardHandler, sessionHandler *SessionHandler, logHandler *LogHandler, pricingHandler *PricingHandler, llmConfigHandler *LLMConfigHandler, alertHandler http.Handler, costHandler *CostHandler) http.Handler {
	mux := http.NewServeMux()

	// API routes.
	mux.HandleFunc("/api/v1/traces/export", traceHandler.ExportTraces)
	mux.HandleFunc("/api/v1/traces/import", traceHandler.ImportTraces)
	mux.HandleFunc("/api/v1/traces/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/traces")
		if path == "" || path == "/" {
			traceHandler.ListTraces(w, r)
			return
		}
		path = strings.TrimPrefix(path, "/")
		parts := strings.SplitN(path, "/", 2)
		traceIDHex := parts[0]
		if len(parts) == 2 {
			switch parts[1] {
			case "diagnosis":
				if r.Method == http.MethodGet {
					traceHandler.GetDiagnosis(w, r, traceIDHex)
					return
				}
			case "diagnose":
				if r.Method == http.MethodPost {
					traceHandler.DiagnoseTrace(w, r, traceIDHex)
					return
				}
			}
		}
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
		mux.HandleFunc("/api/v1/metric-names", metricsHandler.MetricNames)
		mux.HandleFunc("/api/v1/otlp/v1/metrics", metricsHandler.IngestOTLP)
	}

	// API routes — dashboards.
	if dashboardHandler != nil {
		mux.HandleFunc("/api/v1/dashboards/", dashboardHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/dashboards", dashboardHandler.ServeHTTP)
	}

	// API routes — sessions.
	if sessionHandler != nil {
		mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions")
			if path == "" || path == "/" {
				sessionHandler.ListSessions(w, r)
				return
			}
			sessionID := strings.TrimPrefix(path, "/")
			// Check for sub-path: agent-stats
			parts := strings.SplitN(sessionID, "/", 2)
			if len(parts) == 2 && parts[1] == "agent-stats" {
				sessionHandler.GetAgentStats(w, r, parts[0])
				return
			}
			sessionHandler.GetSession(w, r, sessionID)
		})
		mux.HandleFunc("/api/v1/sessions", sessionHandler.ListSessions)
	}

	// API routes — logs.
	if logHandler != nil {
		mux.HandleFunc("/api/v1/logs/", logHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/logs", logHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/log-event-names", logHandler.GetEventNames)
	}

	// API routes — model pricing.
	if pricingHandler != nil {
		mux.HandleFunc("/api/v1/model-pricing/", pricingHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/model-pricing", pricingHandler.ServeHTTP)
	}

	// API routes — LLM configs.
	if llmConfigHandler != nil {
		mux.HandleFunc("/api/v1/llm-configs/", llmConfigHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/llm-configs", llmConfigHandler.ServeHTTP)
	}

	// API routes — alerting.
	if alertHandler != nil {
		mux.HandleFunc("/api/v1/alerts/", alertHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/alerts", alertHandler.ServeHTTP)
	}

	// API routes — cost summary.
	if costHandler != nil {
		mux.HandleFunc("/api/v1/cost-summary/", costHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/cost-summary", costHandler.ServeHTTP)
	}

	// Health check.
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// OpenAPI spec for the API docs page.
	mux.HandleFunc("/api/v1/openapi.json", OpenAPIHandler)

	// Serve Vue SPA from embedded or disk-based FS.
	spaFS, err := fs.Sub(web.StaticFS, "dist")
	if err == nil {
		if _, err := fs.Stat(spaFS, "index.html"); err == nil {
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				serveSPA(spaFS, w, r)
			})
			return mux
		}
	}

	// Fallback: frontend not built yet (dev mode without dist).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(devFallbackHTML))
	})

	return mux
}

// serveSPA serves a single-page app from an fs.FS.
// Static files are served directly; all other paths fall back to index.html
// for client-side routing.
func serveSPA(fsys fs.FS, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Try serving the requested file.
	if f, err := fsys.Open(path); err == nil {
		if serveFile(f, path, w, r) {
			return
		}
	}

	// Fallback to index.html for SPA client-side routing.
	if f, err := fsys.Open("index.html"); err == nil {
		if serveFile(f, "index.html", w, r) {
			return
		}
	}

	http.Error(w, "not found", http.StatusNotFound)
}

// serveFile serves a single file from an fs.FS. Returns true if the file was served.
func serveFile(f fs.File, name string, w http.ResponseWriter, r *http.Request) bool {
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}
	http.ServeContent(w, r, name, stat.ModTime(), rs)
	return true
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
