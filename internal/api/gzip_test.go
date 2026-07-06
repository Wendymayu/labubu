package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGzipMiddlewareCompressesJSON(t *testing.T) {
	// Simulate a large JSON response (like a trace detail payload).
	body := bytes.Repeat([]byte(`{"k":"v"},`), 1000)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	GzipMiddleware(handler).ServeHTTP(rec, req)

	if ce := rec.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", ce)
	}
	if cl := rec.Header().Get("Content-Length"); cl != "" {
		t.Fatalf("Content-Length = %q, want empty (size changed by gzip)", cl)
	}

	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	if !bytes.Equal(decompressed, body) {
		t.Fatalf("decompressed body does not match original")
	}
	if rec.Body.Len() >= len(body) {
		t.Fatalf("compressed size %d >= original %d (no compression happened)", rec.Body.Len(), len(body))
	}
}

func TestGzipMiddlewareSkipsWhenNoAcceptEncoding(t *testing.T) {
	body := []byte(`{"ok":true}`)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	GzipMiddleware(handler).ServeHTTP(rec, req)

	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding = %q, want empty (no Accept-Encoding)", ce)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatalf("body should pass through unchanged")
	}
}

func TestGzipMiddlewareSkipsNonTextContentType(t *testing.T) {
	body := []byte{0x00, 0x01, 0x02, 0x03}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(body)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	GzipMiddleware(handler).ServeHTTP(rec, req)

	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding = %q, want empty (binary not compressed)", ce)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatalf("binary body should pass through unchanged")
	}
}

func TestGzipMiddlewareSkips206(t *testing.T) {
	// Range responses (206 Partial Content) must NOT be compressed: the body
	// is a byte range into the original, gzipping would corrupt it.
	body := []byte("partial-bytes")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Content-Range", "bytes 0-11/100")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(body)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	GzipMiddleware(handler).ServeHTTP(rec, req)

	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding = %q, want empty (206 not compressed)", ce)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatalf("206 body should pass through unchanged")
	}
}

func TestGzipMiddlewareCompressesJavascriptLikeStatic(t *testing.T) {
	// Mimics http.ServeContent serving a .js asset. Go's mime package maps
	// .js to "text/javascript; charset=utf-8" (not application/javascript),
	// so both forms must be recognized.
	for _, ct := range []string{"text/javascript; charset=utf-8", "application/javascript"} {
		body := bytes.Repeat([]byte("console.log(1);"), 200)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			w.Write(body) // implicit 200, no WriteHeader call
		})

		req := httptest.NewRequest(http.MethodGet, "/assets/index.js", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		GzipMiddleware(handler).ServeHTTP(rec, req)

		if ce := rec.Header().Get("Content-Encoding"); ce != "gzip" {
			t.Errorf("Content-Type %q: Content-Encoding = %q, want gzip", ct, ce)
		}
		gr, _ := gzip.NewReader(rec.Body)
		decompressed, _ := io.ReadAll(gr)
		if !bytes.Equal(decompressed, body) {
			t.Errorf("Content-Type %q: decompressed body does not match original", ct)
		}
	}
}
