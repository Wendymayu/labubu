//go:build !cgo || !local_engine

// Package storage provides an in-memory Store implementation for non-CGO builds.
// When compiling without CGO (or without the local_engine tag), NewChDBStore
// returns this in-memory store so the binary compiles and runs for development.
package storage

import (
	"context"
	"sort"
	"sync"
	"time"
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
			// Root span: update when the new batch found a root span
			// (RootName is only set by aggregateTraces when a root span is found).
			if trace.RootName != "" {
				existing.RootSpanID = trace.RootSpanID
				existing.RootName = trace.RootName
				existing.StatusCode = trace.StatusCode
				existing.StatusMessage = trace.StatusMessage
				if trace.SessionID != "" {
					existing.SessionID = trace.SessionID
				}
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

func (m *memStore) ListSessions(ctx context.Context, q SessionQuery) (*SessionListResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Group traces by session_id.
	type agg struct {
		traceCount      int
		totalTokens     uint32
		hasTokens       bool
		totalDurationMS uint64
		maxDurationMS   uint64
		errorCount      int
		firstActiveMS   uint64
		lastActiveMS    uint64
	}
	groups := make(map[string]*agg)

	for _, t := range m.traces {
		if t.SessionID == "" {
			continue
		}
		if q.Service != "" {
			if t.ResourceAttrs["service.name"] != q.Service {
				continue
			}
		}
		if q.Query != "" {
			if !containsSubstring(t.SessionID, q.Query) {
				continue
			}
		}
		if q.StartTimeMS > 0 && t.StartTimeMS < q.StartTimeMS {
			continue
		}
		if q.EndTimeMS > 0 && t.StartTimeMS > q.EndTimeMS {
			continue
		}

		g, ok := groups[t.SessionID]
		if !ok {
			g = &agg{firstActiveMS: t.StartTimeMS, lastActiveMS: t.StartTimeMS}
			groups[t.SessionID] = g
		}
		g.traceCount++
		if t.TotalTokens != nil {
			g.totalTokens += *t.TotalTokens
			g.hasTokens = true
		}
		g.totalDurationMS += t.DurationMS
		if t.DurationMS > g.maxDurationMS {
			g.maxDurationMS = t.DurationMS
		}
		if t.StatusCode == 2 { // ERROR
			g.errorCount++
		}
		if t.StartTimeMS < g.firstActiveMS {
			g.firstActiveMS = t.StartTimeMS
		}
		if t.StartTimeMS > g.lastActiveMS {
			g.lastActiveMS = t.StartTimeMS
		}
	}

	// Convert to slice and sort by last_active_ms descending.
	type sessionEntry struct {
		id  string
		agg *agg
	}
	entries := make([]sessionEntry, 0, len(groups))
	for id, g := range groups {
		entries = append(entries, sessionEntry{id, g})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].agg.lastActiveMS > entries[j].agg.lastActiveMS
	})

	total := len(entries)

	// Paginate.
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	page := entries[start:end]

	items := make([]SessionListItem, len(page))
	for i, e := range page {
		item := SessionListItem{
			SessionID:       e.id,
			TraceCount:      e.agg.traceCount,
			TotalDurationMS: e.agg.totalDurationMS,
			MaxDurationMS:   e.agg.maxDurationMS,
			ErrorCount:      e.agg.errorCount,
			FirstActiveMS:   e.agg.firstActiveMS,
			LastActiveMS:    e.agg.lastActiveMS,
		}
		if e.agg.hasTokens {
			tok := e.agg.totalTokens
			item.TotalTokens = &tok
		}
		if e.agg.traceCount > 0 {
			item.AvgDurationMS = float64(e.agg.totalDurationMS) / float64(e.agg.traceCount)
			item.ErrorRate = float64(e.agg.errorCount) / float64(e.agg.traceCount)
		}
		items[i] = item
	}

	return &SessionListResult{
		Sessions: items,
		Pagination: Pagination{
			Page:     q.Page,
			PageSize: q.PageSize,
			Total:    total,
		},
	}, nil
}

func (m *memStore) GetSession(ctx context.Context, sessionID string) (*SessionDetail, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect traces for this session.
	var sessionTraces []Trace
	for _, t := range m.traces {
		if t.SessionID == sessionID {
			sessionTraces = append(sessionTraces, t)
		}
	}

	if len(sessionTraces) == 0 {
		return nil, nil
	}

	// Sort by start_time_ms ascending.
	sort.Slice(sessionTraces, func(i, j int) bool {
		return sessionTraces[i].StartTimeMS < sessionTraces[j].StartTimeMS
	})

	// Build session summary.
	var totalTokens uint32
	var hasTokens bool
	var totalDurationMS, maxDurationMS uint64
	var errorCount int
	firstActiveMS := sessionTraces[0].StartTimeMS
	lastActiveMS := sessionTraces[0].StartTimeMS

	traces := make([]TraceListItem, len(sessionTraces))
	for i, t := range sessionTraces {
		traces[i] = TraceListItem{
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
		if t.TotalTokens != nil {
			totalTokens += *t.TotalTokens
			hasTokens = true
		}
		totalDurationMS += t.DurationMS
		if t.DurationMS > maxDurationMS {
			maxDurationMS = t.DurationMS
		}
		if t.StatusCode == 2 {
			errorCount++
		}
		if t.StartTimeMS < firstActiveMS {
			firstActiveMS = t.StartTimeMS
		}
		if t.StartTimeMS > lastActiveMS {
			lastActiveMS = t.StartTimeMS
		}
	}

	summary := SessionListItem{
		SessionID:       sessionID,
		TraceCount:      len(sessionTraces),
		TotalDurationMS: totalDurationMS,
		MaxDurationMS:   maxDurationMS,
		ErrorCount:      errorCount,
		FirstActiveMS:   firstActiveMS,
		LastActiveMS:    lastActiveMS,
	}
	if hasTokens {
		summary.TotalTokens = &totalTokens
	}
	if len(sessionTraces) > 0 {
		summary.AvgDurationMS = float64(totalDurationMS) / float64(len(sessionTraces))
		summary.ErrorRate = float64(errorCount) / float64(len(sessionTraces))
	}

	return &SessionDetail{
		Session: summary,
		Traces:  traces,
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

func (m *memStore) Purge(ctx context.Context, maxAge time.Duration, maxCount int) (int, int, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()

	now := uint64(time.Now().UnixMilli())
	cutoffMS := uint64(0)
	if maxAge > 0 {
		cutoffMS = now - uint64(maxAge.Milliseconds())
	}

	deletedTraces := 0
	deletedSpans := 0

	// Phase 1: collect trace IDs to keep based on age.
	keepTraces := make(map[[16]byte]bool)
	for traceID, trace := range m.traces {
		if cutoffMS > 0 && trace.StartTimeMS < cutoffMS {
			continue // too old, skip
		}
		keepTraces[traceID] = true
	}

	// Phase 2: if maxCount > 0, further restrict to newest maxCount traces.
	if maxCount > 0 && len(keepTraces) > maxCount {
		type timedTrace struct {
			id    [16]byte
			start uint64
		}
		sorted := make([]timedTrace, 0, len(keepTraces))
		for id := range keepTraces {
			sorted = append(sorted, timedTrace{id, m.traces[id].StartTimeMS})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].start > sorted[j].start
		})
		keepTraces = make(map[[16]byte]bool)
		for _, tv := range sorted[:maxCount] {
			keepTraces[tv.id] = true
		}
	}

	// Delete traces not in keepTraces.
	for id := range m.traces {
		if !keepTraces[id] {
			delete(m.traces, id)
			deletedTraces++
		}
	}

	// Delete spans belonging to deleted traces.
	newSpans := make([]Span, 0, len(m.spans))
	for _, s := range m.spans {
		if keepTraces[s.TraceID] {
			newSpans = append(newSpans, s)
		} else {
			deletedSpans++
		}
	}
	m.spans = newSpans

	return deletedTraces, deletedSpans, nil
}

func (m *memStore) Close() error {
	return nil
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
