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
	"sync"
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
	}

	return nil
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
	sql := buildGetTraceSQL(traceID) + " FORMAT JSONEachRow"
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get trace: %w", err)
	}
	return parseTraceDetail(result)
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
func (s *chDBStore) Close() error {
	if s.conn != nil {
		C.chdb_close(s.conn)
		s.conn = nil
	}
	return nil
}

// JSON parsing helpers

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

func parseTraceDetail(result string) (*TraceDetail, error) {
	lines := splitLines(result)
	spans := make([]SpanDetail, 0, len(lines))
	var root *SpanDetail
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
	}
	if root == nil {
		return nil, fmt.Errorf("no root span found in trace")
	}

	resourceAttrs := make(map[string]string)

	return &TraceDetail{
		TraceIDHex:    "",
		RootSpanID:    root.SpanID,
		SpanCount:     len(spans),
		StartTimeMS:   root.StartTimeMS,
		DurationMS:    root.DurationMS,
		ResourceAttrs: resourceAttrs,
		Spans:         spans,
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

	return SpanDetail{
		SpanID:            getStr("span_id"),
		ParentSpanID:      getStr("parent_span_id"),
		Name:              getStr("name"),
		Kind:              getStr("kind"),
		StartTimeMS:       getUint64("start_time_ms"),
		DurationMS:        getUint64("duration_ms"),
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
