package api

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// GzipMiddleware compresses text-like responses for clients that accept gzip.
// It is content-type aware: only JSON, HTML, CSS, JS, WASM, SVG, and plain
// text are compressed. Responses that are already encoded, partial (206), or
// cache-validation (304) pass through untouched, so Range requests and
// conditional GETs (used by http.ServeContent for static assets) keep working.
//
// Motivation: trace detail responses can reach several MB (large
// gen_ai.input.messages attributes). Server-side generation is fast (~0.3s);
// the bottleneck is transferring the uncompressed body. gzip cuts transfer
// size ~10x.
func GzipMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()
		h.ServeHTTP(gw, r)
	})
}

// gzipResponseWriter wraps an http.ResponseWriter to gzip-encode the body.
// The decision to compress is made at WriteHeader time (or the first Write for
// implicit 200s), once the handler has set Content-Type.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz           *gzip.Writer
	started      bool // committed to gzip-compressing this response
	wroteHeader  bool // WriteHeader has been called
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	g.wroteHeader = true
	if status == http.StatusOK && g.Header().Get("Content-Encoding") == "" && shouldGzip(g.Header().Get("Content-Type")) {
		g.Header().Set("Content-Encoding", "gzip")
		g.Header().Del("Content-Length")
		g.gz = gzip.NewWriter(g.ResponseWriter)
		g.started = true
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	// Only decide on the implicit-200 path when WriteHeader was NOT called.
	// If WriteHeader was called with a non-200 status (e.g. 206 Range), it
	// already decided not to compress — pass through unchanged.
	if !g.started && !g.wroteHeader && g.Header().Get("Content-Encoding") == "" && shouldGzip(g.Header().Get("Content-Type")) {
		g.Header().Set("Content-Encoding", "gzip")
		g.Header().Del("Content-Length")
		g.gz = gzip.NewWriter(g.ResponseWriter)
		g.started = true
	}
	if g.started {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// Flush forwards flushes so streaming endpoints keep working; flushes the gzip
// writer first so compressed bytes reach the client promptly.
func (g *gzipResponseWriter) Flush() {
	if g.started && g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipResponseWriter) close() {
	if g.started && g.gz != nil {
		g.gz.Close()
	}
}

// shouldGzip reports whether a response with the given Content-Type is worth
// compressing. Content-Type may include parameters (e.g. "; charset=utf-8").
func shouldGzip(ct string) bool {
	ct = strings.ToLower(ct)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	for _, t := range compressibleTypes {
		if ct == t {
			return true
		}
	}
	return false
}

var compressibleTypes = []string{
	"application/json",
	"application/manifest+json",
	"application/javascript",
	"application/wasm",
	"text/html",
	"text/css",
	"text/plain",
	"image/svg+xml",
}
