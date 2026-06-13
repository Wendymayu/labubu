package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

// sessionMockStore is a mock for session handler testing.
type sessionMockStore struct {
	handlerMockStore // embed the existing mock for unused methods

	sessions      *storage.SessionListResult
	sessionDetail *storage.SessionDetail
	sessionErr    error
}

func (m *sessionMockStore) ListSessions(ctx context.Context, q storage.SessionQuery) (*storage.SessionListResult, error) {
	return m.sessions, m.sessionErr
}

func (m *sessionMockStore) GetSession(ctx context.Context, sessionID string) (*storage.SessionDetail, error) {
	return m.sessionDetail, m.sessionErr
}

func (m *sessionMockStore) InsertLogs(ctx context.Context, logs []storage.LogRecord) error { return nil }

func (m *sessionMockStore) ListLogs(ctx context.Context, q storage.LogQuery) (*storage.LogListResult, error) {
	return nil, nil
}

func (m *sessionMockStore) GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]storage.LogListItem, error) {
	return nil, nil
}

func (m *sessionMockStore) GetLogEventNames(ctx context.Context) ([]string, error) { return nil, nil }

func TestListSessions(t *testing.T) {
	tokens := uint32(5000)
	store := &sessionMockStore{
		sessions: &storage.SessionListResult{
			Sessions: []storage.SessionListItem{
				{
					SessionID:       "conv-123",
					TraceCount:      3,
					TotalTokens:     &tokens,
					TotalDurationMS: 4500,
					MaxDurationMS:   2000,
					AvgDurationMS:   1500,
					ErrorCount:      0,
					ErrorRate:       0,
					FirstActiveMS:   1717500000000,
					LastActiveMS:    1717500300000,
				},
			},
			Pagination: storage.Pagination{Page: 1, PageSize: 20, Total: 1},
		},
	}

	handler := NewSessionHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?page=1", nil)
	rec := httptest.NewRecorder()

	handler.ListSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.SessionListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(result.Sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(result.Sessions))
	}
	if result.Sessions[0].SessionID != "conv-123" {
		t.Errorf("expected session_id 'conv-123', got '%s'", result.Sessions[0].SessionID)
	}
	if result.Sessions[0].TraceCount != 3 {
		t.Errorf("expected trace_count 3, got %d", result.Sessions[0].TraceCount)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	store := &sessionMockStore{
		sessionDetail: nil,
	}

	handler := NewSessionHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent", nil)
	rec := httptest.NewRecorder()

	handler.GetSession(rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionEmptyID(t *testing.T) {
	store := &sessionMockStore{}
	handler := NewSessionHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/", nil)
	rec := httptest.NewRecorder()

	handler.GetSession(rec, req, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty session_id, got %d", rec.Code)
	}
}

func TestGetAgentStats(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		stats      *storage.AgentStats
		statsErr   error
		wantStatus int
		wantBody   string
	}{
		{
			name:      "success",
			sessionID: "sess-1",
			stats: &storage.AgentStats{
				TraceSuccessRate:   0.75,
				AvgToolSuccessRate: 0.92,
				ToolUsage:          []storage.ToolUsageItem{{ToolName: "file_read", CallCount: 10, SuccessRate: 1.0}},
				Insights:           []string{"file_read is reliable"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "no data",
			sessionID:  "sess-2",
			stats:      nil,
			wantStatus: http.StatusNotFound,
			wantBody:   "no_agent_data",
		},
		{
			name:       "store error",
			sessionID:  "sess-3",
			statsErr:   fmt.Errorf("db error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &agentStatsMockStore{
				handlerMockStore: handlerMockStore{},
				agentStats:    tt.stats,
				agentStatsErr: tt.statsErr,
			}
			handler := NewSessionHandler(store)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+tt.sessionID+"/agent-stats", nil)
			rec := httptest.NewRecorder()
			handler.GetAgentStats(rec, req, tt.sessionID)

			if rec.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" {
				var resp map[string]string
				json.Unmarshal(rec.Body.Bytes(), &resp)
				if resp["error"] != tt.wantBody {
					t.Errorf("error: got %v, want %v", resp["error"], tt.wantBody)
				}
			}
			if tt.stats != nil {
				var result storage.AgentStats
				json.Unmarshal(rec.Body.Bytes(), &result)
				if result.TraceSuccessRate != tt.stats.TraceSuccessRate {
					t.Errorf("trace_success_rate: got %v, want %v", result.TraceSuccessRate, tt.stats.TraceSuccessRate)
				}
			}
		})
	}
}

type agentStatsMockStore struct {
	handlerMockStore
	agentStats    *storage.AgentStats
	agentStatsErr error
}

func (m *agentStatsMockStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*storage.AgentStats, error) {
	return m.agentStats, m.agentStatsErr
}
