//go:build !cgo || !local_engine

// Package storage provides an in-memory Store implementation for non-CGO builds.
// When compiling without CGO (or without the local_engine tag), NewChDBStore
// returns this in-memory store so the binary compiles and runs for development.
package storage

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

// memStore is an in-memory Store used when chDB CGO is not available.
type memStore struct {
	mu       sync.RWMutex
	spans    []Span
	traces   map[[16]byte]Trace
	services map[string]bool
}

// NewChDBStore returns an in-memory Store when chDB is not available.
// The dataDir parameter is ignored. Data is lost on restart.
func NewChDBStore(dataDir string) (Store, error) {
	_ = dataDir
	return &memStore{
		spans:    make([]Span, 0),
		traces:   make(map[[16]byte]Trace),
		services: make(map[string]bool),
	}, nil
}

func (m *memStore) InsertSpans(ctx context.Context, resource ResourceInfo, scope ScopeInfo, inSpans []Span) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()

	m.spans = append(m.spans, inSpans...)

	// Aggregate into traces.
	traceMap := aggregateTraces(resource, scope, inSpans)
	for traceID, trace := range traceMap {
		if existing, ok := m.traces[traceID]; ok {
			// Merge: update counts, time range, tokens.
			existing.SpanCount += trace.SpanCount
			if trace.StartTimeMS < existing.StartTimeMS {
				existing.StartTimeMS = trace.StartTimeMS
			}
			if trace.EndTimeMS > existing.EndTimeMS {
				existing.EndTimeMS = trace.EndTimeMS
				existing.DurationMS = existing.EndTimeMS - existing.StartTimeMS
			}
			if trace.TotalTokens != nil {
				if existing.TotalTokens == nil {
					v := *trace.TotalTokens
					existing.TotalTokens = &v
				} else {
					sum := *existing.TotalTokens + *trace.TotalTokens
					existing.TotalTokens = &sum
				}
			}
			// Root span: keep the one with zero parent (first or updated).
			if isRootSpan(trace.RootSpanID) {
				existing.RootSpanID = trace.RootSpanID
				existing.RootName = trace.RootName
				existing.StatusCode = trace.StatusCode
				existing.StatusMessage = trace.StatusMessage
			}
			m.traces[traceID] = existing
		} else {
			m.traces[traceID] = trace
		}
	}

	// Track service names.
	if svc := resource.Attributes["service.name"]; svc != "" {
		m.services[svc] = true
	}

	return nil
}

func (m *memStore) ListTraces(ctx context.Context, q TraceQuery) (*TraceListResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect and filter traces.
	filtered := make([]Trace, 0, len(m.traces))
	for _, t := range m.traces {
		if q.Service != "" {
			svc := t.ResourceAttrs["service.name"]
			if svc != q.Service {
				continue
			}
		}
		if q.Status != "" {
			if StatusCodeToString(t.StatusCode) != q.Status {
				continue
			}
		}
		if q.Query != "" {
			if !containsSubstring(t.RootName, q.Query) {
				continue
			}
		}
		if q.StartTimeMS > 0 && t.StartTimeMS < q.StartTimeMS {
			continue
		}
		if q.EndTimeMS > 0 && t.StartTimeMS > q.EndTimeMS {
			continue
		}
		if q.MinDuration > 0 && t.DurationMS < q.MinDuration {
			continue
		}
		if q.MaxDuration > 0 && t.DurationMS > q.MaxDuration {
			continue
		}
		filtered = append(filtered, t)
	}

	// Sort by start time descending.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartTimeMS > filtered[j].StartTimeMS
	})

	total := len(filtered)

	// Paginate.
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}

	page := filtered[start:end]
	if page == nil {
		page = make([]Trace, 0)
	}

	items := make([]TraceListItem, len(page))
	for i, t := range page {
		items[i] = TraceListItem{
			TraceIDHex:  TraceIDToHex(t.TraceID),
			RootSpanID:  SpanIDToHex(t.RootSpanID),
			RootName:    t.RootName,
			RootService: t.ResourceAttrs["service.name"],
			StartTimeMS: t.StartTimeMS,
			DurationMS:  t.DurationMS,
			SpanCount:   t.SpanCount,
			Status:      StatusCodeToString(t.StatusCode),
			TotalTokens: t.TotalTokens,
		}
	}

	return &TraceListResult{
		Traces: items,
		Pagination: Pagination{
			Page:     q.Page,
			PageSize: q.PageSize,
			Total:    total,
		},
	}, nil
}

func (m *memStore) GetTrace(ctx context.Context, traceID [16]byte) (*TraceDetail, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find all spans belonging to this trace.
	var traceSpans []Span
	for _, s := range m.spans {
		if s.TraceID == traceID {
			traceSpans = append(traceSpans, s)
		}
	}

	if len(traceSpans) == 0 {
		return nil, nil
	}

	// Sort spans by start time.
	sort.Slice(traceSpans, func(i, j int) bool {
		return traceSpans[i].StartTimeMS < traceSpans[j].StartTimeMS
	})

	detailSpans := make([]SpanDetail, len(traceSpans))
	var rootSpanID [8]byte
	rootIdx := 0
	for i, s := range traceSpans {
		// Parse events and links JSON.
		events := parseJSONArray(s.Events)
		links := parseJSONArray(s.Links)

		detailSpans[i] = SpanDetail{
			SpanID:            SpanIDToHex(s.SpanID),
			ParentSpanID:      SpanIDToHex(s.ParentSpanID),
			Name:              s.Name,
			Kind:              KindToString(s.Kind),
			StartTimeMS:       s.StartTimeMS,
			DurationMS:        s.DurationMS,
			Attributes:        s.Attributes,
			Events:            events,
			Links:             links,
			Status:            StatusCodeToString(s.StatusCode),
			StatusMessage:     s.StatusMessage,
			InputTokens:       s.InputTokens,
			OutputTokens:      s.OutputTokens,
			TotalTokens:       s.TotalTokens,
			GenAIRequestModel: s.GenAIRequestModel,
		}

		if s.ParentSpanID == [8]byte{} {
			rootSpanID = s.SpanID
			rootIdx = i
		}
	}

	// Use the root span's attributes as resource attrs if available.
	resourceAttrs := make(map[string]string)
	if trace, ok := m.traces[traceID]; ok {
		resourceAttrs = trace.ResourceAttrs
	}

	return &TraceDetail{
		TraceIDHex:    TraceIDToHex(traceID),
		RootSpanID:    SpanIDToHex(rootSpanID),
		SpanCount:     len(traceSpans),
		StartTimeMS:   detailSpans[rootIdx].StartTimeMS,
		DurationMS:    detailSpans[rootIdx].DurationMS,
		ResourceAttrs: resourceAttrs,
		Spans:         detailSpans,
	}, nil
}

func (m *memStore) GetServices(ctx context.Context) ([]string, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	services := make([]string, 0, len(m.services))
	for s := range m.services {
		services = append(services, s)
	}
	sort.Strings(services)
	return services, nil
}

func (m *memStore) Close() error {
	return nil
}

// parseJSONArray parses a JSON array string into []interface{}.
// Returns an empty slice if parsing fails or the string is empty/"[]".
func parseJSONArray(raw string) []interface{} {
	if raw == "" || raw == "[]" {
		return make([]interface{}, 0)
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return make([]interface{}, 0)
	}
	return arr
}

// containsSubstring does a simple case-insensitive substring match.
func containsSubstring(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			cs := s[i+j]
			qc := substr[j]
			// Case-insensitive ASCII comparison.
			if cs >= 'A' && cs <= 'Z' {
				cs = cs + 32
			}
			if qc >= 'A' && qc <= 'Z' {
				qc = qc + 32
			}
			if cs != qc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
