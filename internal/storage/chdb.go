//go:build cgo && local_engine

// Package storage provides the chDB-backed implementation via CGO.
// This file is only compiled when CGO is enabled and the local_engine
// build tag is set, isolating C dependencies from pure-Go tooling.

package storage

/*
#cgo LDFLAGS: -lchdb
#include <stdlib.h>
#include <chdb.h>
*/
import "C"
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// chDBStore implements Store backed by an embedded chDB session.
type chDBStore struct {
	mu   sync.Mutex
	conn C.chdb_conn_t
	dir  string
}

// NewChDBStore creates a new chDB-backed Store.
// dataDir is the directory for chDB persistent storage. If empty,
// an in-memory database is used (data lost on restart).
func NewChDBStore(dataDir string) (Store, error) {
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("create data dir %s: %w", dataDir, err)
		}
	}

	cPath := C.CString(dataDir)
	defer C.free(unsafe.Pointer(cPath))

	conn := C.chdb_connect(cPath)
	if conn == nil {
		return nil, fmt.Errorf("chdb_connect failed for path=%q", dataDir)
	}

	store := &chDBStore{
		conn: conn,
		dir:  dataDir,
	}

	// Run schema migration on startup.
	schemaFile := filepath.Join("internal", "storage", "schema.sql")
	schema, err := os.ReadFile(schemaFile)
	if err != nil {
		schema, err = os.ReadFile("schema.sql")
		if err != nil {
			return nil, fmt.Errorf("read schema.sql: %w (place schema.sql in working dir)", err)
		}
	}

	if err := store.execSQL(string(schema)); err != nil {
		store.Close()
		return nil, fmt.Errorf("run schema migration: %w", err)
	}

	return store, nil
}

// execSQL runs a SQL statement and returns an error if it fails.
func (s *chDBStore) execSQL(sql string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cSQL := C.CString(sql)
	defer C.free(unsafe.Pointer(cSQL))

	result := C.chdb_query(s.conn, cSQL)
	if result == nil {
		return fmt.Errorf("chdb_query returned null")
	}

	if result.error_message != nil {
		errMsg := C.GoString(result.error_message)
		C.chdb_free_result(result)
		return fmt.Errorf("chdb error: %s", errMsg)
	}

	C.chdb_free_result(result)
	return nil
}

// querySQL runs a SQL statement and returns the result data as a string.
func (s *chDBStore) querySQL(sql string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cSQL := C.CString(sql)
	defer C.free(unsafe.Pointer(cSQL))

	result := C.chdb_query(s.conn, cSQL)
	if result == nil {
		return "", fmt.Errorf("chdb_query returned null")
	}
	defer C.chdb_free_result(result)

	if result.error_message != nil {
		return "", fmt.Errorf("chdb error: %s", C.GoString(result.error_message))
	}

	if result.data == nil {
		return "", nil
	}

	return C.GoStringN(result.data, C.int(result.data_size)), nil
}

// InsertSpans writes spans and aggregates trace data.
func (s *chDBStore) InsertSpans(ctx context.Context, resource ResourceInfo, scope ScopeInfo, spans []Span) error {
	if len(spans) == 0 {
		return nil
	}

	for _, span := range spans {
		sql := buildInsertSpanSQL(span)
		if err := s.execSQL(sql); err != nil {
			return fmt.Errorf("insert span %x: %w", span.SpanID, err)
		}
	}

	traceMap := aggregateTraces(resource, scope, spans)
	for _, trace := range traceMap {
		sql := buildUpsertTraceSQL(trace)
		if err := s.execSQL(sql); err != nil {
			return fmt.Errorf("upsert trace %s: %w", trace.TraceIDHex, err)
		}
		// Trigger async cost calculation for each trace that has token data.
		if trace.TotalTokens != nil && *trace.TotalTokens > 0 {
			go func(tid [16]byte) {
				if err := s.UpdateTraceCost(context.Background(), tid); err != nil {
					// Log but don't fail the insert — cost is best-effort.
					_ = err
				}
			}(trace.TraceID)
		}
	}

	return nil
}

// InsertLogs writes log records to the logs table.
func (s *chDBStore) InsertLogs(ctx context.Context, logs []LogRecord) error {
	if len(logs) == 0 {
		return nil
	}
	for _, l := range logs {
		sql := buildInsertLogSQL(l)
		if err := s.execSQL(sql); err != nil {
			return fmt.Errorf("insert log: %w", err)
		}
	}
	return nil
}

// ListLogs returns a paginated list of log records.
func (s *chDBStore) ListLogs(ctx context.Context, q LogQuery) (*LogListResult, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}

	countSQL := buildLogCountSQL(q)
	countResult, err := s.querySQL(countSQL + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("count logs: %w", err)
	}
	total := parseCount(countResult)

	dataSQL := buildLogListSQL(q)
	dataResult, err := s.querySQL(dataSQL + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("list logs: %w", err)
	}

	logs, err := parseLogListItems(dataResult)
	if err != nil {
		return nil, fmt.Errorf("parse log list: %w", err)
	}

	return &LogListResult{
		Logs: logs,
		Pagination: Pagination{
			Page:     q.Page,
			PageSize: q.PageSize,
			Total:    total,
		},
	}, nil
}

// GetLogsByTrace returns all log records for a given trace.
func (s *chDBStore) GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]LogListItem, error) {
	sql := buildGetLogsByTraceSQL(traceID) + " FORMAT JSONEachRow"
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get logs by trace: %w", err)
	}
	return parseLogListItems(result)
}

// GetLogEventNames returns distinct event_name values.
func (s *chDBStore) GetLogEventNames(ctx context.Context) ([]string, error) {
	sql := buildLogEventNamesSQL() + " FORMAT JSONEachRow"
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get log event names: %w", err)
	}
	return parseLogEventNames(result)
}

// ListTraces returns a paginated list of trace summaries.
func (s *chDBStore) ListTraces(ctx context.Context, q TraceQuery) (*TraceListResult, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}

	countSQL := buildTraceCountSQL(q)
	countResult, err := s.querySQL(countSQL + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("count traces: %w", err)
	}
	total := parseCount(countResult)

	dataSQL := buildTraceListSQL(q)
	dataResult, err := s.querySQL(dataSQL + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("list traces: %w", err)
	}

	traces, err := parseTraceListItems(dataResult)
	if err != nil {
		return nil, fmt.Errorf("parse trace list: %w", err)
	}

	return &TraceListResult{
		Traces: traces,
		Pagination: Pagination{
			Page:     q.Page,
			PageSize: q.PageSize,
			Total:    total,
		},
	}, nil
}

// GetTrace returns all spans for a trace.
func (s *chDBStore) GetTrace(ctx context.Context, traceID [16]byte) (*TraceDetail, error) {
	traceIDHex := fmt.Sprintf("%x", traceID)

	// Query spans.
	spansSQL := buildGetTraceSQL(traceID) + " FORMAT JSONEachRow"
	spansResult, err := s.querySQL(spansSQL)
	if err != nil {
		return nil, fmt.Errorf("get trace spans: %w", err)
	}

	// Query trace-level metadata from the traces table.
	metaSQL := buildGetTraceMetaSQL(traceID) + " FORMAT JSONEachRow"
	metaResult, err := s.querySQL(metaSQL)
	if err != nil {
		return nil, fmt.Errorf("get trace meta: %w", err)
	}

	return parseTraceDetail(traceIDHex, spansResult, metaResult)
}

// GetServices returns distinct service names.
func (s *chDBStore) GetServices(ctx context.Context) ([]string, error) {
	sql := `SELECT DISTINCT resource_attributes['service.name'] AS service FROM traces WHERE resource_attributes['service.name'] != '' ORDER BY service FORMAT JSONEachRow`
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get services: %w", err)
	}
	return parseServices(result)
}

// ListSessions returns a paginated list of session summaries.
func (s *chDBStore) ListSessions(ctx context.Context, q SessionQuery) (*SessionListResult, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}

	countSQL := buildSessionCountSQL(q)
	countResult, err := s.querySQL(countSQL + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("count sessions: %w", err)
	}
	total := parseCount(countResult)

	dataSQL := buildSessionListSQL(q)
	dataResult, err := s.querySQL(dataSQL + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	sessions, err := parseSessionListItems(dataResult)
	if err != nil {
		return nil, fmt.Errorf("parse session list: %w", err)
	}

	return &SessionListResult{
		Sessions: sessions,
		Pagination: Pagination{
			Page:     q.Page,
			PageSize: q.PageSize,
			Total:    total,
		},
	}, nil
}

// GetSession returns the session summary and all traces for a session.
func (s *chDBStore) GetSession(ctx context.Context, sessionID string) (*SessionDetail, error) {
	// Get the aggregated session summary using exact-match query.
	summarySQL := buildSessionSummarySQL(sessionID) + " FORMAT JSONEachRow"
	summaryResult, err := s.querySQL(summarySQL)
	if err != nil {
		return nil, fmt.Errorf("get session summary: %w", err)
	}
	sessions, err := parseSessionListItems(summaryResult)
	if err != nil {
		return nil, fmt.Errorf("parse session summary: %w", err)
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	// Get all traces for this session.
	tracesSQL := buildSessionTracesSQL(sessionID) + " FORMAT JSONEachRow"
	tracesResult, err := s.querySQL(tracesSQL)
	if err != nil {
		return nil, fmt.Errorf("get session traces: %w", err)
	}
	traces, err := parseTraceListItems(tracesResult)
	if err != nil {
		return nil, fmt.Errorf("parse session traces: %w", err)
	}

	return &SessionDetail{
		Session: sessions[0],
		Traces:  traces,
	}, nil
}

// Close releases the chDB connection.
func (s *chDBStore) GetModelPricing(ctx context.Context) ([]ModelPricing, error) {
	_ = ctx
	sql := buildModelPricingSelectSQL() + " FORMAT JSONEachRow"
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get model pricing: %w", err)
	}
	return parseModelPricing(result)
}

func (s *chDBStore) UpsertModelPricing(ctx context.Context, p ModelPricing) error {
	_ = ctx
	sql := buildModelPricingUpsertSQL(p)
	return s.execSQL(sql)
}

func (s *chDBStore) DeleteModelPricing(ctx context.Context, modelName string) error {
	_ = ctx
	sql := buildModelPricingDeleteSQL(modelName)
	return s.execSQL(sql)
}

func (s *chDBStore) GetLLMConfigs(ctx context.Context) ([]LLMConfig, error) {
	_ = ctx
	sql := buildLLMConfigSelectSQL() + " FORMAT JSONEachRow"
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get llm configs: %w", err)
	}
	return parseLLMConfigs(result)
}

func (s *chDBStore) CreateLLMConfig(ctx context.Context, c *LLMConfig) error {
	_ = ctx
	if c.IsDefault {
		if err := s.execSQL(buildLLMConfigClearDefaultSQL()); err != nil {
			return fmt.Errorf("clear defaults: %w", err)
		}
	}
	sql := buildLLMConfigInsertSQL(*c)
	return s.execSQL(sql)
}

func (s *chDBStore) UpdateLLMConfig(ctx context.Context, c *LLMConfig) error {
	_ = ctx
	// If api_key is the masked sentinel, retain the existing key.
	if strings.Contains(c.APIKey, "***") {
		existing, err := s.GetLLMConfigs(ctx)
		if err != nil {
			return fmt.Errorf("get existing configs: %w", err)
		}
		for _, e := range existing {
			if e.ID == c.ID {
				c.APIKey = e.APIKey
				break
			}
		}
	}
	if c.IsDefault {
		if err := s.execSQL(buildLLMConfigClearDefaultSQL()); err != nil {
			return fmt.Errorf("clear defaults: %w", err)
		}
	}
	sql := buildLLMConfigUpdateSQL(*c)
	return s.execSQL(sql)
}

func (s *chDBStore) DeleteLLMConfig(ctx context.Context, id string) error {
	_ = ctx
	sql := buildLLMConfigDeleteSQL(id)
	return s.execSQL(sql)
}

func (s *chDBStore) UpdateTraceCost(ctx context.Context, traceID [16]byte) error {
	// Query spans with token/model info.
	spansSQL := buildSelectSpanTokensSQL(traceID) + " FORMAT JSONEachRow"
	result, err := s.querySQL(spansSQL)
	if err != nil {
		return fmt.Errorf("query span tokens: %w", err)
	}

	// Parse span token rows.
	type spanTokenRow struct {
		InputTokens       *uint32 `json:"input_tokens"`
		OutputTokens      *uint32 `json:"output_tokens"`
		TotalTokens       *uint32 `json:"total_tokens"`
		GenAIRequestModel *string `json:"gen_ai_request_model"`
	}
	var rows []spanTokenRow
	for _, line := range splitLines(result) {
		if line == "" {
			continue
		}
		var row spanTokenRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		rows = append(rows, row)
	}

	// Get pricing table.
	pricings, err := s.GetModelPricing(ctx)
	if err != nil {
		return fmt.Errorf("get pricing for cost calc: %w", err)
	}

	// Calculate cost.
	var totalCost float64
	var currency string
	hasCost := false
	for _, row := range rows {
		if row.TotalTokens == nil || *row.TotalTokens == 0 {
			continue
		}
		if row.GenAIRequestModel == nil || *row.GenAIRequestModel == "" {
			continue
		}
		for _, p := range pricings {
			if p.ModelName == *row.GenAIRequestModel {
				inputT := float64(0)
				outputT := float64(0)
				if row.InputTokens != nil {
					inputT = float64(*row.InputTokens)
				}
				if row.OutputTokens != nil {
					outputT = float64(*row.OutputTokens)
				}
				c := (inputT*p.InputPrice + outputT*p.OutputPrice) / 1_000_000.0
				totalCost += c
				hasCost = true
				if currency == "" {
					currency = p.Currency
				}
				break
			}
		}
	}

	if !hasCost {
		return nil
	}

	// Update trace with calculated cost.
	updateSQL := buildUpdateTraceCostSQL(traceID, totalCost, currency)
	return s.execSQL(updateSQL)
}

func (s *chDBStore) Close() error {
	if s.conn != nil {
		C.chdb_close(s.conn)
		s.conn = nil
	}
	return nil
}

// Purge removes traces (and their spans) that exceed the retention policy.
func (s *chDBStore) Purge(ctx context.Context, maxAge time.Duration, maxCount int) (int, int, error) {
	now := uint64(time.Now().UnixMilli())

	// Get current counts.
	countResult, err := s.querySQL("SELECT count(*) AS count FROM traces FORMAT JSONEachRow")
	if err != nil {
		return 0, 0, fmt.Errorf("count traces: %w", err)
	}
	traceCountBefore := parseCount(countResult)

	spanCountResult, err := s.querySQL("SELECT count(*) AS count FROM spans FORMAT JSONEachRow")
	if err != nil {
		return 0, 0, fmt.Errorf("count spans: %w", err)
	}
	spanCountBefore := parseCount(spanCountResult)

	// Phase 1: delete by age.
	if maxAge > 0 {
		cutoffMS := now - uint64(maxAge.Milliseconds())
		if err := s.execSQL(fmt.Sprintf(
			"ALTER TABLE traces DELETE WHERE start_time_ms < %d", cutoffMS)); err != nil {
			return 0, 0, fmt.Errorf("delete old traces: %w", err)
		}
		if err := s.execSQL(fmt.Sprintf(
			"ALTER TABLE spans DELETE WHERE trace_id IN (SELECT trace_id FROM traces WHERE start_time_ms < %d)", cutoffMS)); err != nil {
			return 0, 0, fmt.Errorf("delete old spans: %w", err)
		}
	}

	// Phase 2: delete by count (keep newest maxCount).
	if maxCount > 0 {
		// Re-count after age deletion.
		countResult2, err := s.querySQL("SELECT count(*) AS count FROM traces FORMAT JSONEachRow")
		if err != nil {
			return 0, 0, fmt.Errorf("recount traces: %w", err)
		}
		currentCount := parseCount(countResult2)
		if currentCount > maxCount {
			if err := s.execSQL(fmt.Sprintf(
				"ALTER TABLE spans DELETE WHERE trace_id NOT IN (SELECT trace_id FROM traces ORDER BY start_time_ms DESC LIMIT %d)", maxCount)); err != nil {
				return 0, 0, fmt.Errorf("delete excess spans: %w", err)
			}
			if err := s.execSQL(fmt.Sprintf(
				"ALTER TABLE traces DELETE WHERE trace_id NOT IN (SELECT trace_id FROM traces ORDER BY start_time_ms DESC LIMIT %d)", maxCount)); err != nil {
				return 0, 0, fmt.Errorf("delete excess traces: %w", err)
			}
		}
	}

	// Phase 3: delete orphaned log records.
	if err := s.execSQL("ALTER TABLE logs DELETE WHERE trace_id NOT IN (SELECT trace_id FROM traces)"); err != nil {
		// Non-fatal: log cleanup failure shouldn't block trace purge.
		// The logs table may not exist on first run before any logs are ingested.
	}

	// Estimate deletions (MergeTree mutations are async, exact counts unavailable).
	traceCountAfter, _ := s.querySQL("SELECT count(*) AS count FROM traces FORMAT JSONEachRow")
	spanCountAfter, _ := s.querySQL("SELECT count(*) AS count FROM spans FORMAT JSONEachRow")
	tracesAfter := parseCount(traceCountAfter)
	spansAfter := parseCount(spanCountAfter)

	deletedTraces := traceCountBefore - tracesAfter
	if deletedTraces < 0 {
		deletedTraces = 0
	}
	deletedSpans := spanCountBefore - spansAfter
	if deletedSpans < 0 {
		deletedSpans = 0
	}

	return deletedTraces, deletedSpans, nil
}

// JSON parsing helpers

func parseLogListItems(result string) ([]LogListItem, error) {
	lines := splitLines(result)
	items := make([]LogListItem, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var raw struct {
			TraceIDHex string            `json:"trace_id_hex"`
			SpanIDHex  string            `json:"span_id_hex"`
			Timestamp  uint64            `json:"timestamp"`
			Severity   string            `json:"severity"`
			EventName  string            `json:"event_name"`
			Body       string            `json:"body"`
			Attributes map[string]string `json:"attributes"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse log item: %w (line: %s)", err, line)
		}
		items = append(items, LogListItem{
			TraceIDHex: raw.TraceIDHex,
			SpanIDHex:  raw.SpanIDHex,
			Timestamp:  raw.Timestamp,
			Severity:   raw.Severity,
			EventName:  raw.EventName,
			Body:       raw.Body,
			Attributes: raw.Attributes,
		})
	}
	return items, nil
}

func parseLogEventNames(result string) ([]string, error) {
	lines := splitLines(result)
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var row struct {
			EventName string `json:"event_name"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.EventName != "" {
			names = append(names, row.EventName)
		}
	}
	return names, nil
}

func parseCount(result string) int {
	if result == "" {
		return 0
	}
	var rows []struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(result), &rows); err != nil {
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	return rows[0].Count
}

func parseTraceListItems(result string) ([]TraceListItem, error) {
	var items []TraceListItem
	lines := splitLines(result)
	for _, line := range lines {
		if line == "" {
			continue
		}
		var item TraceListItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("parse trace list item: %w (line: %s)", err, line)
		}
		items = append(items, item)
	}
	return items, nil
}

func parseTraceDetail(traceIDHex, spansResult, metaResult string) (*TraceDetail, error) {
	lines := splitLines(spansResult)
	spans := make([]SpanDetail, 0, len(lines))
	var root *SpanDetail
	var minStartMS, maxEndMS uint64
	for _, line := range lines {
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse span: %w (line: %s)", err, line)
		}
		sd := mapToSpanDetail(raw)
		spans = append(spans, sd)
		if sd.ParentSpanID == "" {
			rootCopy := sd
			root = &rootCopy
		}
		// Track min/max times for trace-level duration.
		if minStartMS == 0 || sd.StartTimeMS < minStartMS {
			minStartMS = sd.StartTimeMS
		}
		endMS := sd.StartTimeMS + sd.DurationMS
		if endMS > maxEndMS {
			maxEndMS = endMS
		}
	}
	if root == nil {
		return nil, fmt.Errorf("no root span found in trace")
	}

	// Parse trace-level metadata from the traces table.
	resourceAttrs := make(map[string]string)
	resourceSchemaURL := ""
	scope := ScopeDetail{}
	if metaResult != "" {
		var metaRaw map[string]interface{}
		if err := json.Unmarshal([]byte(metaResult), &metaRaw); err == nil {
			// resource_attributes is a Map(String,String) returned as JSON object.
			if rawRA, ok := metaRaw["resource_attributes"]; ok && rawRA != nil {
				if m, ok := rawRA.(map[string]interface{}); ok {
					for k, v := range m {
						if s, ok := v.(string); ok {
							resourceAttrs[k] = s
						}
					}
				}
			}
			if v, ok := metaRaw["resource_schema_url"]; ok {
				if s, ok := v.(string); ok {
					resourceSchemaURL = s
				}
			}
			if v, ok := metaRaw["scope_name"]; ok {
				if s, ok := v.(string); ok {
					scope.Name = s
				}
			}
			if v, ok := metaRaw["scope_version"]; ok {
				if s, ok := v.(string); ok {
					scope.Version = s
				}
			}
			// scope_attributes is also Map(String,String).
			scope.Attributes = make(map[string]string)
			if rawSA, ok := metaRaw["scope_attributes"]; ok && rawSA != nil {
				if m, ok := rawSA.(map[string]interface{}); ok {
					for k, v := range m {
						if s, ok := v.(string); ok {
							scope.Attributes[k] = s
						}
					}
				}
			}
		}
	}

	traceDuration := maxEndMS - minStartMS

	return &TraceDetail{
		TraceIDHex:        traceIDHex,
		RootSpanID:        root.SpanID,
		SpanCount:         len(spans),
		StartTimeMS:       minStartMS,
		DurationMS:        traceDuration,
		ResourceAttrs:     resourceAttrs,
		ResourceSchemaURL: resourceSchemaURL,
		Scope:             scope,
		Spans:             spans,
	}, nil
}

func parseServices(result string) ([]string, error) {
	lines := splitLines(result)
	services := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var row struct {
			Service string `json:"service"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.Service != "" {
			services = append(services, row.Service)
		}
	}
	return services, nil
}

func parseSessionListItems(result string) ([]SessionListItem, error) {
	var items []SessionListItem
	lines := splitLines(result)
	for _, line := range lines {
		if line == "" {
			continue
		}
		var raw struct {
			SessionID       string  `json:"session_id"`
			TraceCount      int     `json:"trace_count"`
			TotalTokens     *uint32 `json:"total_tokens"`
				Cost            *float64 `json:"cost"`
				CostCurrency    string   `json:"cost_currency"`
			TotalDurationMS uint64  `json:"total_duration_ms"`
			MaxDurationMS   uint64  `json:"max_duration_ms"`
			AvgDurationMS   float64 `json:"avg_duration_ms"`
			ErrorCount      int     `json:"error_count"`
			ErrorRate       float64 `json:"error_rate"`
			FirstActiveMS   uint64  `json:"first_active_ms"`
			LastActiveMS    uint64  `json:"last_active_ms"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse session item: %w (line: %s)", err, line)
		}
		items = append(items, SessionListItem{
			SessionID:       raw.SessionID,
			TraceCount:      raw.TraceCount,
			TotalTokens:     raw.TotalTokens,
				Cost:            raw.Cost,
				CostCurrency:    raw.CostCurrency,
			TotalDurationMS: raw.TotalDurationMS,
			MaxDurationMS:   raw.MaxDurationMS,
			AvgDurationMS:   raw.AvgDurationMS,
			ErrorCount:      raw.ErrorCount,
			ErrorRate:       raw.ErrorRate,
			FirstActiveMS:   raw.FirstActiveMS,
			LastActiveMS:    raw.LastActiveMS,
		})
	}
	return items, nil
}

func parseLLMConfigs(result string) ([]LLMConfig, error) {
	var items []LLMConfig
	for _, line := range splitLines(result) {
		if line == "" {
			continue
		}
		var c LLMConfig
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("parse llm config: %w (line: %s)", err, line)
		}
		items = append(items, c)
	}
	return items, nil
}

func parseModelPricing(result string) ([]ModelPricing, error) {
	var items []ModelPricing
	for _, line := range splitLines(result) {
		if line == "" {
			continue
		}
		var p ModelPricing
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			return nil, fmt.Errorf("parse model pricing: %w (line: %s)", err, line)
		}
		items = append(items, p)
	}
	return items, nil
}

func mapToSpanDetail(raw map[string]interface{}) SpanDetail {
	getStr := func(k string) string {
		if v, ok := raw[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getUint64 := func(k string) uint64 {
		if v, ok := raw[k]; ok {
			switch n := v.(type) {
			case float64:
				return uint64(n)
			case uint64:
				return n
			}
		}
		return 0
	}
	getNullableUint32 := func(k string) *uint32 {
		if v, ok := raw[k]; ok && v != nil {
			switch n := v.(type) {
			case float64:
				val := uint32(n)
				return &val
			case uint32:
				return &n
			}
		}
		return nil
	}
	getNullableString := func(k string) *string {
		if v, ok := raw[k]; ok && v != nil {
			if s, ok := v.(string); ok {
				return &s
			}
		}
		return nil
	}

	// Parse attributes (chDB returns Map(String,String) as a JSON object).
	attrs := make(map[string]string)
	if rawAttrs, ok := raw["attributes"]; ok && rawAttrs != nil {
		if m, ok := rawAttrs.(map[string]interface{}); ok {
			for k, v := range m {
				if s, ok := v.(string); ok {
					attrs[k] = s
				}
			}
		}
	}

	// Parse events and links (JSON strings).
	events := parseJSONArray(getStr("events"))
	links := parseJSONArray(getStr("links"))

	return SpanDetail{
		SpanID:            getStr("span_id"),
		ParentSpanID:      getStr("parent_span_id"),
		Name:              getStr("name"),
		Kind:              getStr("kind"),
		StartTimeMS:       getUint64("start_time_ms"),
		DurationMS:        getUint64("duration_ms"),
		Attributes:        attrs,
		Events:            events,
		Links:             links,
		Status:            getStr("status_code"),
		StatusMessage:     getStr("status_message"),
		InputTokens:       getNullableUint32("input_tokens"),
		OutputTokens:      getNullableUint32("output_tokens"),
		TotalTokens:       getNullableUint32("total_tokens"),
		GenAIRequestModel: getNullableString("gen_ai_request_model"),
	}
}

func splitLines(s string) []string {
	lines := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
