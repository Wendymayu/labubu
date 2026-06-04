package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

// mockStore is a minimal Store implementation for handler testing.
type handlerMockStore struct {
	traces    *storage.TraceListResult
	detail    *storage.TraceDetail
	services  []string
	listErr   error
	detailErr error
}

func (m *handlerMockStore) InsertSpans(ctx context.Context, r storage.ResourceInfo, s storage.ScopeInfo, spans []storage.Span) error {
	return nil
}

func (m *handlerMockStore) ListTraces(ctx context.Context, q storage.TraceQuery) (*storage.TraceListResult, error) {
	return m.traces, m.listErr
}

func (m *handlerMockStore) GetTrace(ctx context.Context, id [16]byte) (*storage.TraceDetail, error) {
	return m.detail, m.detailErr
}

func (m *handlerMockStore) GetServices(ctx context.Context) ([]string, error) {
	return m.services, nil
}

func (m *handlerMockStore) ListSessions(ctx context.Context, q storage.SessionQuery) (*storage.SessionListResult, error) {
	return nil, nil
}

func (m *handlerMockStore) GetSession(ctx context.Context, sessionID string) (*storage.SessionDetail, error) {
	return nil, nil
}

func (m *handlerMockStore) Close() error { return nil }

func TestListTraces(t *testing.T) {
	store := &handlerMockStore{
		traces: &storage.TraceListResult{
			Traces: []storage.TraceListItem{
				{
					TraceIDHex:  "a1b2c3d4e5f600000000000000000000",
					RootName:    "test-trace",
					RootService: "test-service",
					DurationMS:  1234,
					SpanCount:   5,
					Status:      "OK",
				},
			},
			Pagination: storage.Pagination{Page: 1, PageSize: 20, Total: 1},
		},
	}

	handler := NewTraceHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces?page=1&page_size=20", nil)
	rec := httptest.NewRecorder()

	handler.ListTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.TraceListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(result.Traces) != 1 {
		t.Errorf("expected 1 trace, got %d", len(result.Traces))
	}
	if result.Traces[0].RootName != "test-trace" {
		t.Errorf("expected root_name 'test-trace', got '%s'", result.Traces[0].RootName)
	}
}

func TestGetTraceBadID(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/short", nil)
	rec := httptest.NewRecorder()

	handler.GetTrace(rec, req, "short")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short id, got %d", rec.Code)
	}
}
