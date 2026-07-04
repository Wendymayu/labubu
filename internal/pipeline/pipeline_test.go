package pipeline

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/labubu/labubu/internal/storage"
)

// mockStore implements storage.Store for testing.
type mockStore struct {
	mu       sync.Mutex
	spans    []storage.Span
	traces   []storage.Trace
	services []string
	inserted int
}

func (m *mockStore) InsertSpans(ctx context.Context, r storage.ResourceInfo, s storage.ScopeInfo, spans []storage.Span) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spans = append(m.spans, spans...)
	m.inserted++
	return nil
}

func (m *mockStore) ListTraces(ctx context.Context, q storage.TraceQuery) (*storage.TraceListResult, error) {
	return &storage.TraceListResult{}, nil
}

func (m *mockStore) GetTrace(ctx context.Context, id [16]byte) (*storage.TraceDetail, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStore) GetServices(ctx context.Context) ([]string, error) {
	return m.services, nil
}

func (m *mockStore) ListSessions(ctx context.Context, q storage.SessionQuery) (*storage.SessionListResult, error) {
	return &storage.SessionListResult{}, nil
}

func (m *mockStore) GetSession(ctx context.Context, sessionID string, page, pageSize int) (*storage.SessionDetail, error) {
	return nil, nil
}

func (m *mockStore) Purge(ctx context.Context, maxAge time.Duration, maxCount int) (int, int, error) {
	return 0, 0, nil
}

func (m *mockStore) PurgeLogs(ctx context.Context, maxAge time.Duration) (int, error) {
	return 0, nil
}

func (m *mockStore) DeleteTraces(ctx context.Context, traceIDs [][16]byte) (int, int, error) {
	return 0, 0, nil
}

func (m *mockStore) InsertLogs(ctx context.Context, logs []storage.LogRecord) error { return nil }

func (m *mockStore) ListLogs(ctx context.Context, q storage.LogQuery) (*storage.LogListResult, error) {
	return nil, nil
}

func (m *mockStore) GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]storage.LogListItem, error) {
	return nil, nil
}

func (m *mockStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	return nil, nil
}

func (m *mockStore) GetLogEventNames(ctx context.Context) ([]string, error) { return nil, nil }

func (m *mockStore) GetLLMConfigs(ctx context.Context) ([]storage.LLMConfig, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockStore) CreateLLMConfig(ctx context.Context, c *storage.LLMConfig) error {
	return fmt.Errorf("not implemented")
}
func (m *mockStore) UpdateLLMConfig(ctx context.Context, c *storage.LLMConfig) error {
	return fmt.Errorf("not implemented")
}
func (m *mockStore) DeleteLLMConfig(ctx context.Context, id string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockStore) GetModelPricing(ctx context.Context) ([]storage.ModelPricing, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockStore) UpsertModelPricing(ctx context.Context, p storage.ModelPricing) error {
	return fmt.Errorf("not implemented")
}
func (m *mockStore) DeleteModelPricing(ctx context.Context, modelName string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockStore) UpdateTraceCost(ctx context.Context, traceID [16]byte) error {
	return fmt.Errorf("not implemented")
}
func (m *mockStore) GetCostSummary(ctx context.Context, q storage.CostQuery) (*storage.CostSummaryResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*storage.DiagnosisResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockStore) UpsertDiagnosisResult(ctx context.Context, r *storage.DiagnosisResult) error {
	return fmt.Errorf("not implemented")
}

func (m *mockStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*storage.AgentStats, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStore) GetSessionContextSpans(ctx context.Context, sessionID string) ([]storage.SessionContextSpan, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStore) Close() error { return nil }

func TestPipelineIngestAndFlush(t *testing.T) {
	store := &mockStore{}
	p := New(store, 10, 100*time.Millisecond)

	err := p.Ingest(&Batch{
		Resource: storage.ResourceInfo{Attributes: map[string]string{"service.name": "test-svc"}},
		Scope:    storage.ScopeInfo{Name: "test-scope"},
		Spans: []storage.Span{
			{Name: "span-1", TraceID: [16]byte{1}},
			{Name: "span-2", TraceID: [16]byte{1}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected ingest error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	store.mu.Lock()
	count := len(store.spans)
	store.mu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 spans flushed, got %d", count)
	}

	ctx := context.Background()
	p.Shutdown(ctx)
}

func TestPipelineBackpressure(t *testing.T) {
	store := &mockStore{}
	p := New(store, 1, time.Hour)

	err := p.Ingest(&Batch{Spans: []storage.Span{{Name: "span-1"}}})
	if err != nil {
		t.Fatalf("first ingest should succeed: %v", err)
	}

	select {
	case p.buf <- &Batch{Spans: []storage.Span{{Name: "span-2"}}}:
		t.Error("expected channel to be full")
	default:
		// Expected: channel full.
	}

	err = p.Ingest(&Batch{Spans: []storage.Span{{Name: "span-2"}}})
	if err != ErrBufferFull {
		t.Errorf("expected ErrBufferFull, got %v", err)
	}

	ctx := context.Background()
	p.Shutdown(ctx)
}
