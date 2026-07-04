//go:build !local_engine && nosqlite

// Package storage provides an in-memory Store implementation for non-CGO builds
// with nosqlite tag. By default, non-CGO builds use SQLite Store instead.
// This memStore is kept as a minimal fallback when SQLite is explicitly disabled.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// memStore is an in-memory Store used when chDB CGO is not available.
// LLM configs and diagnosis results are persisted to a JSON file on disk.
type memStore struct {
	mu       sync.RWMutex
	spans    []Span
	traces   map[[16]byte]Trace
	services map[string]bool
	logs     []LogRecord
	pricing    map[string]ModelPricing
	llmConfigs map[string]LLMConfig
	diagnosisResults map[[16]byte]*DiagnosisResult
	jsonPath  string // path to persistence file; "" means no persistence
}

// NewChDBStore returns an in-memory Store when chDB is not available.
// If dataDir is provided, LLM configs and diagnosis results are persisted
// to a JSON file under that directory. Otherwise, data is lost on restart.
func NewChDBStore(dataDir string) (Store, error) {
	jsonPath := ""
	if dataDir != "" {
		// Ensure directory exists.
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
		jsonPath = dataDir + "/memstore.json"
	}
	m := &memStore{
		spans:            make([]Span, 0),
		traces:           make(map[[16]byte]Trace),
		services:         make(map[string]bool),
		logs:             make([]LogRecord, 0),
		pricing:          make(map[string]ModelPricing),
		llmConfigs:       make(map[string]LLMConfig),
		diagnosisResults: make(map[[16]byte]*DiagnosisResult),
		jsonPath:         jsonPath,
	}
	if jsonPath != "" {
		if err := m.loadFromDisk(); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("load persisted data: %w", err)
		}
	}
	return m, nil
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

	// Calculate costs inline for traces with token data (lock already held).
	// Set per-span Cost on stored spans and aggregate trace cost.
	for traceID := range traceMap {
		merged, ok := m.traces[traceID]
		if !ok || merged.TotalTokens == nil || *merged.TotalTokens == 0 {
			continue
		}
		var totalCost float64
		var currency string
		hasCost := false
		for i := range m.spans {
			s := &m.spans[i]
			if s.TraceID != traceID {
				continue
			}
			if s.TotalTokens == nil || *s.TotalTokens == 0 || s.GenAIRequestModel == nil || *s.GenAIRequestModel == "" {
				continue
			}
			for _, p := range m.pricing {
				if p.ModelName == *s.GenAIRequestModel {
					inputT, outputT, ccT, crT := 0.0, 0.0, 0.0, 0.0
					if s.InputTokens != nil {
						inputT = float64(*s.InputTokens)
					}
					if s.OutputTokens != nil {
						outputT = float64(*s.OutputTokens)
					}
					if s.CacheCreationTokens != nil {
						ccT = float64(*s.CacheCreationTokens)
					}
					if s.CacheReadTokens != nil {
						crT = float64(*s.CacheReadTokens)
					}
					spanCost := (inputT*p.InputPrice+ccT*p.InputPrice*1.25+
						crT*p.InputPrice*0.1+outputT*p.OutputPrice) / 1_000_000.0
					spanCost = math.Round(spanCost*1e6) / 1e6
					s.Cost = &spanCost
					s.CostCurrency = p.Currency
					totalCost += spanCost
					hasCost = true
					if currency == "" {
						currency = p.Currency
					}
					break
				}
			}
		}
		if hasCost {
			merged.Cost = &totalCost
			merged.CostCurrency = currency
			m.traces[traceID] = merged
		}
	}

	return nil
}

func (m *memStore) InsertLogs(ctx context.Context, logs []LogRecord) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, logs...)
	return nil
}

func (m *memStore) ListLogs(ctx context.Context, q LogQuery) (*LogListResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Filter.
	filtered := make([]LogRecord, 0, len(m.logs))
	for _, l := range m.logs {
		if q.Severity != "" && !strings.EqualFold(l.Severity, q.Severity) {
			continue
		}
		if q.EventName != "" && l.EventName != q.EventName {
			continue
		}
		if q.Query != "" && !containsSubstring(l.Body, q.Query) {
			continue
		}
		var zeroTrace [16]byte
		if q.TraceID != zeroTrace && l.TraceID != q.TraceID {
			continue
		}
		var zeroSpan [8]byte
		if q.SpanID != zeroSpan && l.SpanID != q.SpanID {
			continue
		}
		if q.StartTime > 0 && l.Timestamp < q.StartTime {
			continue
		}
		if q.EndTime > 0 && l.Timestamp > q.EndTime {
			continue
		}
		filtered = append(filtered, l)
	}

	// Sort by timestamp (descending by default; ascending when Asc is set).
	sort.Slice(filtered, func(i, j int) bool {
		if q.Asc {
			return filtered[i].Timestamp < filtered[j].Timestamp
		}
		return filtered[i].Timestamp > filtered[j].Timestamp
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
		page = make([]LogRecord, 0)
	}

	items := make([]LogListItem, len(page))
	for i, l := range page {
		items[i] = LogListItem{
			TraceIDHex: TraceIDToHex(l.TraceID),
			SpanIDHex:  SpanIDToHex(l.SpanID),
			Timestamp:  l.Timestamp,
			Severity:   l.Severity,
			EventName:  l.EventName,
			Body:       l.Body,
			Attributes: l.Attributes,
		}
	}

	return &LogListResult{
		Logs: items,
		Pagination: Pagination{
			Page:     q.Page,
			PageSize: q.PageSize,
			Total:    total,
		},
	}, nil
}

func (m *memStore) GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]LogListItem, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	var items []LogListItem
	for _, l := range m.logs {
		if l.TraceID == traceID {
			items = append(items, LogListItem{
				TraceIDHex: TraceIDToHex(l.TraceID),
				SpanIDHex:  SpanIDToHex(l.SpanID),
				Timestamp:  l.Timestamp,
				Severity:   l.Severity,
				EventName:  l.EventName,
				Body:       l.Body,
				Attributes: l.Attributes,
			})
		}
	}

	// Sort by timestamp ascending.
	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp < items[j].Timestamp
	})

	return items, nil
}

// GetLogCountsByTrace returns the per-span log count for a trace.
func (m *memStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[string]int)
	for _, l := range m.logs {
		if l.TraceID == traceID {
			counts[SpanIDToHex(l.SpanID)]++
		}
	}
	return counts, nil
}

func (m *memStore) GetLogEventNames(ctx context.Context) ([]string, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[string]bool)
	for _, l := range m.logs {
		if l.EventName != "" && !seen[l.EventName] {
			seen[l.EventName] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
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
		if q.MinSpanCount > 0 && t.SpanCount < q.MinSpanCount {
			continue
		}
		if q.MaxSpanCount > 0 && t.SpanCount > q.MaxSpanCount {
			continue
		}
		if q.MinCost > 0 && (t.Cost == nil || *t.Cost < q.MinCost) {
			continue
		}
		if q.MaxCost > 0 && (t.Cost == nil || *t.Cost > q.MaxCost) {
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
			TraceIDHex:   TraceIDToHex(t.TraceID),
			RootSpanID:   SpanIDToHex(t.RootSpanID),
			RootName:     t.RootName,
			RootService:  t.ResourceAttrs["service.name"],
			StartTimeMS:  t.StartTimeMS,
			DurationMS:   t.DurationMS,
			SpanCount:    t.SpanCount,
			Status:       StatusCodeToString(t.StatusCode),
			TotalTokens:  t.TotalTokens,
			Cost:         t.Cost,
			CostCurrency: t.CostCurrency,
		}
	}

	// Attach gen_ai.input.messages from each trace's root span.
	if len(items) > 0 {
		idxByTid := make(map[string]int, len(items))
		for i := range items {
			idxByTid[items[i].TraceIDHex] = i
		}
		for i := range m.spans {
			sp := &m.spans[i]
			if !isRootSpan(sp.ParentSpanID) {
				continue
			}
			idx, ok := idxByTid[TraceIDToHex(sp.TraceID)]
			if !ok {
				continue
			}
			if v, ok := sp.Attributes["gen_ai.input.messages"]; ok && v != "" {
				vv := v
				items[idx].InputMessages = &vv
			}
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
			CacheCreationTokens: s.CacheCreationTokens,
			CacheReadTokens:   s.CacheReadTokens,
			GenAIRequestModel: s.GenAIRequestModel,
		}

		// Extract GenAI semantic attributes.
		if s.Attributes != nil {
			if v, ok := s.Attributes["gen_ai.system"]; ok {
				detailSpans[i].GenAISystem = &v
			}
			if v, ok := s.Attributes["gen_ai.tool.name"]; ok {
				detailSpans[i].ToolName = &v
				detailSpans[i].IsToolCall = true
			}
		}

		if s.ParentSpanID == [8]byte{} {
			rootSpanID = s.SpanID
			rootIdx = i
		}
	}

	// Use the root span's attributes as resource attrs if available.
	resourceAttrs := make(map[string]string)
	sessionID := ""
	if trace, ok := m.traces[traceID]; ok {
		resourceAttrs = trace.ResourceAttrs
		sessionID = trace.SessionID
	}

	return &TraceDetail{
		TraceIDHex:    TraceIDToHex(traceID),
		RootSpanID:    SpanIDToHex(rootSpanID),
		SpanCount:     len(traceSpans),
		StartTimeMS:   detailSpans[rootIdx].StartTimeMS,
		DurationMS:    detailSpans[rootIdx].DurationMS,
		ResourceAttrs: resourceAttrs,
		SessionID:     sessionID,
		Spans:         detailSpans,
	}, nil
}

func (m *memStore) ListSessions(ctx context.Context, q SessionQuery) (*SessionListResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// The filter only gates WHICH sessions appear (sessions with at least one
	// matching trace); aggregates run over the whole session so they match
	// GetSession. See memstore_session_list_count_test.
	// First pass: collect session IDs that have a matching trace.
	matched := make(map[string]bool)
	for _, t := range m.traces {
		if t.SessionID == "" {
			continue
		}
		if q.Service != "" && t.ResourceAttrs["service.name"] != q.Service {
			continue
		}
		if q.Query != "" && !containsSubstring(t.SessionID, q.Query) {
			continue
		}
		if q.StartTimeMS > 0 && t.StartTimeMS < q.StartTimeMS {
			continue
		}
		if q.EndTimeMS > 0 && t.StartTimeMS > q.EndTimeMS {
			continue
		}
		matched[t.SessionID] = true
	}

	// Second pass: aggregate ALL traces for matched sessions (unfiltered).
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
		if !matched[t.SessionID] { // includes empty session IDs (never matched)
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

func (m *memStore) GetSession(ctx context.Context, sessionID string, page, pageSize int) (*SessionDetail, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

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
			TraceIDHex:   TraceIDToHex(t.TraceID),
			RootSpanID:   SpanIDToHex(t.RootSpanID),
			RootName:     t.RootName,
			RootService:  t.ResourceAttrs["service.name"],
			StartTimeMS:  t.StartTimeMS,
			DurationMS:   t.DurationMS,
			SpanCount:    t.SpanCount,
			Status:       StatusCodeToString(t.StatusCode),
			TotalTokens:  t.TotalTokens,
			Cost:         t.Cost,
			CostCurrency: t.CostCurrency,
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

	// Paginate the traces (already sorted ascending by start_time_ms).
	total := len(traces)
	offset := (page - 1) * pageSize
	if offset > total {
		offset = total
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	pageTraces := traces[offset:end]
	if pageTraces == nil {
		pageTraces = []TraceListItem{}
	}

	// Attach gen_ai.input.messages from each trace's root span.
	if len(pageTraces) > 0 {
		idxByTid := make(map[string]int, len(pageTraces))
		for i := range pageTraces {
			idxByTid[pageTraces[i].TraceIDHex] = i
		}
		for i := range m.spans {
			sp := &m.spans[i]
			if !isRootSpan(sp.ParentSpanID) {
				continue
			}
			idx, ok := idxByTid[TraceIDToHex(sp.TraceID)]
			if !ok {
				continue
			}
			if v, ok := sp.Attributes["gen_ai.input.messages"]; ok && v != "" {
				vv := v
				pageTraces[idx].InputMessages = &vv
			}
		}
	}

	return &SessionDetail{
		Session:    summary,
		Traces:     pageTraces,
		Pagination: Pagination{Page: page, PageSize: pageSize, Total: total},
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
	// (Logs are age-purged separately via PurgeLogs.)
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

func (m *memStore) PurgeLogs(ctx context.Context, maxAge time.Duration) (int, error) {
	_ = ctx
	if maxAge <= 0 {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := uint64(time.Now().UnixMilli()) - uint64(maxAge.Milliseconds())
	deleted := 0
	newLogs := make([]LogRecord, 0, len(m.logs))
	for _, l := range m.logs {
		if l.Timestamp < cutoff {
			deleted++
		} else {
			newLogs = append(newLogs, l)
		}
	}
	m.logs = newLogs
	return deleted, nil
}

func (m *memStore) DeleteTraces(ctx context.Context, traceIDs [][16]byte) (int, int, error) {
	_ = ctx
	if len(traceIDs) == 0 {
		return 0, 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build a set of IDs to delete (avoid O(n*m) lookups).
	toDelete := make(map[[16]byte]bool, len(traceIDs))
	for _, id := range traceIDs {
		toDelete[id] = true
	}

	deletedTraces := 0
	for id := range m.traces {
		if toDelete[id] {
			delete(m.traces, id)
			deletedTraces++
		}
	}

	deletedLogs := 0
	newLogs := make([]LogRecord, 0, len(m.logs))
	for _, l := range m.logs {
		if toDelete[l.TraceID] {
			deletedLogs++
		} else {
			newLogs = append(newLogs, l)
		}
	}
	m.logs = newLogs

	newSpans := make([]Span, 0, len(m.spans))
	for _, s := range m.spans {
		if !toDelete[s.TraceID] {
			newSpans = append(newSpans, s)
		}
	}
	m.spans = newSpans

	for id := range toDelete {
		delete(m.diagnosisResults, id)
	}

	if err := m.saveToDiskLocked(); err != nil {
		return deletedTraces, deletedLogs, err
	}
	return deletedTraces, deletedLogs, nil
}

func (m *memStore) GetModelPricing(ctx context.Context) ([]ModelPricing, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ModelPricing, 0, len(m.pricing))
	for _, p := range m.pricing {
		result = append(result, p)
	}
	return result, nil
}

func (m *memStore) UpsertModelPricing(ctx context.Context, p ModelPricing) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pricing == nil {
		m.pricing = make(map[string]ModelPricing)
	}
	m.pricing[p.ModelName] = p
	return m.saveToDiskLocked()
}

func (m *memStore) DeleteModelPricing(ctx context.Context, modelName string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pricing, modelName)
	return m.saveToDiskLocked()
}

func (m *memStore) GetLLMConfigs(ctx context.Context) ([]LLMConfig, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]LLMConfig, 0, len(m.llmConfigs))
	for _, c := range m.llmConfigs {
		result = append(result, c)
	}
	return result, nil
}

func (m *memStore) CreateLLMConfig(ctx context.Context, c *LLMConfig) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.llmConfigs == nil {
		m.llmConfigs = make(map[string]LLMConfig)
	}
	if c.IsDefault {
		for k, v := range m.llmConfigs {
			v.IsDefault = false
			m.llmConfigs[k] = v
		}
	}
	m.llmConfigs[c.ID] = *c
	return m.saveToDiskLocked()
}

func (m *memStore) UpdateLLMConfig(ctx context.Context, c *LLMConfig) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.llmConfigs[c.ID]
	if !ok {
		return fmt.Errorf("llm config not found: %s", c.ID)
	}
	// If api_key is masked sentinel, keep the existing key.
	if strings.Contains(c.APIKey, "***") {
		c.APIKey = existing.APIKey
	}
	if c.IsDefault {
		for k, v := range m.llmConfigs {
			v.IsDefault = false
			m.llmConfigs[k] = v
		}
	}
	m.llmConfigs[c.ID] = *c
	return m.saveToDiskLocked()
}

func (m *memStore) DeleteLLMConfig(ctx context.Context, id string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.llmConfigs, id)
	return m.saveToDiskLocked()
}

func (m *memStore) UpdateTraceCost(ctx context.Context, traceID [16]byte) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()

	trace, ok := m.traces[traceID]
	if !ok {
		return nil
	}

	// Collect spans for this trace.
	var traceSpans []Span
	for _, s := range m.spans {
		if s.TraceID == traceID {
			traceSpans = append(traceSpans, s)
		}
	}

	// Get pricing table.
	pricingList := make([]ModelPricing, 0, len(m.pricing))
	for _, p := range m.pricing {
		pricingList = append(pricingList, p)
	}

	// Calculate cost.
	var totalCost float64
	var currency string
	hasCost := false
	for _, span := range traceSpans {
		if span.TotalTokens == nil || *span.TotalTokens == 0 {
			continue
		}
		if span.GenAIRequestModel == nil || *span.GenAIRequestModel == "" {
			continue
		}
		for _, p := range pricingList {
			if p.ModelName == *span.GenAIRequestModel {
				inputT := float64(0)
				outputT := float64(0)
				cacheCreateT := float64(0)
				cacheReadT := float64(0)
				if span.InputTokens != nil {
					inputT = float64(*span.InputTokens)
				}
				if span.OutputTokens != nil {
					outputT = float64(*span.OutputTokens)
				}
				if span.CacheCreationTokens != nil {
					cacheCreateT = float64(*span.CacheCreationTokens)
				}
				if span.CacheReadTokens != nil {
					cacheReadT = float64(*span.CacheReadTokens)
				}
				// Anthropic prompt-caching differential rates.
				c := (inputT*p.InputPrice +
					cacheCreateT*p.InputPrice*1.25 +
					cacheReadT*p.InputPrice*0.1 +
					outputT*p.OutputPrice) / 1_000_000.0
				totalCost += c
				hasCost = true
				if currency == "" {
					currency = p.Currency
				}
				break
			}
		}
	}

	if hasCost {
		trace.Cost = &totalCost
		trace.CostCurrency = currency
		m.traces[traceID] = trace
	}
	return nil
}

func (m *memStore) GetCostSummary(ctx context.Context, q CostQuery) (*CostSummaryResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Traces in range with cost>0.
	costlyTraces := make(map[[16]byte]struct{})
	var totalCost float64
	var currency string
	for tid, t := range m.traces {
		if q.StartTimeMS > 0 && t.StartTimeMS < q.StartTimeMS {
			continue
		}
		if q.EndTimeMS > 0 && t.StartTimeMS > q.EndTimeMS {
			continue
		}
		if t.Cost == nil || *t.Cost == 0 {
			continue
		}
		costlyTraces[tid] = struct{}{}
		totalCost += *t.Cost
		if currency == "" && t.CostCurrency != "" {
			currency = t.CostCurrency
		}
	}
	traceCount := len(costlyTraces)

	if q.GroupBy == "service" {
		type svcAgg struct {
			cost, input, cc, cr, output uint64
			traces                       map[[16]byte]struct{}
		}
		sagg := map[string]*svcAgg{}
		var oIn, oCC, oCR, oOut uint64

		for i := range m.spans {
			s := &m.spans[i]
			if _, ok := costlyTraces[s.TraceID]; !ok {
				continue
			}
			if s.TotalTokens == nil {
				continue
			}
			svc := "(unknown)"
			if t, ok := m.traces[s.TraceID]; ok {
				if name := t.ResourceAttrs["service.name"]; name != "" {
					svc = name
				}
			}
			entry := sagg[svc]
			if entry == nil {
				entry = &svcAgg{traces: map[[16]byte]struct{}{}}
				sagg[svc] = entry
			}
			entry.traces[s.TraceID] = struct{}{}
			if s.Cost != nil {
				entry.cost += uint64(math.Round(*s.Cost * 1e6))
			}
			if s.InputTokens != nil {
				entry.input += uint64(*s.InputTokens)
				oIn += uint64(*s.InputTokens)
			}
			if s.CacheCreationTokens != nil {
				entry.cc += uint64(*s.CacheCreationTokens)
				oCC += uint64(*s.CacheCreationTokens)
			}
			if s.CacheReadTokens != nil {
				entry.cr += uint64(*s.CacheReadTokens)
				oCR += uint64(*s.CacheReadTokens)
			}
			if s.OutputTokens != nil {
				entry.output += uint64(*s.OutputTokens)
				oOut += uint64(*s.OutputTokens)
			}
		}

		byService := make([]ServiceCostItem, 0, len(sagg))
		for svc, e := range sagg {
			tc := len(e.traces)
			costF := float64(e.cost) / 1e6
			avg := 0.0
			if tc > 0 {
				avg = math.Round(costF/float64(tc)*1e6) / 1e6
			}
			byService = append(byService, ServiceCostItem{
				Service: svc, Cost: costF,
				Tokens:              e.input + e.cc + e.cr + e.output,
				InputTokens:         e.input, CacheCreationTokens: e.cc,
				CacheReadTokens:     e.cr, OutputTokens: e.output,
				TraceCount:          tc, AvgCost: avg,
			})
		}
		sort.Slice(byService, func(i, j int) bool { return byService[i].Cost > byService[j].Cost })

		avgPerTrace := 0.0
		if traceCount > 0 {
			avgPerTrace = math.Round(totalCost/float64(traceCount)*1e6) / 1e6
		}

		return &CostSummaryResult{
			Period:   "",
			Currency: currency,
			Overview: CostOverview{
				TotalCost:                totalCost,
				TotalInputTokens:         oIn,
				TotalCacheCreationTokens: oCC,
				TotalCacheReadTokens:     oCR,
				TotalOutputTokens:        oOut,
				TotalTokens:              oIn + oCC + oCR + oOut,
				AvgCostPerTrace:          avgPerTrace,
				TraceCount:               traceCount,
			},
			GroupBy:   "service",
			ByService: byService,
		}, nil
	}

	type modelAgg struct {
		cost, input, cc, cr, output uint64
		traces                       map[[16]byte]struct{}
	}
	agg := map[string]*modelAgg{}
	var oIn, oCC, oCR, oOut uint64

	for i := range m.spans {
		s := &m.spans[i]
		if _, ok := costlyTraces[s.TraceID]; !ok {
			continue
		}
		if s.TotalTokens == nil {
			continue
		}
		model := "(unknown)"
		if s.GenAIRequestModel != nil && *s.GenAIRequestModel != "" {
			model = *s.GenAIRequestModel
		}
		entry := agg[model]
		if entry == nil {
			entry = &modelAgg{traces: map[[16]byte]struct{}{}}
			agg[model] = entry
		}
		entry.traces[s.TraceID] = struct{}{}
		if s.Cost != nil {
			entry.cost += uint64(math.Round(*s.Cost * 1e6))
		}
		if s.InputTokens != nil {
			entry.input += uint64(*s.InputTokens)
			oIn += uint64(*s.InputTokens)
		}
		if s.CacheCreationTokens != nil {
			entry.cc += uint64(*s.CacheCreationTokens)
			oCC += uint64(*s.CacheCreationTokens)
		}
		if s.CacheReadTokens != nil {
			entry.cr += uint64(*s.CacheReadTokens)
			oCR += uint64(*s.CacheReadTokens)
		}
		if s.OutputTokens != nil {
			entry.output += uint64(*s.OutputTokens)
			oOut += uint64(*s.OutputTokens)
		}
	}

	byModel := make([]ModelCostItem, 0, len(agg))
	for model, e := range agg {
		tc := len(e.traces)
		var costF float64
		costF = float64(e.cost) / 1e6
		avg := 0.0
		if tc > 0 {
			avg = math.Round(costF/float64(tc)*1e6) / 1e6
		}
		byModel = append(byModel, ModelCostItem{
			Model: model, Cost: costF,
			Tokens: e.input + e.cc + e.cr + e.output,
			InputTokens: e.input, CacheCreationTokens: e.cc,
			CacheReadTokens: e.cr, OutputTokens: e.output,
			TraceCount: tc, AvgCost: avg,
		})
	}
	sort.Slice(byModel, func(i, j int) bool { return byModel[i].Cost > byModel[j].Cost })

	avgPerTrace := 0.0
	if traceCount > 0 {
		avgPerTrace = math.Round(totalCost/float64(traceCount)*1e6) / 1e6
	}

	return &CostSummaryResult{
		Period:   "",
		Currency: currency,
		Overview: CostOverview{
			TotalCost:                totalCost,
			TotalInputTokens:         oIn,
			TotalCacheCreationTokens: oCC,
			TotalCacheReadTokens:     oCR,
			TotalOutputTokens:        oOut,
			TotalTokens:              oIn + oCC + oCR + oOut,
			AvgCostPerTrace:          avgPerTrace,
			TraceCount:               traceCount,
		},
		GroupBy: "model",
		ByModel: byModel,
	}, nil
}

func (m *memStore) Close() error {
	return m.saveToDisk()
}

func (m *memStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*DiagnosisResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	result, ok := m.diagnosisResults[traceID]
	if !ok {
		return nil, nil
	}
	return result, nil
}

func (m *memStore) UpsertDiagnosisResult(ctx context.Context, result *DiagnosisResult) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diagnosisResults[result.TraceID] = result
	return m.saveToDiskLocked()
}

func (m *memStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*AgentStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect all traces for this session.
	var sessionTraces []Trace
	for _, t := range m.traces {
		if t.SessionID == sessionID {
			sessionTraces = append(sessionTraces, t)
		}
	}

	if len(sessionTraces) == 0 {
		return nil, nil
	}

	// Collect all spans for these traces.
	var allSpans []Span
	traceIDs := make(map[[16]byte]struct{})
	for _, t := range sessionTraces {
		traceIDs[t.TraceID] = struct{}{}
	}
	for _, s := range m.spans {
		if _, ok := traceIDs[s.TraceID]; ok {
			allSpans = append(allSpans, s)
		}
	}

	return computeAgentStats(sessionTraces, allSpans), nil
}

// GetSessionContextSpans returns the main agent's LLM spans for a session.
func (m *memStore) GetSessionContextSpans(ctx context.Context, sessionID string) ([]SessionContextSpan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect this session's traces and map trace_id_hex -> root_span_id_hex.
	rootByTrace := make(map[string]string)
	traceIDs := make(map[[16]byte]struct{})
	for _, t := range m.traces {
		if t.SessionID != sessionID {
			continue
		}
		traceIDs[t.TraceID] = struct{}{}
		rootByTrace[t.TraceIDHex] = SpanIDToHex(t.RootSpanID)
	}
	if len(traceIDs) == 0 {
		return nil, nil
	}

	// Collect all spans for those traces.
	var inputs []sessionSpanInput
	for _, s := range m.spans {
		if _, ok := traceIDs[s.TraceID]; !ok {
			continue
		}
		inputs = append(inputs, sessionSpanInput{
			TraceIDHex:          TraceIDToHex(s.TraceID),
			SpanIDHex:           SpanIDToHex(s.SpanID),
			ParentSpanIDHex:     SpanIDToHex(s.ParentSpanID),
			Name:                s.Name,
			StartTimeMS:         s.StartTimeMS,
			InputTokens:         s.InputTokens,
			OutputTokens:        s.OutputTokens,
			TotalTokens:         s.TotalTokens,
			CacheReadTokens:     s.CacheReadTokens,
			CacheCreationTokens: s.CacheCreationTokens,
			GenAIRequestModel:   s.GenAIRequestModel,
		})
	}

	return computeSessionContextSpans(rootByTrace, inputs), nil
}

// --- JSON file persistence for LLM configs and diagnosis results ---

// persistedMemData is the on-disk format.
type persistedMemData struct {
	ModelPricing     []ModelPricing     `json:"model_pricing"`
	LLMConfigs       []LLMConfig        `json:"llm_configs"`
	DiagnosisResults []DiagnosisResult  `json:"diagnosis_results"`
}

func (m *memStore) loadFromDisk() error {
	data, err := os.ReadFile(m.jsonPath)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var pd persistedMemData
	if err := json.Unmarshal(data, &pd); err != nil {
		return fmt.Errorf("unmarshal memstore: %w", err)
	}
	for _, p := range pd.ModelPricing {
		m.pricing[p.ModelName] = p
	}
	for _, c := range pd.LLMConfigs {
		m.llmConfigs[c.ID] = c
	}
	for _, d := range pd.DiagnosisResults {
		m.diagnosisResults[d.TraceID] = &d
	}
	return nil
}

// saveToDisk saves all persisted data to the JSON file (acquires lock).
func (m *memStore) saveToDisk() error {
	if m.jsonPath == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeJSONLocked()
}

// saveToDiskLocked saves without acquiring the lock (caller must hold m.mu).
func (m *memStore) saveToDiskLocked() error {
	if m.jsonPath == "" {
		return nil
	}
	return m.writeJSONLocked()
}

func (m *memStore) writeJSONLocked() error {
	pricing := make([]ModelPricing, 0, len(m.pricing))
	for _, p := range m.pricing {
		pricing = append(pricing, p)
	}
	configs := make([]LLMConfig, 0, len(m.llmConfigs))
	for _, c := range m.llmConfigs {
		configs = append(configs, c)
	}
	results := make([]DiagnosisResult, 0, len(m.diagnosisResults))
	for _, d := range m.diagnosisResults {
		results = append(results, *d)
	}

	pd := persistedMemData{
		ModelPricing:     pricing,
		LLMConfigs:       configs,
		DiagnosisResults: results,
	}
	data, err := json.MarshalIndent(pd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal memstore: %w", err)
	}
	return os.WriteFile(m.jsonPath, data, 0644)
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
