//go:build !local_engine && !nosqlite

// Package storage provides a pure-Go SQLite Store implementation for non-CGO builds.
// Uses modernc.org/sqlite (no CGO required) with WAL mode for concurrent reads.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed sqlite_schema.sql
var sqliteSchemaSQL string

// sqliteStore implements the Store interface using SQLite with WAL mode.
type sqliteStore struct {
	mu  sync.Mutex // protects write operations
	db  *sql.DB
	dir string
}

// NewChDBStore creates a SQLite-backed Store for non-CGO builds.
// On CGO builds, this function is replaced by the chDB implementation.
func NewChDBStore(dataDir string) (Store, error) {
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "labubu.db")

	// WAL mode enables concurrent reads while writes are serialized.
	// busy_timeout prevents immediate failures on write contention.
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Apply schema (embedded at compile time)
	if _, err := db.Exec(sqliteSchemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	// Migrate: add provider_type column to existing llm_configs tables.
	db.Exec(`ALTER TABLE llm_configs ADD COLUMN provider_type TEXT NOT NULL DEFAULT 'openai'`)

	// Migrate: add prompt-caching token columns to existing spans tables.
	// New columns are nullable; populated by the backfill below for old rows.
	db.Exec(`ALTER TABLE spans ADD COLUMN cache_creation_tokens INTEGER`)
	db.Exec(`ALTER TABLE spans ADD COLUMN cache_read_tokens INTEGER`)

	// Migrate: add per-span cost columns for accurate by-model cost aggregation.
	db.Exec(`ALTER TABLE spans ADD COLUMN cost REAL`)
	db.Exec(`ALTER TABLE spans ADD COLUMN cost_currency TEXT NOT NULL DEFAULT ''`)

	s := &sqliteStore{db: db, dir: dataDir}

	// Seed default pricing from config (same as memStore/chDB)
	cfg := LoadConfig("")
	for _, m := range cfg.Pricing.Models {
		s.UpsertModelPricing(context.Background(), m)
	}

	// Backfill prompt-caching token columns + totals for pre-existing spans.
	s.backfillCacheTokens(context.Background())

	// Backfill per-span cost for pre-existing spans (idempotent).
	s.backfillSpanCost(context.Background())

	return s, nil
}

// backfillCacheTokens migrates spans that predate the cache_creation_tokens /
// cache_read_tokens columns. Those spans stored only input+output in
// total_tokens, but the raw attributes JSON still holds the Claude Code cache
// values. This re-derives the cache columns, recomputes span total_tokens, and
// re-aggregates trace total_tokens + cost. Idempotent: once cache_creation_tokens
// is non-NULL the span is skipped.
func (s *sqliteStore) backfillCacheTokens(ctx context.Context) {
	// Load pricing for cost recompute.
	pricingMap := make(map[string]ModelPricing)
	if pricingRows, err := s.db.QueryContext(ctx, `SELECT model_name, input_price, output_price, currency FROM model_pricing`); err == nil {
		for pricingRows.Next() {
			var p ModelPricing
			if pricingRows.Scan(&p.ModelName, &p.InputPrice, &p.OutputPrice, &p.Currency) == nil {
				pricingMap[p.ModelName] = p
			}
		}
		pricingRows.Close()
	}

	// Find spans predating the cache columns whose attributes hold cache values.
	// Both columns NULL precisely targets pre-fix spans (new spans always write
	// at least one cache column, even if zero).
	rows, err := s.db.QueryContext(ctx,
		`SELECT trace_id_hex, span_id_hex, input_tokens, output_tokens, attributes
		 FROM spans
		 WHERE cache_creation_tokens IS NULL AND cache_read_tokens IS NULL
		   AND (attributes LIKE '%cache_creation_tokens%' OR attributes LIKE '%cache_read_tokens%')`)
	if err != nil {
		return
	}
	defer rows.Close()

	type spanFix struct {
		traceID, spanID           string
		cacheCreate, cacheRead, total uint32
	}
	var fixes []spanFix
	affectedTraces := make(map[string]struct{})

	for rows.Next() {
		var traceID, spanID, attrsJSON string
		var inputTokens, outputTokens sql.NullInt32
		if err := rows.Scan(&traceID, &spanID, &inputTokens, &outputTokens, &attrsJSON); err != nil {
			continue
		}
		attrs := map[string]string{}
		_ = json.Unmarshal([]byte(attrsJSON), &attrs)
		cc := parseUint32Attr(attrs, "cache_creation_tokens", "cache_creation_input_tokens")
		cr := parseUint32Attr(attrs, "cache_read_tokens", "cache_read_input_tokens")

		var sum uint32
		if inputTokens.Valid {
			sum += uint32(inputTokens.Int32)
		}
		if outputTokens.Valid {
			sum += uint32(outputTokens.Int32)
		}
		sum += cc + cr

		fixes = append(fixes, spanFix{traceID, spanID, cc, cr, sum})
		affectedTraces[traceID] = struct{}{}
	}
	rows.Close()

	if len(fixes) == 0 {
		return
	}

	const cacheCreateRate = 1.25
	const cacheReadRate = 0.1

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	for _, f := range fixes {
		if _, err := tx.ExecContext(ctx,
			`UPDATE spans SET cache_creation_tokens = ?, cache_read_tokens = ?, total_tokens = ? WHERE trace_id_hex = ? AND span_id_hex = ?`,
			f.cacheCreate, f.cacheRead, f.total, f.traceID, f.spanID); err != nil {
			return
		}
	}

	// Re-aggregate affected traces: total_tokens + cost.
	for traceID := range affectedTraces {
		var sumTotal sql.NullInt64
		_ = tx.QueryRowContext(ctx,
			`SELECT COALESCE(sum(total_tokens),0) FROM spans WHERE trace_id_hex = ?`, traceID,
		).Scan(&sumTotal)
		var traceTotal *uint32
		if sumTotal.Valid && sumTotal.Int64 > 0 {
			v := uint32(sumTotal.Int64)
			traceTotal = &v
		}

		var cost float64
		var currency string
		if tokenRows, err := tx.QueryContext(ctx,
			`SELECT input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, gen_ai_request_model
			 FROM spans WHERE trace_id_hex = ? AND total_tokens IS NOT NULL`, traceID); err == nil {
			for tokenRows.Next() {
				var inT, outT, ccT, crT sql.NullInt32
				var model sql.NullString
				if tokenRows.Scan(&inT, &outT, &ccT, &crT, &model) != nil {
					continue
				}
				p, ok := pricingMap[model.String]
				if !ok {
					continue
				}
				in := 0.0
				if inT.Valid {
					in = float64(inT.Int32)
				}
				out := 0.0
				if outT.Valid {
					out = float64(outT.Int32)
				}
				ccv := 0.0
				if ccT.Valid {
					ccv = float64(ccT.Int32)
				}
				crv := 0.0
				if crT.Valid {
					crv = float64(crT.Int32)
				}
				spanCost := (in*p.InputPrice + ccv*p.InputPrice*cacheCreateRate +
					crv*p.InputPrice*cacheReadRate + out*p.OutputPrice) / 1_000_000.0
				cost += spanCost
				if currency == "" {
					currency = p.Currency
				}
			}
			tokenRows.Close()
		}

		cost = math.Round(cost*1e6) / 1e6
		if cost > 0 {
			tx.ExecContext(ctx, `UPDATE traces SET total_tokens = ?, cost = ?, cost_currency = ? WHERE trace_id_hex = ?`,
				traceTotal, cost, currency, traceID)
		} else {
			tx.ExecContext(ctx, `UPDATE traces SET total_tokens = ? WHERE trace_id_hex = ?`, traceTotal, traceID)
		}
	}

	_ = tx.Commit()
}

// backfillSpanCost populates spans.cost/cost_currency for pre-existing
// spans (added alongside the span-level cost column). Idempotent: only
// touches traces that still have an LLM span (total_tokens IS NOT NULL)
// with NULL cost; non-LLM spans are never costed and stay NULL.
func (s *sqliteStore) backfillSpanCost(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT trace_id_hex FROM spans WHERE cost IS NULL AND total_tokens IS NOT NULL`)
	if err != nil {
		return
	}
	var traceIDs []string
	for rows.Next() {
		var hexStr string
		if rows.Scan(&hexStr) == nil {
			traceIDs = append(traceIDs, hexStr)
		}
	}
	rows.Close()
	for _, hexStr := range traceIDs {
		b, err := hex.DecodeString(hexStr)
		if err != nil || len(b) != 16 {
			continue
		}
		var tid [16]byte
		copy(tid[:], b)
		s.UpdateTraceCost(ctx, tid)
	}
}

// parseUint32Attr reads the first present numeric attribute key as uint32.
func parseUint32Attr(attrs map[string]string, keys ...string) uint32 {
	for _, k := range keys {
		if v, ok := attrs[k]; ok {
			if n, err := strconv.ParseUint(v, 10, 32); err == nil {
				return uint32(n)
			}
		}
	}
	return 0
}

// mergeTotalTokens combines a previously-stored trace total with the current
// batch's total. Accumulates across batches (existing + new); preserves the
// existing total when the new batch has none; returns the new batch's value
// (possibly nil) when there is no prior total.
func mergeTotalTokens(existing sql.NullInt32, newTotal *uint32) *uint32 {
	if !existing.Valid {
		return newTotal
	}
	ex := uint32(existing.Int32)
	if newTotal == nil {
		v := ex
		return &v
	}
	sum := ex + *newTotal
	return &sum
}

// --- Trace methods ---

func (s *sqliteStore) InsertSpans(ctx context.Context, resource ResourceInfo, scope ScopeInfo, inSpans []Span) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Insert each span
	for _, sp := range inSpans {
		attrsJSON, _ := json.Marshal(sp.Attributes)
		_, err := tx.Exec(
			`INSERT OR REPLACE INTO spans (
				trace_id_hex, span_id_hex, parent_span_id_hex, trace_state, name, kind,
				start_time_ms, end_time_ms, duration_ms, attributes,
				dropped_attributes_count, events, dropped_events_count,
				links, dropped_links_count, status_code, status_message,
				input_tokens, output_tokens, total_tokens, cache_creation_tokens, cache_read_tokens, gen_ai_request_model,
				cost, cost_currency
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			TraceIDToHex(sp.TraceID), SpanIDToHex(sp.SpanID), SpanIDToHex(sp.ParentSpanID),
			sp.TraceState, sp.Name, KindToString(sp.Kind),
			sp.StartTimeMS, sp.EndTimeMS, sp.DurationMS, string(attrsJSON),
			0, sp.Events, 0, sp.Links, 0,
			StatusCodeToString(sp.StatusCode), sp.StatusMessage,
			sp.InputTokens, sp.OutputTokens, sp.TotalTokens, sp.CacheCreationTokens, sp.CacheReadTokens, sp.GenAIRequestModel,
			sp.Cost, sp.CostCurrency,
		)
		if err != nil {
			return fmt.Errorf("insert span: %w", err)
		}
	}

	// Aggregate traces from spans
	traceMap := aggregateTraces(resource, scope, inSpans)

	for traceID, trace := range traceMap {
		traceIDHex := TraceIDToHex(traceID)

		// Check if trace already exists — merge with existing data
		var existingSpanCount int
	var existingStartMS, existingEndMS int64
		var existingRootSpanID, existingRootName, existingSessionID string
		var existingTotalTokens sql.NullInt32
		var existingCost sql.NullFloat64
		var existingCostCurrency string
		var existingResAttrsJSON string

		row := tx.QueryRow(
			`SELECT span_count, start_time_ms, end_time_ms, root_span_id_hex, root_name,
			        total_tokens, cost, cost_currency, session_id, resource_attributes
			 FROM traces WHERE trace_id_hex = ?`,
			traceIDHex,
		)
		err := row.Scan(
			&existingSpanCount, &existingStartMS, &existingEndMS,
			&existingRootSpanID, &existingRootName,
			&existingTotalTokens, &existingCost, &existingCostCurrency,
			&existingSessionID, &existingResAttrsJSON,
		)
		if err == sql.ErrNoRows {
			// New trace, insert directly
		} else if err != nil {
			return fmt.Errorf("select existing trace: %w", err)
		} else {
			// Merge: update counts, time range, tokens
			trace.SpanCount += uint16(existingSpanCount)
			if uint64(existingStartMS) < trace.StartTimeMS && existingStartMS != 0 {
				trace.StartTimeMS = uint64(existingStartMS)
			}
			if uint64(existingEndMS) > trace.EndTimeMS {
				trace.EndTimeMS = uint64(existingEndMS)
			}
			trace.DurationMS = trace.EndTimeMS - trace.StartTimeMS
			if existingRootSpanID != "" && trace.RootSpanID == [8]byte{} {
				trace.RootName = existingRootName
			}
			if existingSessionID != "" && trace.SessionID == "" {
				trace.SessionID = existingSessionID
			}
			// Preserve existing resource_attributes if new batch is empty
			if (trace.ResourceAttrs == nil || len(trace.ResourceAttrs) == 0) && existingResAttrsJSON != "" && existingResAttrsJSON != "{}" && existingResAttrsJSON != "null" {
				trace.ResourceAttrs = jsonToMap(existingResAttrsJSON)
			}
			// Accumulate total_tokens across batches (a trace often arrives
			// in multiple OTLP batches). Re-sending the same span_id would
			// double-count — accepted rare edge, corrected by UpdateTraceCost.
			trace.TotalTokens = mergeTotalTokens(existingTotalTokens, trace.TotalTokens)
			if trace.Cost == nil && existingCost.Valid {
				v := existingCost.Float64
				trace.Cost = &v
			}
			if trace.CostCurrency == "" && existingCostCurrency != "" {
				trace.CostCurrency = existingCostCurrency
			}
		}

		// Insert or replace trace
		resAttrsJSON, _ := json.Marshal(trace.ResourceAttrs)
		scopeAttrsJSON, _ := json.Marshal(trace.ScopeAttrs)
		_, err = tx.Exec(
			`INSERT OR REPLACE INTO traces (
				trace_id_hex, root_span_id_hex, root_name, span_count,
				start_time_ms, end_time_ms, duration_ms,
				resource_attributes, resource_schema_url,
				scope_name, scope_version, scope_attributes, scope_schema_url,
				trace_state, dropped_span_count,
				status_code, status_message,
				total_tokens, session_id, cost, cost_currency
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			traceIDHex, SpanIDToHex(trace.RootSpanID), trace.RootName, trace.SpanCount,
			trace.StartTimeMS, trace.EndTimeMS, trace.DurationMS,
			string(resAttrsJSON), trace.ResourceSchemaURL,
			trace.ScopeName, trace.ScopeVersion, string(scopeAttrsJSON), trace.ScopeSchemaURL,
			"", 0,
			StatusCodeToString(trace.StatusCode), trace.StatusMessage,
			trace.TotalTokens, trace.SessionID, trace.Cost, trace.CostCurrency,
		)
		if err != nil {
			return fmt.Errorf("upsert trace: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	// Async cost calculation for traces with token data (same pattern as chDB)
	go func() {
		for traceID := range traceMap {
			s.UpdateTraceCost(context.Background(), traceID)
		}
	}()

	return nil
}

func (s *sqliteStore) ListTraces(ctx context.Context, q TraceQuery) (*TraceListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	where, args := buildSqliteTraceWhereClause(q)

	// Count total
	var total int
	countSQL := `SELECT count(*) FROM traces` + where
	err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("count traces: %w", err)
	}

	// Fetch page
	offset := (q.Page - 1) * q.PageSize
	listSQL := `SELECT trace_id_hex, root_span_id_hex, root_name,
	               json_extract(resource_attributes, '$."service.name"') AS root_service,
	               start_time_ms, duration_ms, span_count, status_code,
	               total_tokens, cost, cost_currency
	        FROM traces` + where + ` ORDER BY start_time_ms DESC LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, listSQL, append(args, q.PageSize, offset)...)
	if err != nil {
		return nil, fmt.Errorf("list traces: %w", err)
	}
	defer rows.Close()

	var traces []TraceListItem
	for rows.Next() {
		var t TraceListItem
		var rootService sql.NullString
		var totalTokens sql.NullInt32
		var cost sql.NullFloat64
		var costCurrency sql.NullString
		err := rows.Scan(
			&t.TraceIDHex, &t.RootSpanID, &t.RootName, &rootService,
			&t.StartTimeMS, &t.DurationMS, &t.SpanCount, &t.Status,
			&totalTokens, &cost, &costCurrency,
		)
		if err != nil {
			return nil, fmt.Errorf("scan trace: %w", err)
		}
		if rootService.Valid {
			t.RootService = rootService.String
		}
		if totalTokens.Valid {
			v := uint32(totalTokens.Int32)
			t.TotalTokens = &v
		}
		if cost.Valid {
			v := cost.Float64
			t.Cost = &v
		}
		if costCurrency.Valid {
			t.CostCurrency = costCurrency.String
		}
		traces = append(traces, t)
	}

	if traces == nil {
		traces = []TraceListItem{}
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

func (s *sqliteStore) GetTrace(ctx context.Context, traceID [16]byte) (*TraceDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	traceIDHex := TraceIDToHex(traceID)

	// Fetch spans
	spanRows, err := s.db.QueryContext(ctx,
		`SELECT span_id_hex, parent_span_id_hex, name, kind, start_time_ms, duration_ms,
		        attributes, events, links, status_code, status_message,
		        input_tokens, output_tokens, total_tokens, cache_creation_tokens, cache_read_tokens, gen_ai_request_model
		 FROM spans WHERE trace_id_hex = ? ORDER BY start_time_ms`,
		traceIDHex,
	)
	if err != nil {
		return nil, fmt.Errorf("query spans: %w", err)
	}
	defer spanRows.Close()

	var spans []SpanDetail
	var unpricedSpans int
	for spanRows.Next() {
		var sd SpanDetail
		var kind, status string
		var attrsJSON, eventsJSON, linksJSON string
		var inputTokens, outputTokens, totalTokens, cacheCreationTokens, cacheReadTokens sql.NullInt32
		var genAIModel sql.NullString
		err := spanRows.Scan(
			&sd.SpanID, &sd.ParentSpanID, &sd.Name, &kind, &sd.StartTimeMS, &sd.DurationMS,
			&attrsJSON, &eventsJSON, &linksJSON, &status, &sd.StatusMessage,
			&inputTokens, &outputTokens, &totalTokens, &cacheCreationTokens, &cacheReadTokens, &genAIModel,
		)
		if err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		sd.Kind = kind
		sd.Status = status
		sd.Attributes = jsonToMap(attrsJSON)
		sd.Events = parseJSONArray(eventsJSON)
		sd.Links = parseJSONArray(linksJSON)
		if inputTokens.Valid {
			v := uint32(inputTokens.Int32)
			sd.InputTokens = &v
		}
		if outputTokens.Valid {
			v := uint32(outputTokens.Int32)
			sd.OutputTokens = &v
		}
		if totalTokens.Valid {
			v := uint32(totalTokens.Int32)
			sd.TotalTokens = &v
		}
		if cacheCreationTokens.Valid {
			v := uint32(cacheCreationTokens.Int32)
			sd.CacheCreationTokens = &v
		}
		if cacheReadTokens.Valid {
			v := uint32(cacheReadTokens.Int32)
			sd.CacheReadTokens = &v
		}
		if genAIModel.Valid {
			sd.GenAIRequestModel = &genAIModel.String
		}
		// Extract GenAI semantic attributes.
		if sd.Attributes != nil {
			if v, ok := sd.Attributes["gen_ai.system"]; ok {
				sd.GenAISystem = &v
			}
			if v, ok := sd.Attributes["gen_ai.tool.name"]; ok {
				sd.ToolName = &v
				sd.IsToolCall = true
			}
		}
		// Count spans with tokens but no model pricing
		if sd.GenAIRequestModel != nil && sd.TotalTokens != nil {
			// Will check pricing later
		} else if sd.TotalTokens != nil && sd.GenAIRequestModel == nil {
			unpricedSpans++
		}
		spans = append(spans, sd)
	}

	if spans == nil {
		spans = []SpanDetail{}
	}

	// Fetch trace metadata
	var detail TraceDetail
	var resAttrsJSON, scopeAttrsJSON string
	var cost sql.NullFloat64
	var costCurrency sql.NullString
	var scopeName, scopeVersion, resSchemaURL string

	err = s.db.QueryRowContext(ctx,
		`SELECT trace_id_hex, root_span_id_hex, span_count, start_time_ms, duration_ms,
		        resource_attributes, resource_schema_url,
		        scope_name, scope_version, scope_attributes,
		        cost, cost_currency
		 FROM traces WHERE trace_id_hex = ?`,
		traceIDHex,
	).Scan(
		&detail.TraceIDHex, &detail.RootSpanID, &detail.SpanCount, &detail.StartTimeMS, &detail.DurationMS,
		&resAttrsJSON, &resSchemaURL,
		&scopeName, &scopeVersion, &scopeAttrsJSON,
		&cost, &costCurrency,
	)
	if err != nil {
		return nil, fmt.Errorf("query trace meta: %w", err)
	}

	detail.ResourceAttrs = jsonToMap(resAttrsJSON)
	detail.ResourceSchemaURL = resSchemaURL
	detail.Scope = ScopeDetail{
		Name:       scopeName,
		Version:    scopeVersion,
		Attributes: jsonToMap(scopeAttrsJSON),
	}
	if cost.Valid {
		v := cost.Float64
		detail.Cost = &v
	}
	if costCurrency.Valid {
		detail.CostCurrency = costCurrency.String
	}
	detail.Spans = spans
	detail.UnpricedSpans = unpricedSpans

	return &detail, nil
}

func (s *sqliteStore) GetServices(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT json_extract(resource_attributes, '$."service.name"') AS service
		 FROM traces
		 WHERE json_extract(resource_attributes, '$."service.name"') IS NOT NULL
		   AND json_extract(resource_attributes, '$."service.name"') != ''
		 ORDER BY service`,
	)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		services = append(services, svc)
	}
	return services, nil
}

// --- Session methods ---

func (s *sqliteStore) ListSessions(ctx context.Context, q SessionQuery) (*SessionListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	where, args := buildSqliteSessionWhereClause(q)

	// Count
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(DISTINCT session_id) FROM traces WHERE session_id != ''`+where,
		args...,
	).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("count sessions: %w", err)
	}

	// List
	offset := (q.Page - 1) * q.PageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id,
		        count(*) AS trace_count,
		        sum(duration_ms) AS total_duration_ms,
		        max(duration_ms) AS max_duration_ms,
		        round(avg(duration_ms)) AS avg_duration_ms,
		        min(start_time_ms) AS first_active_ms,
		        max(start_time_ms) AS last_active_ms,
		        sum(CASE WHEN status_code='ERROR' THEN 1 ELSE 0 END) AS error_count,
		        round(sum(CASE WHEN status_code='ERROR' THEN 1 ELSE 0 END)*100.0/count(*)) AS error_rate,
		        sum(total_tokens) AS total_tokens,
		        sum(cost) AS cost,
		        CASE WHEN count(DISTINCT cost_currency) > 1 THEN 'mixed' ELSE max(cost_currency) END AS cost_currency
		 FROM traces WHERE session_id != ''`+where+
			` GROUP BY session_id ORDER BY last_active_ms DESC LIMIT ? OFFSET ?`,
		append(args, q.PageSize, offset)...,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionListItem
	for rows.Next() {
		var sl SessionListItem
		var totalTokens sql.NullInt64
		var cost sql.NullFloat64
		var costCurrency sql.NullString
		err := rows.Scan(
			&sl.SessionID, &sl.TraceCount, &sl.TotalDurationMS, &sl.MaxDurationMS, &sl.AvgDurationMS,
			&sl.FirstActiveMS, &sl.LastActiveMS, &sl.ErrorCount, &sl.ErrorRate,
			&totalTokens, &cost, &costCurrency,
		)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		if totalTokens.Valid {
			v := uint32(totalTokens.Int64)
			sl.TotalTokens = &v
		}
		if cost.Valid {
			v := cost.Float64
			sl.Cost = &v
		}
		if costCurrency.Valid {
			sl.CostCurrency = costCurrency.String
		}
		sessions = append(sessions, sl)
	}

	if sessions == nil {
		sessions = []SessionListItem{}
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

func (s *sqliteStore) GetSession(ctx context.Context, sessionID string) (*SessionDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Session summary
	var sl SessionListItem
	var totalTokens sql.NullInt64
	var cost sql.NullFloat64
	var costCurrency sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT session_id,
		        count(*) AS trace_count,
		        sum(duration_ms) AS total_duration_ms,
		        max(duration_ms) AS max_duration_ms,
		        round(avg(duration_ms)) AS avg_duration_ms,
		        min(start_time_ms) AS first_active_ms,
		        max(start_time_ms) AS last_active_ms,
		        sum(CASE WHEN status_code='ERROR' THEN 1 ELSE 0 END) AS error_count,
		        round(sum(CASE WHEN status_code='ERROR' THEN 1 ELSE 0 END)*100.0/count(*)) AS error_rate,
		        sum(total_tokens) AS total_tokens,
		        sum(cost) AS cost,
		        CASE WHEN count(DISTINCT cost_currency) > 1 THEN 'mixed' ELSE max(cost_currency) END AS cost_currency
		 FROM traces WHERE session_id = ? GROUP BY session_id`,
		sessionID,
	).Scan(
		&sl.SessionID, &sl.TraceCount, &sl.TotalDurationMS, &sl.MaxDurationMS, &sl.AvgDurationMS,
		&sl.FirstActiveMS, &sl.LastActiveMS, &sl.ErrorCount, &sl.ErrorRate,
		&totalTokens, &cost, &costCurrency,
	)
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}
	if totalTokens.Valid {
		v := uint32(totalTokens.Int64)
		sl.TotalTokens = &v
	}
	if cost.Valid {
		v := cost.Float64
		sl.Cost = &v
	}
	if costCurrency.Valid {
		sl.CostCurrency = costCurrency.String
	}

	// Session traces
	rows, err := s.db.QueryContext(ctx,
		`SELECT trace_id_hex, root_span_id_hex, root_name,
		        json_extract(resource_attributes, '$."service.name"') AS root_service,
		        start_time_ms, duration_ms, span_count, status_code,
		        total_tokens, cost, cost_currency
		 FROM traces WHERE session_id = ? ORDER BY start_time_ms ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query session traces: %w", err)
	}
	defer rows.Close()

	var traces []TraceListItem
	for rows.Next() {
		var t TraceListItem
		var rootService sql.NullString
		var totalTokens sql.NullInt32
		var cost sql.NullFloat64
		var costCurrency sql.NullString
		err := rows.Scan(
			&t.TraceIDHex, &t.RootSpanID, &t.RootName, &rootService,
			&t.StartTimeMS, &t.DurationMS, &t.SpanCount, &t.Status,
			&totalTokens, &cost, &costCurrency,
		)
		if err != nil {
			return nil, fmt.Errorf("scan session trace: %w", err)
		}
		if rootService.Valid {
			t.RootService = rootService.String
		}
		if totalTokens.Valid {
			v := uint32(totalTokens.Int32)
			t.TotalTokens = &v
		}
		if cost.Valid {
			v := cost.Float64
			t.Cost = &v
		}
		if costCurrency.Valid {
			t.CostCurrency = costCurrency.String
		}
		traces = append(traces, t)
	}

	if traces == nil {
		traces = []TraceListItem{}
	}

	return &SessionDetail{Session: sl, Traces: traces}, nil
}

// --- Log methods ---

func (s *sqliteStore) InsertLogs(ctx context.Context, logs []LogRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, l := range logs {
		attrsJSON, _ := json.Marshal(l.Attributes)
		_, err := tx.Exec(
			`INSERT INTO logs (trace_id_hex, span_id_hex, timestamp, severity, event_name, body, attributes)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			TraceIDToHex(l.TraceID), SpanIDToHex(l.SpanID),
			l.Timestamp, l.Severity, l.EventName, l.Body, string(attrsJSON),
		)
		if err != nil {
			return fmt.Errorf("insert log: %w", err)
		}
	}

	return tx.Commit()
}

func (s *sqliteStore) ListLogs(ctx context.Context, q LogQuery) (*LogListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	where, args := buildSqliteLogWhereClause(q)

	// Count
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM logs`+where, args...,
	).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("count logs: %w", err)
	}

	// List
	offset := (q.Page - 1) * q.PageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT trace_id_hex, span_id_hex, timestamp, severity, event_name, body, attributes
		 FROM logs`+where+` ORDER BY timestamp DESC LIMIT ? OFFSET ?`,
		append(args, q.PageSize, offset)...,
	)
	if err != nil {
		return nil, fmt.Errorf("list logs: %w", err)
	}
	defer rows.Close()

	var logs []LogListItem
	for rows.Next() {
		var l LogListItem
		var attrsJSON string
		err := rows.Scan(
			&l.TraceIDHex, &l.SpanIDHex, &l.Timestamp, &l.Severity,
			&l.EventName, &l.Body, &attrsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		l.Attributes = jsonToMap(attrsJSON)
		logs = append(logs, l)
	}

	if logs == nil {
		logs = []LogListItem{}
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

func (s *sqliteStore) GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]LogListItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT trace_id_hex, span_id_hex, timestamp, severity, event_name, body, attributes
		 FROM logs WHERE trace_id_hex = ? ORDER BY timestamp ASC`,
		TraceIDToHex(traceID),
	)
	if err != nil {
		return nil, fmt.Errorf("query logs by trace: %w", err)
	}
	defer rows.Close()

	var logs []LogListItem
	for rows.Next() {
		var l LogListItem
		var attrsJSON string
		err := rows.Scan(
			&l.TraceIDHex, &l.SpanIDHex, &l.Timestamp, &l.Severity,
			&l.EventName, &l.Body, &attrsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		l.Attributes = jsonToMap(attrsJSON)
		logs = append(logs, l)
	}

	if logs == nil {
		logs = []LogListItem{}
	}
	return logs, nil
}

// GetLogCountsByTrace returns the per-span log count for a trace.
func (s *sqliteStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT span_id_hex, COUNT(*) FROM logs WHERE trace_id_hex = ? GROUP BY span_id_hex`,
		TraceIDToHex(traceID),
	)
	if err != nil {
		return nil, fmt.Errorf("count logs by trace: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var spanIDHex string
		var n int
		if err := rows.Scan(&spanIDHex, &n); err != nil {
			return nil, fmt.Errorf("scan log count: %w", err)
		}
		counts[spanIDHex] = n
	}
	return counts, nil
}

func (s *sqliteStore) GetLogEventNames(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT event_name FROM logs WHERE event_name != '' ORDER BY event_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query event names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan event name: %w", err)
		}
		names = append(names, n)
	}
	return names, nil
}

// --- Pricing methods ---

func (s *sqliteStore) GetModelPricing(ctx context.Context) ([]ModelPricing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT model_name, input_price, output_price, currency FROM model_pricing ORDER BY model_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query pricing: %w", err)
	}
	defer rows.Close()

	var pricing []ModelPricing
	for rows.Next() {
		var p ModelPricing
		if err := rows.Scan(&p.ModelName, &p.InputPrice, &p.OutputPrice, &p.Currency); err != nil {
			return nil, fmt.Errorf("scan pricing: %w", err)
		}
		pricing = append(pricing, p)
	}
	return pricing, nil
}

func (s *sqliteStore) UpsertModelPricing(ctx context.Context, p ModelPricing) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO model_pricing (model_name, input_price, output_price, currency)
		 VALUES (?, ?, ?, ?)`,
		p.ModelName, p.InputPrice, p.OutputPrice, p.Currency,
	)
	return err
}

func (s *sqliteStore) DeleteModelPricing(ctx context.Context, modelName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx,
		`DELETE FROM model_pricing WHERE model_name = ?`, modelName,
	)
	return err
}

// --- LLMConfig methods ---

func (s *sqliteStore) GetLLMConfigs(ctx context.Context) ([]LLMConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, model_name, provider_type, provider_url, api_key, is_default, temperature, max_tokens
		 FROM llm_configs ORDER BY model_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query llm configs: %w", err)
	}
	defer rows.Close()

	var configs []LLMConfig
	for rows.Next() {
		var c LLMConfig
		var isDefault int
		if err := rows.Scan(&c.ID, &c.ModelName, &c.ProviderType, &c.ProviderURL, &c.APIKey, &isDefault, &c.Temperature, &c.MaxTokens); err != nil {
			return nil, fmt.Errorf("scan llm config: %w", err)
		}
		c.IsDefault = isDefault != 0
		configs = append(configs, c)
	}
	return configs, nil
}

func (s *sqliteStore) CreateLLMConfig(ctx context.Context, c *LLMConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If setting as default, clear other defaults first
	if c.IsDefault {
		_, err := s.db.ExecContext(ctx, `UPDATE llm_configs SET is_default = 0 WHERE is_default = 1`)
		if err != nil {
			return fmt.Errorf("clear defaults: %w", err)
		}
	}

	isDefault := 0
	if c.IsDefault {
		isDefault = 1
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_configs (id, model_name, provider_type, provider_url, api_key, is_default, temperature, max_tokens)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ModelName, c.ProviderType, c.ProviderURL, c.APIKey, isDefault, c.Temperature, c.MaxTokens,
	)
	return err
}

func (s *sqliteStore) UpdateLLMConfig(ctx context.Context, c *LLMConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If API key contains "***" (masked sentinel), preserve existing key
	apiKey := c.APIKey
	if strings.Contains(apiKey, "***") {
		var existingKey string
		err := s.db.QueryRow(`SELECT api_key FROM llm_configs WHERE id = ?`, c.ID).Scan(&existingKey)
		if err != nil {
			return fmt.Errorf("fetch existing key: %w", err)
		}
		apiKey = existingKey
	}

	// If setting as default, clear other defaults first
	if c.IsDefault {
		_, err := s.db.ExecContext(ctx, `UPDATE llm_configs SET is_default = 0 WHERE is_default = 1`)
		if err != nil {
			return fmt.Errorf("clear defaults: %w", err)
		}
	}

	isDefault := 0
	if c.IsDefault {
		isDefault = 1
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE llm_configs SET model_name=?, provider_type=?, provider_url=?, api_key=?, is_default=?, temperature=?, max_tokens=?
		 WHERE id=?`,
		c.ModelName, c.ProviderType, c.ProviderURL, apiKey, isDefault, c.Temperature, c.MaxTokens, c.ID,
	)
	return err
}

func (s *sqliteStore) DeleteLLMConfig(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `DELETE FROM llm_configs WHERE id = ?`, id)
	return err
}

// --- Cost methods ---

func (s *sqliteStore) UpdateTraceCost(ctx context.Context, traceID [16]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	traceIDHex := TraceIDToHex(traceID)

	pricingRows, err := s.db.QueryContext(ctx,
		`SELECT model_name, input_price, output_price, currency FROM model_pricing`)
	if err != nil {
		return fmt.Errorf("query pricing: %w", err)
	}
	pricingMap := make(map[string]ModelPricing)
	for pricingRows.Next() {
		var p ModelPricing
		if err := pricingRows.Scan(&p.ModelName, &p.InputPrice, &p.OutputPrice, &p.Currency); err != nil {
			pricingRows.Close()
			return fmt.Errorf("scan pricing: %w", err)
		}
		pricingMap[p.ModelName] = p
	}
	pricingRows.Close()

	rows, err := s.db.QueryContext(ctx,
		`SELECT span_id_hex, input_tokens, output_tokens, total_tokens,
		        cache_creation_tokens, cache_read_tokens, gen_ai_request_model
		 FROM spans WHERE trace_id_hex = ? AND total_tokens IS NOT NULL`,
		traceIDHex)
	if err != nil {
		return fmt.Errorf("query span tokens: %w", err)
	}

	const cacheCreateRate = 1.25
	const cacheReadRate = 0.1

	var totalCost float64
	var traceTotalTokens uint64
	var currency string
	var unpricedCount int
	type spanCostRow struct {
		spanID       string
		cost         float64
		costCurrency string
	}
	var spanCosts []spanCostRow

	for rows.Next() {
		var spanID string
		var inputTokens, outputTokens, totalTokens, cacheCreationTokens, cacheReadTokens sql.NullInt32
		var genAIModel sql.NullString
		if err := rows.Scan(&spanID, &inputTokens, &outputTokens, &totalTokens,
			&cacheCreationTokens, &cacheReadTokens, &genAIModel); err != nil {
			rows.Close()
			return fmt.Errorf("scan span token: %w", err)
		}
		// Trace total = sum of all span total_tokens (pricing-independent).
		if totalTokens.Valid {
			traceTotalTokens += uint64(totalTokens.Int32)
		}

		modelName := "(unknown)"
		if genAIModel.Valid {
			modelName = genAIModel.String
		}
		p, ok := pricingMap[modelName]
		if !ok {
			unpricedCount++
			spanCosts = append(spanCosts, spanCostRow{spanID: spanID})
			continue
		}

		inT := uint32(0)
		if inputTokens.Valid {
			inT = uint32(inputTokens.Int32)
		}
		outT := uint32(0)
		if outputTokens.Valid {
			outT = uint32(outputTokens.Int32)
		}
		ccT := uint32(0)
		if cacheCreationTokens.Valid {
			ccT = uint32(cacheCreationTokens.Int32)
		}
		crT := uint32(0)
		if cacheReadTokens.Valid {
			crT = uint32(cacheReadTokens.Int32)
		}

		spanCost := (float64(inT)*p.InputPrice +
			float64(ccT)*p.InputPrice*cacheCreateRate +
			float64(crT)*p.InputPrice*cacheReadRate +
			float64(outT)*p.OutputPrice) / 1_000_000.0
		spanCost = math.Round(spanCost*1e6) / 1e6
		totalCost += spanCost
		if currency == "" {
			currency = p.Currency
		}
		spanCosts = append(spanCosts, spanCostRow{spanID: spanID, cost: spanCost, costCurrency: p.Currency})
	}
	rows.Close()

	// Write per-span cost (including 0 for unpriced, so by_model has no NULLs).
	for _, sc := range spanCosts {
		s.db.ExecContext(ctx,
			`UPDATE spans SET cost = ?, cost_currency = ? WHERE trace_id_hex = ? AND span_id_hex = ?`,
			sc.cost, sc.costCurrency, traceIDHex, sc.spanID)
	}

	if totalCost == 0 && unpricedCount == 0 {
		// Still set trace total_tokens even with no cost.
		if traceTotalTokens > 0 {
			s.db.ExecContext(ctx, `UPDATE traces SET total_tokens = ? WHERE trace_id_hex = ?`,
				traceTotalTokens, traceIDHex)
		}
		return nil
	}

	totalCost = math.Round(totalCost*1e6) / 1e6
	_, err = s.db.ExecContext(ctx,
		`UPDATE traces SET total_tokens = ?, cost = ?, cost_currency = ? WHERE trace_id_hex = ?`,
		traceTotalTokens, totalCost, currency, traceIDHex)
	return err
}

func (s *sqliteStore) GetCostSummary(ctx context.Context, q CostQuery) (*CostSummaryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Overview cost + count + currency from traces (no join, no fan-out).
	var overview CostOverview
	var currency string
	err := s.db.QueryRowContext(ctx,
		`SELECT
		    COALESCE(sum(cost), 0),
		    count(*),
		    COALESCE(max(cost_currency), 'USD')
		 FROM traces
		 WHERE start_time_ms >= ? AND start_time_ms <= ?
		   AND cost IS NOT NULL AND cost > 0`,
		q.StartTimeMS, q.EndTimeMS,
	).Scan(&overview.TotalCost, &overview.TraceCount, &currency)
	if err != nil {
		return nil, fmt.Errorf("query cost overview: %w", err)
	}
	if overview.TraceCount > 0 {
		overview.AvgCostPerTrace = math.Round(overview.TotalCost/float64(overview.TraceCount)*1e6) / 1e6
	}

	// Token buckets from spans (the single source of truth).
	err = s.db.QueryRowContext(ctx,
		`SELECT
		    COALESCE(sum(input_tokens), 0),
		    COALESCE(sum(cache_creation_tokens), 0),
		    COALESCE(sum(cache_read_tokens), 0),
		    COALESCE(sum(output_tokens), 0)
		 FROM spans
		 WHERE total_tokens IS NOT NULL
		   AND trace_id_hex IN (
		      SELECT trace_id_hex FROM traces
		      WHERE start_time_ms >= ? AND start_time_ms <= ?
		        AND cost IS NOT NULL AND cost > 0
		   )`,
		q.StartTimeMS, q.EndTimeMS,
	).Scan(&overview.TotalInputTokens, &overview.TotalCacheCreationTokens,
		&overview.TotalCacheReadTokens, &overview.TotalOutputTokens)
	if err != nil {
		// Non-critical: leave buckets at zero.
		overview.TotalInputTokens = 0
		overview.TotalCacheCreationTokens = 0
		overview.TotalCacheReadTokens = 0
		overview.TotalOutputTokens = 0
	}
	overview.TotalTokens = overview.TotalInputTokens + overview.TotalCacheCreationTokens +
		overview.TotalCacheReadTokens + overview.TotalOutputTokens

	// by_service: join spans→traces for service.name (service lives on the
	// trace resource, not on spans). Only compute when requested.
	if q.GroupBy == "service" {
		rows, err := s.db.QueryContext(ctx,
			`SELECT
			    COALESCE(json_extract(t.resource_attributes, '$."service.name"'), '(unknown)') AS service,
			    COALESCE(sum(s.cost), 0) AS cost,
			    COALESCE(sum(s.input_tokens), 0) AS input_tokens,
			    COALESCE(sum(s.cache_creation_tokens), 0) AS cache_creation_tokens,
			    COALESCE(sum(s.cache_read_tokens), 0) AS cache_read_tokens,
			    COALESCE(sum(s.output_tokens), 0) AS output_tokens,
			    count(DISTINCT s.trace_id_hex) AS trace_count
			 FROM spans s
			 JOIN traces t ON t.trace_id_hex = s.trace_id_hex
			 WHERE s.total_tokens IS NOT NULL
			   AND t.start_time_ms >= ? AND t.start_time_ms <= ?
			   AND t.cost IS NOT NULL AND t.cost > 0
			 GROUP BY json_extract(t.resource_attributes, '$."service.name"')
			 ORDER BY cost DESC`,
			q.StartTimeMS, q.EndTimeMS,
		)
		if err != nil {
			return nil, fmt.Errorf("query cost by service: %w", err)
		}
		defer rows.Close()

		var byService []ServiceCostItem
		for rows.Next() {
			var sc ServiceCostItem
			if err := rows.Scan(&sc.Service, &sc.Cost, &sc.InputTokens, &sc.CacheCreationTokens,
				&sc.CacheReadTokens, &sc.OutputTokens, &sc.TraceCount); err != nil {
				return nil, fmt.Errorf("scan service cost: %w", err)
			}
			sc.Tokens = sc.InputTokens + sc.CacheCreationTokens + sc.CacheReadTokens + sc.OutputTokens
			if sc.TraceCount > 0 {
				sc.AvgCost = math.Round(sc.Cost/float64(sc.TraceCount)*1e6) / 1e6
			}
			byService = append(byService, sc)
		}
		if byService == nil {
			byService = []ServiceCostItem{}
		}

		return &CostSummaryResult{
			Currency:  currency,
			Overview:  overview,
			GroupBy:   "service",
			ByService: byService,
		}, nil
	}

	// by_model: group spans directly (no fan-out join).
	rows, err := s.db.QueryContext(ctx,
		`SELECT
		    COALESCE(gen_ai_request_model, '(unknown)') AS model,
		    COALESCE(sum(cost), 0) AS cost,
		    COALESCE(sum(input_tokens), 0) AS input_tokens,
		    COALESCE(sum(cache_creation_tokens), 0) AS cache_creation_tokens,
		    COALESCE(sum(cache_read_tokens), 0) AS cache_read_tokens,
		    COALESCE(sum(output_tokens), 0) AS output_tokens,
		    count(DISTINCT trace_id_hex) AS trace_count
		 FROM spans
		 WHERE total_tokens IS NOT NULL
		   AND trace_id_hex IN (
		      SELECT trace_id_hex FROM traces
		      WHERE start_time_ms >= ? AND start_time_ms <= ?
		        AND cost IS NOT NULL AND cost > 0
		   )
		 GROUP BY gen_ai_request_model
		 ORDER BY cost DESC`,
		q.StartTimeMS, q.EndTimeMS,
	)
	if err != nil {
		return nil, fmt.Errorf("query cost by model: %w", err)
	}
	defer rows.Close()

	var byModel []ModelCostItem
	for rows.Next() {
		var m ModelCostItem
		if err := rows.Scan(&m.Model, &m.Cost, &m.InputTokens, &m.CacheCreationTokens,
			&m.CacheReadTokens, &m.OutputTokens, &m.TraceCount); err != nil {
			return nil, fmt.Errorf("scan model cost: %w", err)
		}
		m.Tokens = m.InputTokens + m.CacheCreationTokens + m.CacheReadTokens + m.OutputTokens
		if m.TraceCount > 0 {
			m.AvgCost = math.Round(m.Cost/float64(m.TraceCount)*1e6) / 1e6
		}
		byModel = append(byModel, m)
	}
	if byModel == nil {
		byModel = []ModelCostItem{}
	}

	return &CostSummaryResult{
		Currency: currency,
		Overview: overview,
		GroupBy:  "model",
		ByModel:  byModel,
	}, nil
}

// --- Diagnosis methods ---

func (s *sqliteStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*DiagnosisResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	traceIDHex := TraceIDToHex(traceID)

	var dr DiagnosisResult
	var scoresJSON, findingsJSON string
	var stale int
	var createdAt string

	err := s.db.QueryRowContext(ctx,
		`SELECT trace_id_hex, model_name, scores, overall_score, findings, summary,
		        spans_snapshot, raw_response, created_at, stale
		 FROM diagnosis_results WHERE trace_id_hex = ?`,
		traceIDHex,
	).Scan(
		&dr.TraceIDHex, &dr.ModelName, &scoresJSON, &dr.OverallScore, &findingsJSON, &dr.Summary,
		&dr.SpansSnapshot, &dr.RawResponse, &createdAt, &stale,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query diagnosis: %w", err)
	}

	dr.TraceID = traceID
	dr.Stale = stale != 0

	if err := json.Unmarshal([]byte(scoresJSON), &dr.Scores); err != nil {
		dr.Scores = DiagnosisScores{}
	}
	if err := json.Unmarshal([]byte(findingsJSON), &dr.Findings); err != nil {
		dr.Findings = []DiagnosisFinding{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		dr.CreatedAt = t
	}

	return &dr, nil
}

func (s *sqliteStore) UpsertDiagnosisResult(ctx context.Context, result *DiagnosisResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scoresJSON, _ := json.Marshal(result.Scores)
	findingsJSON, _ := json.Marshal(result.Findings)
	stale := 0
	if result.Stale {
		stale = 1
	}
	createdAt := result.CreatedAt.Format(time.RFC3339)
	if createdAt == "" || result.CreatedAt.IsZero() {
		createdAt = time.Now().Format(time.RFC3339)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO diagnosis_results
		 (trace_id_hex, model_name, scores, overall_score, findings, summary,
		  spans_snapshot, raw_response, created_at, stale)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		TraceIDToHex(result.TraceID), result.ModelName, string(scoresJSON), result.OverallScore,
		string(findingsJSON), result.Summary, result.SpansSnapshot, result.RawResponse,
		createdAt, stale,
	)
	return err
}

// --- Purge ---

func (s *sqliteStore) Purge(ctx context.Context, maxAge time.Duration, maxCount int) (deletedTraces int, deletedSpans int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowMS := uint64(time.Now().UnixMilli())

	// Phase 1: Delete by age
	if maxAge > 0 {
		cutoffMS := nowMS - uint64(maxAge.Milliseconds())
		result, e := s.db.ExecContext(ctx,
			`DELETE FROM traces WHERE start_time_ms < ?`, cutoffMS,
		)
		if e != nil {
			return 0, 0, fmt.Errorf("purge by age: %w", e)
		}
		affected, _ := result.RowsAffected()
		deletedTraces += int(affected)
	}

	// Phase 2: Delete by count
	if maxCount > 0 {
		// Count remaining traces
		var remaining int
		s.db.QueryRowContext(ctx, `SELECT count(*) FROM traces`).Scan(&remaining)

		if remaining > maxCount {
			result, e := s.db.ExecContext(ctx,
				`DELETE FROM traces WHERE trace_id_hex NOT IN (
				    SELECT trace_id_hex FROM traces ORDER BY start_time_ms DESC LIMIT ?
				)`,
				maxCount,
			)
			if e != nil {
				return deletedTraces, 0, fmt.Errorf("purge by count: %w", e)
			}
			affected, _ := result.RowsAffected()
			deletedTraces += int(affected)
		}
	}

	// Clean orphaned spans and logs
	result, e := s.db.ExecContext(ctx,
		`DELETE FROM spans WHERE trace_id_hex NOT IN (SELECT trace_id_hex FROM traces)`,
	)
	if e != nil {
		return deletedTraces, 0, fmt.Errorf("purge orphan spans: %w", e)
	}
	affected, _ := result.RowsAffected()
	deletedSpans += int(affected)

	result, e = s.db.ExecContext(ctx,
		`DELETE FROM logs WHERE trace_id_hex NOT IN (SELECT trace_id_hex FROM traces)`,
	)
	if e != nil {
		return deletedTraces, deletedSpans, fmt.Errorf("purge orphan logs: %w", e)
	}

	return deletedTraces, deletedSpans, nil
}

// --- Close ---

func (s *sqliteStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*AgentStats, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// --- Helper functions for WHERE clause construction ---

func buildSqliteTraceWhereClause(q TraceQuery) (string, []interface{}) {
	var clauses []string
	var args []interface{}

	if q.Service != "" {
		clauses = append(clauses, `json_extract(resource_attributes, '$."service.name"') = ?`)
		args = append(args, q.Service)
	}
	if q.Status != "" {
		clauses = append(clauses, `status_code = ?`)
		args = append(args, q.Status)
	}
	if q.Query != "" {
		clauses = append(clauses, `root_name LIKE ?`)
		args = append(args, "%"+q.Query+"%")
	}
	if q.StartTimeMS > 0 {
		clauses = append(clauses, `start_time_ms >= ?`)
		args = append(args, q.StartTimeMS)
	}
	if q.EndTimeMS > 0 {
		clauses = append(clauses, `start_time_ms <= ?`)
		args = append(args, q.EndTimeMS)
	}
	if q.MinDuration > 0 {
		clauses = append(clauses, `duration_ms >= ?`)
		args = append(args, q.MinDuration)
	}
	if q.MaxDuration > 0 {
		clauses = append(clauses, `duration_ms <= ?`)
		args = append(args, q.MaxDuration)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

func buildSqliteSessionWhereClause(q SessionQuery) (string, []interface{}) {
	var clauses []string
	var args []interface{}

	if q.Service != "" {
		clauses = append(clauses, `json_extract(resource_attributes, '$."service.name"') = ?`)
		args = append(args, q.Service)
	}
	if q.Query != "" {
		clauses = append(clauses, `session_id LIKE ?`)
		args = append(args, "%"+q.Query+"%")
	}
	if q.StartTimeMS > 0 {
		clauses = append(clauses, `start_time_ms >= ?`)
		args = append(args, q.StartTimeMS)
	}
	if q.EndTimeMS > 0 {
		clauses = append(clauses, `start_time_ms <= ?`)
		args = append(args, q.EndTimeMS)
	}

	where := ""
	if len(clauses) > 0 {
		where = " AND " + strings.Join(clauses, " AND ")
	}
	return where, args
}

func buildSqliteLogWhereClause(q LogQuery) (string, []interface{}) {
	var clauses []string
	var args []interface{}

	if q.Severity != "" {
		clauses = append(clauses, `severity = ?`)
		args = append(args, q.Severity)
	}
	if q.EventName != "" {
		clauses = append(clauses, `event_name = ?`)
		args = append(args, q.EventName)
	}
	if q.Query != "" {
		clauses = append(clauses, `body LIKE ?`)
		args = append(args, "%"+q.Query+"%")
	}
	if q.TraceID != [16]byte{} {
		clauses = append(clauses, `trace_id_hex = ?`)
		args = append(args, TraceIDToHex(q.TraceID))
	}
	if q.SpanID != [8]byte{} {
		clauses = append(clauses, `span_id_hex = ?`)
		args = append(args, SpanIDToHex(q.SpanID))
	}
	if q.StartTime > 0 {
		clauses = append(clauses, `timestamp >= ?`)
		args = append(args, q.StartTime)
	}
	if q.EndTime > 0 {
		clauses = append(clauses, `timestamp <= ?`)
		args = append(args, q.EndTime)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

// jsonToMap parses a JSON string into a map[string]string.
func jsonToMap(raw string) map[string]string {
	if raw == "" || raw == "{}" {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]string{}
	}
	return m
}
