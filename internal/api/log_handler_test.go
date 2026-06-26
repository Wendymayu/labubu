package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseLogQuerySpanID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?span_id=0102030405060708&trace_id=e16c42c68388d1d891d3d0c80a9892ca", nil)
	q := parseLogQuery(req)
	want := [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if q.SpanID != want {
		t.Errorf("SpanID = %x, want %x", q.SpanID, want)
	}
}

func TestParseLogQuerySpanIDInvalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?span_id=not-hex", nil)
	q := parseLogQuery(req)
	if q.SpanID != [8]byte{} {
		t.Errorf("SpanID = %x, want zero (invalid hex ignored)", q.SpanID)
	}
}

func TestGetLogCountsByTraceHandler(t *testing.T) {
	store := &handlerMockStore{logCounts: map[string]int{"50800eb5931cb62d": 3, "aabbccddeeff0011": 1}}
	h := NewLogHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/e16c42c68388d1d891d3d0c80a9892ca/counts", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Counts["50800eb5931cb62d"] != 3 {
		t.Errorf("count = %d, want 3", body.Counts["50800eb5931cb62d"])
	}
	if len(body.Counts) != 2 {
		t.Errorf("got %d spans, want 2", len(body.Counts))
	}
}

func TestGetLogCountsByTraceInvalidID(t *testing.T) {
	h := NewLogHandler(&handlerMockStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/not-hex/counts", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// Ensure the bare /logs/{id} path still routes to GetLogsByTrace (download path regression guard).
func TestGetLogsByTraceStillRouted(t *testing.T) {
	h := NewLogHandler(&handlerMockStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/e16c42c68388d1d891d3d0c80a9892ca", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
