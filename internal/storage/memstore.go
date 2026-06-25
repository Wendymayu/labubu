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

	// Sort by timestamp descending.
	sort.Slice(filtered, func(i, j int) bool {
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

	// Delete logs belonging to deleted traces.
	newLogs := make([]LogRecord, 0, len(m.logs))
	for _, l := range m.logs {
		if keepTraces[l.TraceID] {
			newLogs = append(newLogs, l)
		}
	}
	m.logs = newLogs

	return deletedTraces, deletedSpans, nil
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

// computeAgentStats calculates agent behavior statistics from traces and spans.
func computeAgentStats(sessionTraces []Trace, allSpans []Span) *AgentStats {
	// Trace success rate: ok traces / total traces.
	okCount := 0
	for _, t := range sessionTraces {
		if StatusCodeToString(t.StatusCode) == "OK" {
			okCount++
		}
	}
	traceSuccessRate := float64(okCount) / float64(len(sessionTraces))

	// Span-per-trace.
	totalSpans := len(allSpans)
	spanPerTrace := float64(0)
	if len(sessionTraces) > 0 {
		spanPerTrace = float64(totalSpans) / float64(len(sessionTraces))
	}

	// Identify tool spans and LLM spans.
	var toolSpans []Span
	for _, s := range allSpans {
		if s.Attributes["gen_ai.tool.name"] != "" {
			toolSpans = append(toolSpans, s)
		}
	}

	// Sort tool spans by StartTimeMS.
	sort.Slice(toolSpans, func(i, j int) bool {
		return toolSpans[i].StartTimeMS < toolSpans[j].StartTimeMS
	})

	// Total and successful tool calls.
	totalToolCalls := len(toolSpans)
	successfulToolCalls := 0
	for _, s := range toolSpans {
		if StatusCodeToString(s.StatusCode) == "OK" {
			successfulToolCalls++
		}
	}

	avgToolSuccessRate := float64(0)
	if totalToolCalls > 0 {
		avgToolSuccessRate = float64(successfulToolCalls) / float64(totalToolCalls)
	}

	// Per-tool statistics.
	type toolAgg struct {
		callCount   int
		successCount int
		spans       []Span
	}
	toolAggs := make(map[string]*toolAgg)
	for _, s := range toolSpans {
		name := s.Attributes["gen_ai.tool.name"]
		agg, ok := toolAggs[name]
		if !ok {
			agg = &toolAgg{}
			toolAggs[name] = agg
		}
		agg.callCount++
		if StatusCodeToString(s.StatusCode) == "OK" {
			agg.successCount++
		}
		agg.spans = append(agg.spans, s)
	}

	// Retry detection per tool: consecutive error spans followed by 1 ok span = 1 retry group.
	// Retry count = number of error spans in the group.
	// Also track max loop: consecutive same-tool-name spans in the overall time-ordered tool spans.
	totalRetries := 0
	for _, agg := range toolAggs {
		consecutiveErrors := 0
		for _, s := range agg.spans {
			if StatusCodeToString(s.StatusCode) == "ERROR" {
				consecutiveErrors++
			} else if StatusCodeToString(s.StatusCode) == "OK" && consecutiveErrors > 0 {
				totalRetries += consecutiveErrors
				consecutiveErrors = 0
			} else {
				consecutiveErrors = 0
			}
		}
	}

	avgRetries := float64(0)
	if len(toolAggs) > 0 {
		avgRetries = float64(totalRetries) / float64(len(toolAggs))
	}

	// Loop detection: max consecutive same tool_name spans in time-ordered tool spans.
	globalMaxLoop := 0
	if len(toolSpans) > 0 {
		currentLoop := 1
		for i := 1; i < len(toolSpans); i++ {
			if toolSpans[i].Attributes["gen_ai.tool.name"] == toolSpans[i-1].Attributes["gen_ai.tool.name"] {
				currentLoop++
			} else {
				if currentLoop > globalMaxLoop {
					globalMaxLoop = currentLoop
				}
				currentLoop = 1
			}
		}
		if currentLoop > globalMaxLoop {
			globalMaxLoop = currentLoop
		}
	}

	// Per-tool max loop.
	toolMaxLoops := make(map[string]int)
	if len(toolSpans) > 0 {
		currentName := toolSpans[0].Attributes["gen_ai.tool.name"]
		currentCount := 1
		for i := 1; i < len(toolSpans); i++ {
			name := toolSpans[i].Attributes["gen_ai.tool.name"]
			if name == currentName {
				currentCount++
			} else {
				if currentCount > toolMaxLoops[currentName] {
					toolMaxLoops[currentName] = currentCount
				}
				currentName = name
				currentCount = 1
			}
		}
		if currentCount > toolMaxLoops[currentName] {
			toolMaxLoops[currentName] = currentCount
		}
	}

	// Avg loop depth across all tool types.
	totalLoopDepth := 0
	for _, depth := range toolMaxLoops {
		totalLoopDepth += depth
	}
	avgLoopDepth := float64(0)
	if len(toolMaxLoops) > 0 {
		avgLoopDepth = float64(totalLoopDepth) / float64(len(toolMaxLoops))
	}

	// Per-tool retry count for avg_retries per tool.
	toolRetryCounts := make(map[string]int)
	for name, agg := range toolAggs {
		consecutiveErrors := 0
		for _, s := range agg.spans {
			if StatusCodeToString(s.StatusCode) == "ERROR" {
				consecutiveErrors++
			} else if StatusCodeToString(s.StatusCode) == "OK" && consecutiveErrors > 0 {
				toolRetryCounts[name] += consecutiveErrors
				consecutiveErrors = 0
			} else {
				consecutiveErrors = 0
			}
		}
	}

	// Avg retries across all tool types.
	sumRetries := 0
	for _, rc := range toolRetryCounts {
		sumRetries += rc
	}
	if len(toolRetryCounts) > 0 {
		avgRetries = float64(sumRetries) / float64(len(toolRetryCounts))
	}

	// Build tool usage items, sorted by call count descending.
	toolUsage := make([]ToolUsageItem, 0, len(toolAggs))
	for name, agg := range toolAggs {
		successRate := float64(0)
		if agg.callCount > 0 {
			successRate = float64(agg.successCount) / float64(agg.callCount)
		}
		toolUsage = append(toolUsage, ToolUsageItem{
			ToolName:    name,
			CallCount:   agg.callCount,
			SuccessRate: successRate,
			AvgRetries:  float64(toolRetryCounts[name]),
			MaxLoop:     toolMaxLoops[name],
		})
	}
	sort.Slice(toolUsage, func(i, j int) bool {
		return toolUsage[i].CallCount > toolUsage[j].CallCount
	})

	stats := &AgentStats{
		TraceSuccessRate:    traceSuccessRate,
		AvgToolSuccessRate:  avgToolSuccessRate,
		AvgRetries:          avgRetries,
		AvgLoopDepth:        avgLoopDepth,
		MaxLoopDepth:        globalMaxLoop,
		SpanPerTrace:        spanPerTrace,
		TotalToolCalls:      totalToolCalls,
		SuccessfulToolCalls: successfulToolCalls,
		ToolUsage:           toolUsage,
	}

	stats.Insights = generateInsights(stats)

	return stats
}

// generateInsights produces actionable insights from agent stats.
func generateInsights(stats *AgentStats) []string {
	var insights []string
	for _, item := range stats.ToolUsage {
		if item.MaxLoop >= 3 {
			insights = append(insights, fmt.Sprintf("%s has max loop depth %d — agent may be stuck in a retry loop", item.ToolName, item.MaxLoop))
		}
	}
	for _, item := range stats.ToolUsage {
		if item.SuccessRate < 0.8 && item.CallCount >= 3 {
			insights = append(insights, fmt.Sprintf("%s has low success rate (%d%%) — consider adding fallback logic", item.ToolName, int(item.SuccessRate*100)))
		}
	}
	if stats.TraceSuccessRate < 0.7 && stats.TraceSuccessRate > 0 {
		insights = append(insights, fmt.Sprintf("Over %d%% of traces failed — agent configuration may need adjustment", int((1-stats.TraceSuccessRate)*100)))
	}
	if stats.AvgRetries > 1.0 {
		insights = append(insights, fmt.Sprintf("High average retry count (%.1f) — tool calls frequently fail on first attempt", stats.AvgRetries))
	}
	if len(insights) > 4 {
		insights = insights[:4]
	}
	return insights
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
