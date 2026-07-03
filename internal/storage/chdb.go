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
	"encoding/hex"
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
		// Query existing trace rows so we can merge batch-level aggregates
		// with data already stored from previous InsertSpans calls.
		selectSQL := buildSelectExistingTraceSQL(trace.TraceID) + " FORMAT JSONEachRow"
		result, err := s.querySQL(selectSQL)
		if err != nil {
			return fmt.Errorf("query existing trace %s: %w", trace.TraceIDHex, err)
		}

		if result != "" {
			// Merge span counts, token totals, and time range from every
			// existing row (there may be >1 if duplicates were created
			// before this fix).
			lines := splitLines(result)
			for _, line := range lines {
				if line == "" {
					continue
				}
				var existing struct {
					SpanCount   uint16  `json:"span_count"`
					TotalTokens *uint32 `json:"total_tokens"`
					StartTimeMS uint64  `json:"start_time_ms"`
					EndTimeMS   uint64  `json:"end_time_ms"`
					RootSpanID  string  `json:"root_span_id"`
					RootName    string  `json:"root_name"`
					SessionID   string  `json:"session_id"`
				}
				if err := json.Unmarshal([]byte(line), &existing); err != nil {
					continue
				}

				// Accumulate span count across batches.
				trace.SpanCount += existing.SpanCount

				// Accumulate tokens.
				if existing.TotalTokens != nil {
					if trace.TotalTokens == nil {
						v := *existing.TotalTokens
						trace.TotalTokens = &v
					} else {
						sum := *trace.TotalTokens + *existing.TotalTokens
						trace.TotalTokens = &sum
					}
				}

				// Extend time range so the trace envelope covers all batches.
				if existing.StartTimeMS < trace.StartTimeMS {
					trace.StartTimeMS = existing.StartTimeMS
				}
				if existing.EndTimeMS > trace.EndTimeMS {
					trace.EndTimeMS = existing.EndTimeMS
					trace.DurationMS = trace.EndTimeMS - trace.StartTimeMS
				}

				// Preserve root-span info when the current batch only
				// carries child spans (root was ingested in a prior batch).
				if isZeroSpanID(trace.RootSpanID) && existing.RootSpanID != "" {
					if decoded, err := hex.DecodeString(existing.RootSpanID); err == nil && len(decoded) == 8 {
						copy(trace.RootSpanID[:], decoded)
					}
					trace.RootName = existing.RootName
					if trace.SessionID == "" {
						trace.SessionID = existing.SessionID
					}
				}
			}

			// Remove old rows so we can insert a single merged row.
			deleteSQL := buildDeleteTraceSQL(trace.TraceID)
			if err := s.execSQL(deleteSQL); err != nil {
				return fmt.Errorf("delete old trace rows %s: %w", trace.TraceIDHex, err)
			}
		}

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

// GetLogCountsByTrace returns the per-span log count for a trace.
func (s *chDBStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	sql := buildLogCountsByTraceSQL(traceID) + " FORMAT JSONEachRow"
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get log counts by trace: %w", err)
	}
	return parseLogCounts(result), nil
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

	if len(traces) > 0 {
		if err := s.loadRootSpanInputMessages(ctx, traces); err != nil {
			return nil, fmt.Errorf("load input messages: %w", err)
		}
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

// loadRootSpanInputMessages fetches gen_ai.input.messages from each trace's
// root span and attaches it to the matching item. The root span is the span
// whose parent_span_id is all zeros — the same rule isRootSpan uses at
// ingestion. A future probe populates this attribute on root spans.
func (s *chDBStore) loadRootSpanInputMessages(ctx context.Context, traces []TraceListItem) error {
	_ = ctx
	idxByTid := make(map[string]int, len(traces))
	inList := make([]string, 0, len(traces))
	for i, t := range traces {
		idxByTid[strings.ToLower(t.TraceIDHex)] = i
		inList = append(inList, fmt.Sprintf("unhex('%s')", escapeSQL(t.TraceIDHex)))
	}
	query := fmt.Sprintf(
		`SELECT lower(hex(trace_id)) AS trace_id_hex, attributes['gen_ai.input.messages'] AS input_messages
		FROM spans
		WHERE parent_span_id = unhex('0000000000000000') AND trace_id IN (%s)`,
		strings.Join(inList, ","))
	result, err := s.querySQL(query + " FORMAT JSONEachRow")
	if err != nil {
		return fmt.Errorf("query root span input: %w", err)
	}
	for _, line := range splitLines(result) {
		if line == "" {
			continue
		}
		var row struct {
			TraceIDHex    string `json:"trace_id_hex"`
			InputMessages string `json:"input_messages"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return fmt.Errorf("parse root span input: %w", err)
		}
		if row.InputMessages == "" {
			continue
		}
		if idx, ok := idxByTid[strings.ToLower(row.TraceIDHex)]; ok {
			v := row.InputMessages
			traces[idx].InputMessages = &v
		}
	}
	return nil
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
func (s *chDBStore) GetSession(ctx context.Context, sessionID string, page, pageSize int) (*SessionDetail, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

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

	// Total traces for pagination.
	countResult, err := s.querySQL(buildSessionTraceCountSQL(sessionID) + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("count session traces: %w", err)
	}
	total := parseCount(countResult)

	// Get a page of traces for this session.
	tracesSQL := buildSessionTracesSQL(sessionID, page, pageSize) + " FORMAT JSONEachRow"
	tracesResult, err := s.querySQL(tracesSQL)
	if err != nil {
		return nil, fmt.Errorf("get session traces: %w", err)
	}
	traces, err := parseTraceListItems(tracesResult)
	if err != nil {
		return nil, fmt.Errorf("parse session traces: %w", err)
	}

	return &SessionDetail{
		Session:    sessions[0],
		Traces:     traces,
		Pagination: Pagination{Page: page, PageSize: pageSize, Total: total},
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

func (s *chDBStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*DiagnosisResult, error) {
	_ = ctx
	sql := buildDiagnosisResultSelectSQL(traceID) + " FORMAT JSONEachRow"
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get diagnosis result: %w", err)
	}
	return parseDiagnosisResult(result)
}

func (s *chDBStore) UpsertDiagnosisResult(ctx context.Context, result *DiagnosisResult) error {
	_ = ctx
	// Delete existing row first (MergeTree doesn't support real UPSERT).
	deleteSQL := buildDiagnosisResultDeleteSQL(result.TraceID)
	s.execSQL(deleteSQL) // ignore error — row may not exist
	insertSQL := buildDiagnosisResultInsertSQL(*result)
	return s.execSQL(insertSQL)
}

func (s *chDBStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*AgentStats, error) {
	sid := escapeSQL(sessionID)

	// Traces for the session — only StatusCode is needed for trace success rate.
	traceResult, err := s.querySQL(fmt.Sprintf(
		`SELECT toString(status_code) AS status_code
		 FROM traces WHERE session_id = '%s' FORMAT JSONEachRow`, sid))
	if err != nil {
		return nil, fmt.Errorf("query session traces: %w", err)
	}
	var traces []Trace
	for _, line := range splitLines(traceResult) {
		if line == "" {
			continue
		}
		var row struct {
			StatusCode string `json:"status_code"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse session trace: %w (line: %s)", err, line)
		}
		traces = append(traces, Trace{StatusCode: StatusCodeFromString(row.StatusCode)})
	}
	if len(traces) == 0 {
		// No data -> handler returns 404 no_agent_data so the UI hides the
		// section gracefully (matches memstore/SQLite), rather than a 500.
		return nil, nil
	}

	// Spans belonging to those traces. trace_id is binary (FixedString(16)) in
	// both tables, so the subquery compares directly without unhex.
	// computeAgentStats only reads StartTimeMS, StatusCode and Attributes.
	spanResult, err := s.querySQL(fmt.Sprintf(
		`SELECT start_time_ms, toString(status_code) AS status_code, attributes
		 FROM spans
		 WHERE trace_id IN (SELECT trace_id FROM traces WHERE session_id = '%s')
		 ORDER BY start_time_ms FORMAT JSONEachRow`, sid))
	if err != nil {
		return nil, fmt.Errorf("query session spans: %w", err)
	}
	var spans []Span
	for _, line := range splitLines(spanResult) {
		if line == "" {
			continue
		}
		var row struct {
			StartTimeMS uint64            `json:"start_time_ms"`
			StatusCode  string            `json:"status_code"`
			Attributes  map[string]string `json:"attributes"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse session span: %w (line: %s)", err, line)
		}
		spans = append(spans, Span{
			StartTimeMS: row.StartTimeMS,
			StatusCode:  StatusCodeFromString(row.StatusCode),
			Attributes:  row.Attributes,
		})
	}

	return computeAgentStats(traces, spans), nil
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

	// Phase 3: orphaned log records are age-purged separately via PurgeLogs.

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

// PurgeLogs removes log records older than (now - maxAge) by their own
// timestamp. Non-fatal: the logs table may not exist on first run.
func (s *chDBStore) PurgeLogs(ctx context.Context, maxAge time.Duration) (int, error) {
	_ = ctx
	if maxAge <= 0 {
		return 0, nil
	}
	cutoffMS := uint64(time.Now().UnixMilli()) - uint64(maxAge.Milliseconds())
	if err := s.execSQL(fmt.Sprintf("ALTER TABLE logs DELETE WHERE timestamp < %d", cutoffMS)); err != nil {
		return 0, nil // non-fatal: logs table may not exist yet
	}
	return 0, nil // MergeTree mutations are async; exact count unavailable
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

// parseLogCounts parses JSONEachRow rows of {span_id_hex, n} into a map.
func parseLogCounts(result string) map[string]int {
	counts := make(map[string]int)
	for _, line := range splitLines(result) {
		if line == "" {
			continue
		}
		var row struct {
			SpanIDHex string `json:"span_id_hex"`
			N         int    `json:"n"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		counts[row.SpanIDHex] = row.N
	}
	return counts
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

func parseDiagnosisResult(result string) (*DiagnosisResult, error) {
	for _, line := range splitLines(result) {
		if line == "" {
			continue
		}
		var raw struct {
			TraceIDHex    string    `json:"trace_id_hex"`
			ModelName     string    `json:"model_name"`
			Scores        string    `json:"scores"`
			OverallScore  uint8     `json:"overall_score"`
			Findings      string    `json:"findings"`
			Summary       string    `json:"summary"`
			SpansSnapshot string    `json:"spans_snapshot"`
			RawResponse   string    `json:"raw_response"`
			CreatedAt     time.Time `json:"created_at"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse diagnosis result: %w (line: %s)", err, line)
		}

		var scores DiagnosisScores
		if err := json.Unmarshal([]byte(raw.Scores), &scores); err != nil {
			return nil, fmt.Errorf("parse diagnosis scores: %w", err)
		}

		var findings []DiagnosisFinding
		if err := json.Unmarshal([]byte(raw.Findings), &findings); err != nil {
			return nil, fmt.Errorf("parse diagnosis findings: %w", err)
		}

		traceIDBytes, err := hex.DecodeString(raw.TraceIDHex)
		if err != nil {
			return nil, fmt.Errorf("decode trace_id: %w", err)
		}
		var traceID [16]byte
		copy(traceID[:], traceIDBytes)

		return &DiagnosisResult{
			TraceID:       traceID,
			TraceIDHex:    raw.TraceIDHex,
			ModelName:     raw.ModelName,
			Scores:        scores,
			OverallScore:  raw.OverallScore,
			Findings:      findings,
			Summary:       raw.Summary,
			SpansSnapshot: raw.SpansSnapshot,
			RawResponse:   raw.RawResponse,
			CreatedAt:     raw.CreatedAt,
		}, nil
	}
	return nil, nil // no diagnosis found
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

	sd := SpanDetail{
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
		CacheCreationTokens: getNullableUint32("cache_creation_tokens"),
		CacheReadTokens:   getNullableUint32("cache_read_tokens"),
		GenAIRequestModel: getNullableString("gen_ai_request_model"),
	}

	// Extract GenAI semantic attributes.
	if attrs != nil {
		if v, ok := attrs["gen_ai.system"]; ok {
			sd.GenAISystem = &v
		}
		if v, ok := attrs["gen_ai.tool.name"]; ok {
			sd.ToolName = &v
			sd.IsToolCall = true
		}
	}

	return sd
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
