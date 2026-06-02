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
