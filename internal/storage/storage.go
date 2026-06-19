// Package storage defines the trace storage interface and data types.
// Implementations (e.g., chDB via CGO) live in sibling files.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
	Cost              *float64
	CostCurrency      string
	SessionID         string
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

// CostQuery defines filters for cost summary aggregation.
type CostQuery struct {
	StartTimeMS uint64
	EndTimeMS   uint64
}

// CostOverview holds aggregated cost totals.
type CostOverview struct {
	TotalCost         float64 `json:"total_cost"`
	TotalTokens       uint64  `json:"total_tokens"`
	TotalInputTokens  uint64  `json:"total_input_tokens"`
	TotalOutputTokens uint64  `json:"total_output_tokens"`
	AvgCostPerTrace   float64 `json:"avg_cost_per_trace"`
	TraceCount        int     `json:"trace_count"`
}

// ModelCostItem holds cost aggregation for a single model.
type ModelCostItem struct {
	Model        string  `json:"model"`
	Cost         float64 `json:"cost"`
	Tokens       uint64  `json:"tokens"`
	InputTokens  uint64  `json:"input_tokens"`
	OutputTokens uint64  `json:"output_tokens"`
	TraceCount   int     `json:"trace_count"`
	AvgCost      float64 `json:"avg_cost"`
}

// CostSummaryResult holds the full cost dashboard response.
type CostSummaryResult struct {
	Period   string          `json:"period"`
	Currency string          `json:"currency"`
	Overview CostOverview    `json:"overview"`
	ByModel  []ModelCostItem `json:"by_model"`
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
	TotalTokens  *uint32  `json:"total_tokens"`
	Cost         *float64 `json:"cost"`
	CostCurrency string   `json:"cost_currency"`
}

// Pagination holds page metadata.
type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

// SessionQuery defines filters for listing sessions.
type SessionQuery struct {
	Page        int
	PageSize    int
	Service     string
	Query       string // session_id fuzzy search
	StartTimeMS uint64
	EndTimeMS   uint64
}

// SessionListItem is a session summary for the list view.
type SessionListItem struct {
	SessionID       string  `json:"session_id"`
	TraceCount      int     `json:"trace_count"`
	TotalTokens     *uint32  `json:"total_tokens"`
	Cost            *float64 `json:"cost"`
	CostCurrency    string   `json:"cost_currency"`
	TotalDurationMS uint64   `json:"total_duration_ms"`
	MaxDurationMS   uint64  `json:"max_duration_ms"`
	AvgDurationMS   float64 `json:"avg_duration_ms"`
	ErrorCount      int     `json:"error_count"`
	ErrorRate       float64 `json:"error_rate"`
	FirstActiveMS   uint64  `json:"first_active_ms"`
	LastActiveMS    uint64  `json:"last_active_ms"`
}

// SessionDetail is a session with all its traces.
type SessionDetail struct {
	Session SessionListItem `json:"session"`
	Traces  []TraceListItem `json:"traces"`
}

// SessionListResult holds a page of session summaries.
type SessionListResult struct {
	Sessions   []SessionListItem `json:"sessions"`
	Pagination Pagination        `json:"pagination"`
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
	Cost              *float64          `json:"cost"`
	CostCurrency      string            `json:"cost_currency"`
	UnpricedSpans     int               `json:"unpriced_spans"`
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
	GenAISystem         *string           `json:"gen_ai_system"`        // Attributes["gen_ai.system"]
	ToolName            *string           `json:"tool_name"`            // Attributes["gen_ai.tool.name"]
	IsToolCall          bool              `json:"is_tool_call"`         // ToolName != nil
}

// ToolUsageItem holds statistics for a single tool across a session.
type ToolUsageItem struct {
	ToolName    string  `json:"tool_name"`
	CallCount   int     `json:"call_count"`
	SuccessRate float64 `json:"success_rate"`
	AvgRetries  float64 `json:"avg_retries"`
	MaxLoop     int     `json:"max_loop"`
}

// AgentStats holds aggregate agent behavior statistics for a session.
type AgentStats struct {
	TraceSuccessRate    float64        `json:"trace_success_rate"`
	AvgToolSuccessRate  float64        `json:"avg_tool_success_rate"`
	AvgRetries          float64        `json:"avg_retries"`
	AvgLoopDepth        float64        `json:"avg_loop_depth"`
	MaxLoopDepth        int            `json:"max_loop_depth"`
	SpanPerTrace        float64        `json:"span_per_trace"`
	TotalToolCalls      int            `json:"total_tool_calls"`
	SuccessfulToolCalls int            `json:"successful_tool_calls"`
	ToolUsage           []ToolUsageItem `json:"tool_usage"`
	Insights            []string       `json:"insights"`
}

// ModelPricing holds pricing configuration for a single model.
type ModelPricing struct {
	ModelName   string  `json:"model_name"`
	InputPrice  float64 `json:"input_price"`  // per 1M input tokens
	OutputPrice float64 `json:"output_price"` // per 1M output tokens
	Currency    string  `json:"currency"`     // "USD" or "CNY"
}

// LLMConfig holds configuration for a single LLM model used for trace analysis.
type LLMConfig struct {
	ID           string  `json:"id"`
	ModelName    string  `json:"model_name"`
	ProviderType string  `json:"provider_type"` // "openai" or "anthropic", default "openai"
	ProviderURL  string  `json:"provider_url"`
	APIKey       string  `json:"api_key"`     // plaintext at rest, masked on GET
	IsDefault    bool    `json:"is_default"`
	Temperature  float64 `json:"temperature"` // default 0.7
	MaxTokens    int     `json:"max_tokens"`  // default 4096
}

// MaskAPIKey truncates an API key for display: shows first 3 and last 2 chars
// for keys longer than 8 characters, otherwise returns "***".
func MaskAPIKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:3] + "***" + key[len(key)-2:]
}

// PricingConfig holds the default pricing loaded from YAML.
type PricingConfig struct {
	Models []ModelPricing `yaml:"models"`
}

// DiagnosisFinding is a single issue found by the LLM during trace diagnosis.
type DiagnosisFinding struct {
	Severity    string `json:"severity"`              // "error" | "warning" | "info"
	Dimension   string `json:"dimension"`             // "latency" | "cost" | "error" | "efficiency"
	Title       string `json:"title"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
	SpanName    string `json:"span_name,omitempty"`
	SpanIndex   int    `json:"span_index,omitempty"`
}

// DiagnosisScores holds the four dimension scores from a trace diagnosis.
type DiagnosisScores struct {
	Latency    int `json:"latency"`
	Cost       int `json:"cost"`
	Error      int `json:"error"`
	Efficiency int `json:"efficiency"`
}

// DiagnosisResult holds a complete LLM diagnosis for a single trace.
type DiagnosisResult struct {
	TraceID       [16]byte          `json:"trace_id"`
	TraceIDHex    string            `json:"trace_id_hex"`
	ModelName     string            `json:"model_name"`
	Scores        DiagnosisScores   `json:"scores"`
	OverallScore  uint8             `json:"overall_score"`
	Findings      []DiagnosisFinding `json:"findings"`
	Summary       string            `json:"summary"`
	SpansSnapshot string            `json:"-"`
	RawResponse   string            `json:"-"`
	CreatedAt     time.Time         `json:"created_at"`
	Stale         bool              `json:"stale"`
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

	// ListSessions returns a paginated list of session summaries.
	ListSessions(ctx context.Context, q SessionQuery) (*SessionListResult, error)

	// GetSession returns a session summary and all its traces.
	GetSession(ctx context.Context, sessionID string) (*SessionDetail, error)

	// Purge removes traces (and their spans) that exceed the retention policy.
	// maxAge: delete traces with start_time_ms older than (now - maxAge). 0 = no age limit.
	// maxCount: keep only the newest maxCount traces. 0 = no count limit.
	// Returns the number of deleted traces and spans.
	Purge(ctx context.Context, maxAge time.Duration, maxCount int) (deletedTraces int, deletedSpans int, err error)

	// InsertLogs writes a batch of log records.
	InsertLogs(ctx context.Context, logs []LogRecord) error

	// ListLogs returns a paginated list of log records matching the query.
	ListLogs(ctx context.Context, q LogQuery) (*LogListResult, error)

	// GetLogsByTrace returns all log records for a given trace, ordered by timestamp.
	GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]LogListItem, error)

	// GetLogEventNames returns distinct event_name values for the filter dropdown.
	GetLogEventNames(ctx context.Context) ([]string, error)

	// ModelPricing CRUD.
	GetModelPricing(ctx context.Context) ([]ModelPricing, error)
	UpsertModelPricing(ctx context.Context, p ModelPricing) error
	DeleteModelPricing(ctx context.Context, modelName string) error

	// LLMConfig CRUD.
	GetLLMConfigs(ctx context.Context) ([]LLMConfig, error)
	CreateLLMConfig(ctx context.Context, c *LLMConfig) error
	UpdateLLMConfig(ctx context.Context, c *LLMConfig) error
	DeleteLLMConfig(ctx context.Context, id string) error

	// UpdateTraceCost recalculates and stores cost for a trace.
	UpdateTraceCost(ctx context.Context, traceID [16]byte) error

	// GetCostSummary returns aggregated cost data for the given time range.
	GetCostSummary(ctx context.Context, q CostQuery) (*CostSummaryResult, error)

	// GetDiagnosisResult returns the stored diagnosis for a trace, or nil if none exists.
	GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*DiagnosisResult, error)

	// UpsertDiagnosisResult inserts or replaces the diagnosis result for a trace.
	UpsertDiagnosisResult(ctx context.Context, result *DiagnosisResult) error

	// GetSessionAgentStats computes agent behavior statistics for a session.
	GetSessionAgentStats(ctx context.Context, sessionID string) (*AgentStats, error)

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

// --- Log types ---

// LogRecord represents a single OTLP log record stored in the logs table.
type LogRecord struct {
	TraceID    [16]byte
	SpanID     [8]byte
	Timestamp  uint64
	Severity   string // TRACE, DEBUG, INFO, WARN, ERROR, FATAL
	EventName  string // extracted from attributes["event.name"]
	Body       string // JSON string, preserved as-is
	Attributes map[string]string
}

// LogQuery defines filters for listing logs.
type LogQuery struct {
	Page      int
	PageSize  int
	Severity  string   // "" = all
	EventName string   // "" = all
	Query     string   // full-text search on body
	TraceID   [16]byte // zero value = no trace filter
	StartTime uint64
	EndTime   uint64
}

// LogListItem is the API response item for a log record.
type LogListItem struct {
	TraceIDHex  string            `json:"trace_id_hex"`
	SpanIDHex   string            `json:"span_id_hex"`
	Timestamp   uint64            `json:"timestamp"`
	Severity    string            `json:"severity"`
	EventName   string            `json:"event_name"`
	Body        string            `json:"body"`
	Attributes  map[string]string `json:"attributes"`
}

// LogListResult holds a page of log records.
type LogListResult struct {
	Logs       []LogListItem `json:"logs"`
	Pagination Pagination    `json:"pagination"`
}

// parseJSONArray parses a JSON array string into []interface{}.
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

// SeverityToNumber converts a severity string to a numeric level for comparison.
// Higher number = more severe.
func SeverityToNumber(s string) int {
	switch strings.ToUpper(s) {
	case "TRACE":
		return 0
	case "DEBUG":
		return 1
	case "INFO":
		return 2
	case "WARN":
		return 3
	case "ERROR":
		return 4
	case "FATAL":
		return 5
	default:
		return -1
	}
}
