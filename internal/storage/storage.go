// Package storage defines the trace storage interface and data types.
// Implementations (e.g., chDB via CGO) live in sibling files.
package storage

import (
	"context"
	"fmt"
)

// Span represents a single OTLP span stored in the spans table.
type Span struct {
	TraceID                [16]byte
	SpanID                 [8]byte
	ParentSpanID           [8]byte // all zeros = root span
	Name                   string
	Kind                   int32   // OTel SpanKind enum: 0=UNSPECIFIED, 1=INTERNAL, 2=SERVER, 3=CLIENT, 4=PRODUCER, 5=CONSUMER
	StartTimeMS            uint64
	EndTimeMS              uint64
	DurationMS             uint64
	Attributes             map[string]string
	Events                 string // JSON array of OTel span events
	Links                  string // JSON array of OTel span links
	StatusCode             int32  // 0=UNSET, 1=OK, 2=ERROR
	StatusMessage          string
	InputTokens            *uint32 // nullable — only set for LLM spans
	OutputTokens           *uint32
	TotalTokens            *uint32
	GenAIRequestModel      *string // nullable
	TraceState             string
}

// ResourceInfo holds OTel resource information shared by a batch of spans.
type ResourceInfo struct {
	Attributes map[string]string
	SchemaURL  string
}

// ScopeInfo holds OTel instrumentation scope information.
type ScopeInfo struct {
	Name       string
	Version    string
	Attributes map[string]string
	SchemaURL  string
}

// Trace is the trace-level aggregate stored in the traces table.
type Trace struct {
	TraceID           [16]byte
	TraceIDHex        string
	RootSpanID        [8]byte
	RootName          string
	SpanCount         uint16
	StartTimeMS       uint64
	EndTimeMS         uint64
	DurationMS        uint64
	ResourceAttrs     map[string]string
	ResourceSchemaURL string
	ScopeName         string
	ScopeVersion      string
	ScopeAttrs        map[string]string
	ScopeSchemaURL    string
	StatusCode        int32
	StatusMessage     string
	TotalTokens       *uint32
}

// TraceQuery defines filters for listing traces.
type TraceQuery struct {
	Page        int
	PageSize    int
	Service     string
	Status      string // "OK", "ERROR", "UNSET", "" = all
	Query       string // root_name fuzzy search
	StartTimeMS uint64
	EndTimeMS   uint64
	MinDuration uint64
	MaxDuration uint64
}

// TraceListResult holds a page of trace summaries.
type TraceListResult struct {
	Traces     []TraceListItem `json:"traces"`
	Pagination Pagination      `json:"pagination"`
}

// TraceListItem is a lightweight trace summary for the list view.
type TraceListItem struct {
	TraceIDHex   string  `json:"trace_id_hex"`
	RootSpanID   string  `json:"root_span_id"`
	RootName     string  `json:"root_name"`
	RootService  string  `json:"root_service"`
	StartTimeMS  uint64  `json:"start_time_ms"`
	DurationMS   uint64  `json:"duration_ms"`
	SpanCount    uint16  `json:"span_count"`
	Status       string  `json:"status"`
	TotalTokens  *uint32 `json:"total_tokens"`
}

// Pagination holds page metadata.
type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

// TraceDetail is the full trace with all spans for the detail view.
type TraceDetail struct {
	TraceIDHex        string            `json:"trace_id_hex"`
	RootSpanID        string            `json:"root_span_id"`
	SpanCount         int               `json:"span_count"`
	StartTimeMS       uint64            `json:"start_time_ms"`
	DurationMS        uint64            `json:"duration_ms"`
	ResourceAttrs     map[string]string `json:"resource_attributes"`
	ResourceSchemaURL string            `json:"resource_schema_url"`
	Scope             ScopeDetail       `json:"scope"`
	Spans             []SpanDetail      `json:"spans"`
}

// ScopeDetail is scope info in the trace detail response.
type ScopeDetail struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Attributes map[string]string `json:"attributes"`
}

// SpanDetail is a single span in the trace detail response.
type SpanDetail struct {
	SpanID              string            `json:"span_id"`
	ParentSpanID        string            `json:"parent_span_id"` // "" = root
	Name                string            `json:"name"`
	Kind                string            `json:"kind"`
	StartTimeMS         uint64            `json:"start_time_ms"`
	DurationMS          uint64            `json:"duration_ms"`
	Attributes          map[string]string `json:"attributes"`
	Events              []interface{}     `json:"events"`  // parsed JSON
	Links               []interface{}     `json:"links"`   // parsed JSON
	Status              string            `json:"status"`
	StatusMessage       string            `json:"status_message,omitempty"`
	InputTokens         *uint32           `json:"input_tokens"`
	OutputTokens        *uint32           `json:"output_tokens"`
	TotalTokens         *uint32           `json:"total_tokens"`
	GenAIRequestModel   *string           `json:"gen_ai_request_model"`
}

// Store is the storage backend interface. All chDB details are hidden behind this.
type Store interface {
	// InsertSpans writes a batch of spans and aggregates trace-level data
	// into the traces table. The same trace_id may appear across multiple
	// InsertSpans calls (spans from one trace arriving in batches).
	InsertSpans(ctx context.Context, resource ResourceInfo, scope ScopeInfo, spans []Span) error

	// ListTraces returns a paginated list of trace summaries matching the query.
	ListTraces(ctx context.Context, q TraceQuery) (*TraceListResult, error)

	// GetTrace returns all spans for a given trace, ordered by start_time_ms.
	GetTrace(ctx context.Context, traceID [16]byte) (*TraceDetail, error)

	// GetServices returns distinct service.name values for the filter dropdown.
	GetServices(ctx context.Context) ([]string, error)

	// Close releases resources (e.g., chDB session).
	Close() error
}

// Helper: byte arrays are the canonical internal representation.
// The API layer converts to/from hex strings.

// SpanIDToHex converts an 8-byte span ID to a 16-char hex string.
func SpanIDToHex(id [8]byte) string {
	if id == ([8]byte{}) {
		return ""
	}
	return fmt.Sprintf("%016x", id)
}

// TraceIDToHex converts a 16-byte trace ID to a 32-char hex string.
func TraceIDToHex(id [16]byte) string {
	return fmt.Sprintf("%032x", id)
}

// KindToString converts an OTel SpanKind int32 to its string representation.
func KindToString(kind int32) string {
	switch kind {
	case 0:
		return "UNSPECIFIED"
	case 1:
		return "INTERNAL"
	case 2:
		return "SERVER"
	case 3:
		return "CLIENT"
	case 4:
		return "PRODUCER"
	case 5:
		return "CONSUMER"
	default:
		return "UNSPECIFIED"
	}
}

// StatusCodeToString converts an OTel StatusCode int32 to string.
func StatusCodeToString(code int32) string {
	switch code {
	case 0:
		return "UNSET"
	case 1:
		return "OK"
	case 2:
		return "ERROR"
	default:
		return "UNSET"
	}
}
