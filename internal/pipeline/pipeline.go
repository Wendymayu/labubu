// Package pipeline provides asynchronous batch processing for trace ingestion.
// It buffers incoming spans and flushes to storage in batches for write efficiency.
package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/labubu/labubu/internal/storage"
)

// ErrBufferFull is returned when the ingest channel is at capacity.
var ErrBufferFull = fmt.Errorf("pipeline buffer full")

// Batch is a group of spans sharing the same Resource and Scope.
type Batch struct {
	Resource storage.ResourceInfo
	Scope    storage.ScopeInfo
	Spans    []storage.Span
}

// Pipeline buffers and batch-writes spans to the Store.
type Pipeline struct {
	store  storage.Store
	buf    chan *Batch
	wg     sync.WaitGroup
	done   chan struct{}
	closed bool
	mu     sync.Mutex
}

// New creates a Pipeline with the given buffer size.
func New(store storage.Store, bufSize int, flushInterval time.Duration) *Pipeline {
	p := &Pipeline{
		store: store,
		buf:   make(chan *Batch, bufSize),
		done:  make(chan struct{}),
	}
	p.wg.Add(1)
	go p.worker(flushInterval)
	return p
}

// Ingest enqueues a batch for writing. Returns ErrBufferFull if the channel
// is full (caller should return 503 to the OTLP sender).
func (p *Pipeline) Ingest(batch *Batch) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("pipeline closed")
	}
	p.mu.Unlock()

	select {
	case p.buf <- batch:
		return nil
	default:
		return ErrBufferFull
	}
}

// Shutdown gracefully stops the pipeline, flushing pending batches.
func (p *Pipeline) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	close(p.buf)
	p.wg.Wait()
	return nil
}

// worker drains the buffer channel and flushes batches to storage.
func (p *Pipeline) worker(flushInterval time.Duration) {
	defer p.wg.Done()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	var pending []*Batch

	for {
		select {
		case batch, ok := <-p.buf:
			if !ok {
				p.flush(pending)
				return
			}
			pending = append(pending, batch)

		case <-ticker.C:
			if len(pending) > 0 {
				p.flush(pending)
				pending = nil
			}
		}
	}
}

// flush writes all pending batches to storage.
func (p *Pipeline) flush(batches []*Batch) {
	ctx := context.Background()
	for _, b := range batches {
		if err := p.store.InsertSpans(ctx, b.Resource, b.Scope, b.Spans); err != nil {
			fmt.Printf("pipeline: flush error: %v\n", err)
		}
	}
}
