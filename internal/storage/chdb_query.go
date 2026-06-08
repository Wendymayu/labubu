package storage

import (
	"fmt"
	"strings"
)

// buildInsertSpanSQL builds an INSERT statement for a single span.
func buildInsertSpanSQL(span Span) string {
	inputTokens := nullUint32(span.InputTokens)
	outputTokens := nullUint32(span.OutputTokens)
	totalTokens := nullUint32(span.TotalTokens)
	genAIModel := nullString(span.GenAIRequestModel)

	return fmt.Sprintf(
		`INSERT INTO spans (
			trace_id, span_id, parent_span_id, trace_state, name, kind,
			start_time_ms, end_time_ms, duration_ms,
			attributes, dropped_attributes_count,
			events, dropped_events_count,
			links, dropped_links_count,
			status_code, status_message,
			input_tokens, output_tokens, total_tokens, gen_ai_request_model
		) VALUES (
			unhex('%x'), unhex('%x'), unhex('%x'), '%s', '%s', %d,
			%d, %d, %d,
			%s, 0,
			'%s', 0,
			'%s', 0,
			%d, '%s',
			%s, %s, %s, %s
		)`,
		span.TraceID, span.SpanID, span.ParentSpanID,
		escapeSQL(span.TraceState),
		escapeSQL(span.Name), span.Kind,
		span.StartTimeMS, span.EndTimeMS, span.DurationMS,
		mapToSQL(span.Attributes),
		escapeSQL(span.Events),
		escapeSQL(span.Links),
		span.StatusCode, escapeSQL(span.StatusMessage),
		inputTokens, outputTokens, totalTokens, genAIModel,
	)
}

// buildUpsertTraceSQL builds an INSERT that handles duplicate trace_id
// by replacing with the latest data.
func buildUpsertTraceSQL(trace Trace) string {
	totalTokens := nullUint32(trace.TotalTokens)

	return fmt.Sprintf(
		`INSERT INTO traces (
			trace_id, trace_id_hex, root_span_id, root_name, span_count,
			start_time_ms, end_time_ms, duration_ms,
			resource_attributes, resource_schema_url,
			scope_name, scope_version, scope_attributes, scope_schema_url,
			trace_state, dropped_span_count,
			status_code, status_message, total_tokens, session_id
		) VALUES (
			unhex('%s'), '%s', unhex('%s'), '%s', %d,
			%d, %d, %d,
			%s, '%s',
			'%s', '%s', %s, '%s',
			'%s', 0,
			%d, '%s', %s, '%s'
		)`,
		trace.TraceIDHex, trace.TraceIDHex,
		trace.RootSpanID,
		escapeSQL(trace.RootName), trace.SpanCount,
		trace.StartTimeMS, trace.EndTimeMS, trace.DurationMS,
		mapToSQL(trace.ResourceAttrs), escapeSQL(trace.ResourceSchemaURL),
		escapeSQL(trace.ScopeName), escapeSQL(trace.ScopeVersion),
		mapToSQL(trace.ScopeAttrs), escapeSQL(trace.ScopeSchemaURL),
		"",
		trace.StatusCode, escapeSQL(trace.StatusMessage),
		totalTokens, escapeSQL(trace.SessionID),
	)
}

// buildTraceCountSQL builds a count query matching the given filters.
func buildTraceCountSQL(q TraceQuery) string {
	return "SELECT count(*) AS count FROM traces" + buildTraceWhereClause(q)
}

// buildTraceListSQL builds a list query with filters, ordering, and pagination.
func buildTraceListSQL(q TraceQuery) string {
	offset := (q.Page - 1) * q.PageSize
	return fmt.Sprintf(
		`SELECT
			trace_id_hex, root_name, root_span_id,
			resource_attributes['service.name'] AS root_service,
			start_time_ms, duration_ms, span_count,
			toString(status_code) AS status,
			total_tokens, cost, cost_currency
		FROM traces%s
		ORDER BY start_time_ms DESC
		LIMIT %d OFFSET %d`,
		buildTraceWhereClause(q), q.PageSize, offset,
	)
}

// buildTraceWhereClause builds the WHERE clause for trace queries.
func buildTraceWhereClause(q TraceQuery) string {
	var clauses []string

	if q.Service != "" {
		clauses = append(clauses, fmt.Sprintf(
			"resource_attributes['service.name'] = '%s'", escapeSQL(q.Service),
		))
	}
	if q.Status != "" {
		clauses = append(clauses, fmt.Sprintf(
			"status_code = '%s'", escapeSQL(q.Status),
		))
	}
	if q.Query != "" {
		clauses = append(clauses, fmt.Sprintf(
			"root_name LIKE '%%%s%%'", escapeSQL(q.Query),
		))
	}
	if q.StartTimeMS > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"start_time_ms >= %d", q.StartTimeMS,
		))
	}
	if q.EndTimeMS > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"start_time_ms <= %d", q.EndTimeMS,
		))
	}
	if q.MinDuration > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"duration_ms >= %d", q.MinDuration,
		))
	}
	if q.MaxDuration > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"duration_ms <= %d", q.MaxDuration,
		))
	}

	if len(clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(clauses, " AND ")
}

// buildGetTraceSQL builds a query to fetch all spans for a trace.
func buildGetTraceSQL(traceID [16]byte) string {
	return fmt.Sprintf(
		`SELECT
			hex(span_id) AS span_id,
			hex(parent_span_id) AS parent_span_id,
			name,
			toString(kind) AS kind,
			start_time_ms,
			end_time_ms,
			duration_ms,
			attributes,
			events,
			links,
			toString(status_code) AS status_code,
			status_message,
			input_tokens,
			output_tokens,
			total_tokens,
			gen_ai_request_model
		FROM spans
		WHERE trace_id = unhex('%x')
		ORDER BY start_time_ms`,
		traceID,
	)
}

// buildGetTraceMetaSQL builds a query to fetch trace-level metadata.
func buildGetTraceMetaSQL(traceID [16]byte) string {
	return fmt.Sprintf(
		`SELECT
			resource_attributes,
			resource_schema_url,
			scope_name,
			scope_version,
			scope_attributes,
			cost, cost_currency
		FROM traces
		WHERE trace_id = unhex('%x')
		LIMIT 1`,
		traceID,
	)
}

// buildSessionListWhereClause builds the WHERE clause for session queries.
func buildSessionListWhereClause(q SessionQuery) string {
	clauses := []string{"session_id != ''"}

	if q.Service != "" {
		clauses = append(clauses, fmt.Sprintf(
			"resource_attributes['service.name'] = '%s'", escapeSQL(q.Service),
		))
	}
	if q.Query != "" {
		clauses = append(clauses, fmt.Sprintf(
			"session_id LIKE '%%%s%%'", escapeSQL(q.Query),
		))
	}
	if q.StartTimeMS > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"start_time_ms >= %d", q.StartTimeMS,
		))
	}
	if q.EndTimeMS > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"start_time_ms <= %d", q.EndTimeMS,
		))
	}

	return " WHERE " + strings.Join(clauses, " AND ")
}

// buildSessionCountSQL builds a count query for distinct sessions.
func buildSessionCountSQL(q SessionQuery) string {
	return "SELECT count() AS count FROM (SELECT DISTINCT session_id FROM traces" + buildSessionListWhereClause(q) + ")"
}

// buildSessionListSQL builds a query that aggregates session metrics.
func buildSessionListSQL(q SessionQuery) string {
	offset := (q.Page - 1) * q.PageSize
	return fmt.Sprintf(
		`SELECT
			session_id,
			count() AS trace_count,
			sum(total_tokens) AS total_tokens,
			sum(cost) AS cost,
			any(cost_currency) AS cost_currency,
			sum(duration_ms) AS total_duration_ms,
			max(duration_ms) AS max_duration_ms,
			round(avg(duration_ms), 1) AS avg_duration_ms,
			sum(if(status_code = 'ERROR', 1, 0)) AS error_count,
			round(sum(if(status_code = 'ERROR', 1, 0)) / count(), 4) AS error_rate,
			min(start_time_ms) AS first_active_ms,
			max(start_time_ms) AS last_active_ms
		FROM traces%s
		GROUP BY session_id
		ORDER BY last_active_ms DESC
		LIMIT %d OFFSET %d`,
		buildSessionListWhereClause(q), q.PageSize, offset,
	)
}

// buildSessionSummarySQL builds a query for a single session's aggregated summary.
func buildSessionSummarySQL(sessionID string) string {
	return fmt.Sprintf(
		`SELECT
			session_id,
			count() AS trace_count,
			sum(total_tokens) AS total_tokens,
			sum(cost) AS cost,
			any(cost_currency) AS cost_currency,
			sum(duration_ms) AS total_duration_ms,
			max(duration_ms) AS max_duration_ms,
			round(avg(duration_ms), 1) AS avg_duration_ms,
			sum(if(status_code = 'ERROR', 1, 0)) AS error_count,
			round(sum(if(status_code = 'ERROR', 1, 0)) / count(), 4) AS error_rate,
			min(start_time_ms) AS first_active_ms,
			max(start_time_ms) AS last_active_ms
		FROM traces
		WHERE session_id = '%s'
		GROUP BY session_id`,
		escapeSQL(sessionID),
	)
}

// buildSessionTracesSQL builds a query to fetch all traces for a session.
func buildSessionTracesSQL(sessionID string) string {
	return fmt.Sprintf(
		`SELECT
			trace_id_hex, root_name, root_span_id,
			resource_attributes['service.name'] AS root_service,
			start_time_ms, duration_ms, span_count,
			toString(status_code) AS status,
			total_tokens, cost, cost_currency
		FROM traces
		WHERE session_id = '%s'
		ORDER BY start_time_ms ASC`,
		escapeSQL(sessionID),
	)
}

// buildInsertLogSQL builds an INSERT statement for a single log record.
func buildInsertLogSQL(l LogRecord) string {
	return fmt.Sprintf(
		`INSERT INTO logs (trace_id, span_id, timestamp, severity, event_name, body, attributes) VALUES (
			unhex('%x'), unhex('%x'), %d, '%s', '%s', '%s', %s
		)`,
		l.TraceID, l.SpanID, l.Timestamp,
		escapeSQL(l.Severity), escapeSQL(l.EventName),
		escapeSQL(l.Body), mapToSQL(l.Attributes),
	)
}

// buildLogCountSQL builds a count query for logs matching the filters.
func buildLogCountSQL(q LogQuery) string {
	return "SELECT count(*) AS count FROM logs" + buildLogWhereClause(q)
}

// buildLogListSQL builds a list query for logs with filters, ordering, and pagination.
func buildLogListSQL(q LogQuery) string {
	offset := (q.Page - 1) * q.PageSize
	return fmt.Sprintf(
		`SELECT
			hex(trace_id) AS trace_id_hex,
			hex(span_id) AS span_id_hex,
			timestamp, severity, event_name, body, attributes
		FROM logs%s
		ORDER BY timestamp DESC
		LIMIT %d OFFSET %d`,
		buildLogWhereClause(q), q.PageSize, offset,
	)
}

// buildLogWhereClause builds the WHERE clause for log queries.
func buildLogWhereClause(q LogQuery) string {
	var clauses []string

	if q.Severity != "" {
		clauses = append(clauses, fmt.Sprintf(
			"severity = '%s'", escapeSQL(q.Severity),
		))
	}
	if q.EventName != "" {
		clauses = append(clauses, fmt.Sprintf(
			"event_name = '%s'", escapeSQL(q.EventName),
		))
	}
	if q.Query != "" {
		clauses = append(clauses, fmt.Sprintf(
			"body LIKE '%%%s%%'", escapeSQL(q.Query),
		))
	}
	var zeroTrace [16]byte
	if q.TraceID != zeroTrace {
		clauses = append(clauses, fmt.Sprintf(
			"trace_id = unhex('%x')", q.TraceID,
		))
	}
	if q.StartTime > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"timestamp >= %d", q.StartTime,
		))
	}
	if q.EndTime > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"timestamp <= %d", q.EndTime,
		))
	}

	if len(clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(clauses, " AND ")
}

// buildGetLogsByTraceSQL builds a query to fetch all logs for a trace.
func buildGetLogsByTraceSQL(traceID [16]byte) string {
	return fmt.Sprintf(
		`SELECT
			hex(trace_id) AS trace_id_hex,
			hex(span_id) AS span_id_hex,
			timestamp, severity, event_name, body, attributes
		FROM logs
		WHERE trace_id = unhex('%x')
		ORDER BY timestamp ASC`,
		traceID,
	)
}

// buildLogEventNamesSQL builds a query to fetch distinct event_name values.
func buildLogEventNamesSQL() string {
	return `SELECT DISTINCT event_name FROM logs WHERE event_name != '' ORDER BY event_name`
}

// --- SQL helpers ---

// escapeSQL escapes backslashes and single quotes for SQL string literals.
func escapeSQL(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "'", "\\'")
}

// nullUint32 formats a *uint32 for SQL: NULL or the numeric value.
func nullUint32(v *uint32) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprintf("%d", *v)
}

// nullString formats a *string for SQL: NULL or a quoted string.
func nullString(v *string) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprintf("'%s'", escapeSQL(*v))
}

// mapToSQL converts a map[string]string to chDB Map literal syntax.
func mapToSQL(m map[string]string) string {
	if len(m) == 0 {
		return "map()"
	}
	var pairs []string
	for k, v := range m {
		pairs = append(pairs, fmt.Sprintf("'%s': '%s'", escapeSQL(k), escapeSQL(v)))
	}
	return fmt.Sprintf("map(%s)", strings.Join(pairs, ", "))
}

// buildModelPricingSelectSQL builds a query to fetch all pricing entries.
func buildModelPricingSelectSQL() string {
	return `SELECT model_name, input_price, output_price, currency FROM model_pricing ORDER BY model_name`
}

// buildModelPricingUpsertSQL builds an INSERT to add or replace a pricing entry.
func buildModelPricingUpsertSQL(p ModelPricing) string {
	return fmt.Sprintf(
		`INSERT INTO model_pricing (model_name, input_price, output_price, currency) VALUES ('%s', %f, %f, '%s')`,
		escapeSQL(p.ModelName), p.InputPrice, p.OutputPrice, escapeSQL(p.Currency),
	)
}

// buildModelPricingDeleteSQL builds a DELETE for a single pricing entry.
func buildModelPricingDeleteSQL(modelName string) string {
	return fmt.Sprintf(`DELETE FROM model_pricing WHERE model_name = '%s'`, escapeSQL(modelName))
}

// buildUpdateTraceCostSQL updates cost/cost_currency for a single trace.
func buildUpdateTraceCostSQL(traceID [16]byte, cost float64, currency string) string {
	return fmt.Sprintf(
		`ALTER TABLE traces UPDATE cost = %f, cost_currency = '%s' WHERE trace_id = unhex('%x')`,
		cost, escapeSQL(currency), traceID,
	)
}

// buildSelectSpanTokensSQL fetches tokens + model info for all spans in a trace.
func buildSelectSpanTokensSQL(traceID [16]byte) string {
	return fmt.Sprintf(
		`SELECT input_tokens, output_tokens, total_tokens, gen_ai_request_model
		FROM spans WHERE trace_id = unhex('%x')`, traceID,
	)
}

// aggregateTraces groups spans by trace_id and produces Trace aggregates.
func aggregateTraces(resource ResourceInfo, scope ScopeInfo, spans []Span) map[[16]byte]Trace {
	traces := make(map[[16]byte]Trace)
	for _, span := range spans {
		t, exists := traces[span.TraceID]
		if !exists {
			t = Trace{
				TraceID:           span.TraceID,
				TraceIDHex:        TraceIDToHex(span.TraceID),
				StartTimeMS:       span.StartTimeMS,
				EndTimeMS:         span.EndTimeMS,
				DurationMS:        span.DurationMS,
				ResourceAttrs:     resource.Attributes,
				ResourceSchemaURL: resource.SchemaURL,
				ScopeName:         scope.Name,
				ScopeVersion:      scope.Version,
				ScopeAttrs:        scope.Attributes,
				ScopeSchemaURL:    scope.SchemaURL,
				StatusCode:        span.StatusCode,
				StatusMessage:     span.StatusMessage,
				TotalTokens:       span.TotalTokens,
			}
		}
		if isRootSpan(span.ParentSpanID) {
			t.RootSpanID = span.SpanID
			t.RootName = span.Name
			t.StatusCode = span.StatusCode
			t.StatusMessage = span.StatusMessage
			if sid, ok := span.Attributes["jiuwenclaw.session.id"]; ok {
				t.SessionID = sid
			}
		}
		t.SpanCount++
		if span.StartTimeMS < t.StartTimeMS {
			t.StartTimeMS = span.StartTimeMS
		}
		if span.EndTimeMS > t.EndTimeMS {
			t.EndTimeMS = span.EndTimeMS
			t.DurationMS = t.EndTimeMS - t.StartTimeMS
		}
		if span.TotalTokens != nil {
			if t.TotalTokens == nil {
				v := *span.TotalTokens
				t.TotalTokens = &v
			} else {
				sum := *t.TotalTokens + *span.TotalTokens
				t.TotalTokens = &sum
			}
		}
		traces[span.TraceID] = t
	}
	return traces
}

func isRootSpan(parentSpanID [8]byte) bool {
	return parentSpanID == [8]byte{}
}
