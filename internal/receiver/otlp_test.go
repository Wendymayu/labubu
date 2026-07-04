package receiver

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labubu/labubu/internal/pipeline"
	"github.com/labubu/labubu/internal/storage"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// memTestStore implements storage.Store with stubs for all methods.
// Used in tests where we only need InsertSpans to succeed.
type memTestStore struct{}

func (m *memTestStore) InsertSpans(_ context.Context, _ storage.ResourceInfo, _ storage.ScopeInfo, _ []storage.Span) error {
	return nil
}
func (m *memTestStore) ListTraces(_ context.Context, _ storage.TraceQuery) (*storage.TraceListResult, error) {
	return &storage.TraceListResult{Traces: []storage.TraceListItem{}}, nil
}
func (m *memTestStore) GetTrace(_ context.Context, _ [16]byte) (*storage.TraceDetail, error) {
	return nil, nil
}
func (m *memTestStore) GetServices(_ context.Context) ([]string, error) {
	return nil, nil
}
func (m *memTestStore) ListSessions(_ context.Context, _ storage.SessionQuery) (*storage.SessionListResult, error) {
	return &storage.SessionListResult{Sessions: []storage.SessionListItem{}}, nil
}
func (m *memTestStore) GetSession(_ context.Context, _ string, _, _ int) (*storage.SessionDetail, error) {
	return nil, nil
}
func (m *memTestStore) Purge(_ context.Context, _ time.Duration, _ int) (int, int, error) {
	return 0, 0, nil
}
func (m *memTestStore) PurgeLogs(_ context.Context, _ time.Duration) (int, error) {
	return 0, nil
}
func (m *memTestStore) InsertLogs(_ context.Context, _ []storage.LogRecord) error {
	return nil
}
func (m *memTestStore) ListLogs(_ context.Context, _ storage.LogQuery) (*storage.LogListResult, error) {
	return &storage.LogListResult{Logs: []storage.LogListItem{}}, nil
}
func (m *memTestStore) GetLogsByTrace(_ context.Context, _ [16]byte) ([]storage.LogListItem, error) {
	return nil, nil
}
func (m *memTestStore) GetLogCountsByTrace(_ context.Context, _ [16]byte) (map[string]int, error) {
	return nil, nil
}
func (m *memTestStore) GetLogEventNames(_ context.Context) ([]string, error) {
	return nil, nil
}
func (m *memTestStore) GetModelPricing(_ context.Context) ([]storage.ModelPricing, error) {
	return nil, nil
}
func (m *memTestStore) UpsertModelPricing(_ context.Context, _ storage.ModelPricing) error {
	return nil
}
func (m *memTestStore) DeleteModelPricing(_ context.Context, _ string) error {
	return nil
}
func (m *memTestStore) GetLLMConfigs(_ context.Context) ([]storage.LLMConfig, error) {
	return nil, nil
}
func (m *memTestStore) CreateLLMConfig(_ context.Context, _ *storage.LLMConfig) error {
	return nil
}
func (m *memTestStore) UpdateLLMConfig(_ context.Context, _ *storage.LLMConfig) error {
	return nil
}
func (m *memTestStore) DeleteLLMConfig(_ context.Context, _ string) error {
	return nil
}
func (m *memTestStore) UpdateTraceCost(_ context.Context, _ [16]byte) error {
	return nil
}
func (m *memTestStore) GetCostSummary(_ context.Context, _ storage.CostQuery) (*storage.CostSummaryResult, error) {
	return nil, nil
}
func (m *memTestStore) GetDiagnosisResult(_ context.Context, _ [16]byte) (*storage.DiagnosisResult, error) {
	return nil, nil
}
func (m *memTestStore) UpsertDiagnosisResult(_ context.Context, _ *storage.DiagnosisResult) error {
	return nil
}
func (m *memTestStore) GetSessionAgentStats(_ context.Context, _ string) (*storage.AgentStats, error) {
	return nil, nil
}
func (m *memTestStore) GetSessionContextSpans(_ context.Context, _ string) ([]storage.SessionContextSpan, error) {
	return nil, nil
}
func (m *memTestStore) Close() error {
	return nil
}

func TestTranslateSpanBasic(t *testing.T) {
	// Basic smoke test: nil spans should produce empty result.
	spans := TranslateSpans(nil)
	if len(spans) != 0 {
		t.Errorf("expected 0 spans from nil input, got %d", len(spans))
	}
}

func TestNewReceiver(t *testing.T) {
	// Verify that New accepts nil store and does not panic.
	r := New(nil, nil, nil)
	if r == nil {
		t.Error("expected non-nil receiver")
	}
}

func TestAnyValueToString(t *testing.T) {
	if s := anyValueToString(nil); s != "" {
		t.Errorf("expected empty string for nil, got %q", s)
	}
}

func TestKeyValueToMap(t *testing.T) {
	if m := keyValueToMap(nil); len(m) != 0 {
		t.Errorf("expected empty map for nil, got %v", m)
	}
}

func TestHTTPTracesJSON(t *testing.T) {
	// Standard OTLP JSON payload with base64-encoded traceId/spanId (protojson format).
	jsonPayload := `{
			"resourceSpans": [{
				"resource": {
					"attributes": [
						{"key": "service.name", "value": {"stringValue": "test-service"}}
					]
				},
				"scopeSpans": [{
					"scope": {"name": "test-lib", "version": "1.0"},
					"spans": [{
						"traceId": "MWYyZDRlNjcxY2VkNGI3NjgwMDAwMDAw",
						"spanId": "YWJjMTIzNDU2Nzg5",
						"name": "test-span",
						"kind": 1,
						"startTimeUnixNano": "1608238394254000000",
						"endTimeUnixNano": "1608238394354000000",
						"status": {"code": 1}
					}]
				}]
			}]
		}`

	store := &memTestStore{}
	p := pipeline.New(store, 100, 1*time.Second)
	defer p.Shutdown(context.Background())

	r := New(p, nil, store)

	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader([]byte(jsonPayload)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.handleHTTPTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHTTPTracesJSONWithCharset(t *testing.T) {
	// Content-Type may include charset parameter like "application/json; charset=utf-8".
	jsonPayload := `{
			"resourceSpans": [{
				"resource": {
					"attributes": [
						{"key": "service.name", "value": {"stringValue": "test-service"}}
					]
				},
				"scopeSpans": [{
					"scope": {"name": "test-lib", "version": "1.0"},
					"spans": [{
						"traceId": "MWYyZDRlNjcxY2VkNGI3NjgwMDAwMDAw",
						"spanId": "YWJjMTIzNDU2Nzg5",
						"name": "test-span",
						"kind": 1,
						"startTimeUnixNano": "1608238394254000000",
						"endTimeUnixNano": "1608238394354000000",
						"status": {"code": 1}
					}]
				}]
			}]
		}`

	store := &memTestStore{}
	p := pipeline.New(store, 100, 1*time.Second)
	defer p.Shutdown(context.Background())

	r := New(p, nil, store)

	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader([]byte(jsonPayload)))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()

	r.handleHTTPTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for charset Content-Type, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestNormalizeAttributes(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]string
		expect map[string]string
	}{
		{
			name: "Claude Code keys → standard keys",
			input: map[string]string{
				"input_tokens":  "100",
				"output_tokens": "50",
				"session.id":    "abc-123",
			},
			expect: map[string]string{
				"input_tokens":                   "100",
				"output_tokens":                  "50",
				"session.id":                     "abc-123",
				"gen_ai.usage.input_tokens":     "100",
				"gen_ai.usage.output_tokens":    "50",
				"jiuwenclaw.session.id":          "abc-123",
			},
		},
		{
			name: "standard keys already present → no alias",
			input: map[string]string{
				"gen_ai.usage.input_tokens": "200",
				"input_tokens":              "100",
			},
			expect: map[string]string{
				"gen_ai.usage.input_tokens": "200",
				"input_tokens":              "100",
			},
		},
		{
			name: "OpenInference llm.* keys",
			input: map[string]string{
				"llm.usage.input_tokens":  "300",
				"llm.usage.output_tokens": "150",
				"llm.request.model":       "gpt-4o",
			},
			expect: map[string]string{
				"llm.usage.input_tokens":       "300",
				"llm.usage.output_tokens":      "150",
				"llm.request.model":            "gpt-4o",
				"gen_ai.usage.input_tokens":    "300",
				"gen_ai.usage.output_tokens":   "150",
				"gen_ai.request.model":         "gpt-4o",
			},
		},
		{
			name:   "empty attributes → no changes",
			input:  map[string]string{},
			expect: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := make(map[string]string, len(tt.input))
			for k, v := range tt.input {
				attrs[k] = v
			}
			normalizeAttributes(attrs)
			for k, v := range tt.expect {
				if attrs[k] != v {
					t.Errorf("key %q: got %q, want %q", k, attrs[k], v)
				}
			}
			for k := range attrs {
				if _, ok := tt.expect[k]; !ok {
					t.Errorf("unexpected key %q added", k)
				}
			}
		})
	}
}

func TestTokenExtractionAfterNormalize(t *testing.T) {
	tests := []struct {
		name         string
		input        map[string]string
		expectInput  *uint32
		expectOutput *uint32
		expectTotal  *uint32
	}{
		{
			name:         "Claude Code input_tokens → typed column",
			input:        map[string]string{"input_tokens": "100", "output_tokens": "50"},
			expectInput:  uint32Ptr(100),
			expectOutput: uint32Ptr(50),
			expectTotal:  uint32Ptr(150),
		},
		{
			name:         "OpenInference llm keys → typed column",
			input:        map[string]string{"llm.usage.input_tokens": "200", "llm.usage.output_tokens": "100"},
			expectInput:  uint32Ptr(200),
			expectOutput: uint32Ptr(100),
			expectTotal:  uint32Ptr(300),
		},
		{
			name:         "standard keys already present",
			input:        map[string]string{"gen_ai.usage.input_tokens": "300"},
			expectInput:  uint32Ptr(300),
			expectOutput: nil,
			expectTotal:  uint32Ptr(300),
		},
		{
			name:         "no token keys → nil",
			input:        map[string]string{"other_attr": "value"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := make(map[string]string, len(tt.input))
			for k, v := range tt.input {
				attrs[k] = v
			}
			normalizeAttributes(attrs)
			in, out, _, _, tot := storage.DeriveTokenBuckets(attrs)
			if !uint32PtrEqual(in, tt.expectInput) {
				t.Errorf("inputTokens: got %v, want %v", in, tt.expectInput)
			}
			if !uint32PtrEqual(out, tt.expectOutput) {
				t.Errorf("outputTokens: got %v, want %v", out, tt.expectOutput)
			}
			if !uint32PtrEqual(tot, tt.expectTotal) {
				t.Errorf("totalTokens: got %v, want %v", tot, tt.expectTotal)
			}
		})
	}
}

func uint32Ptr(v uint32) *uint32 { return &v }

func uint32PtrEqual(a, b *uint32) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func intKV(key string, v int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v}}}
}

// TestTranslateSpanCacheTokens verifies Claude Code's prompt-caching tokens
// (cache_creation_tokens / cache_read_tokens) are extracted into typed columns
// and folded into total_tokens. This is the root cause of the undercount bug.
func TestTranslateSpanCacheTokens(t *testing.T) {
	span := &tracepb.Span{
		TraceId: make([]byte, 16),
		SpanId:  make([]byte, 8),
		Name:    "claude_code.llm_request",
		Attributes: []*commonpb.KeyValue{
			intKV("input_tokens", 2),
			intKV("output_tokens", 100),
			intKV("cache_creation_tokens", 189194),
			intKV("cache_read_tokens", 5000),
		},
	}

	got := translateSpan(span)

	if !uint32PtrEqual(got.InputTokens, uint32Ptr(2)) {
		t.Errorf("InputTokens: got %v, want 2", got.InputTokens)
	}
	if !uint32PtrEqual(got.OutputTokens, uint32Ptr(100)) {
		t.Errorf("OutputTokens: got %v, want 100", got.OutputTokens)
	}
	if !uint32PtrEqual(got.CacheCreationTokens, uint32Ptr(189194)) {
		t.Errorf("CacheCreationTokens: got %v, want 189194", got.CacheCreationTokens)
	}
	if !uint32PtrEqual(got.CacheReadTokens, uint32Ptr(5000)) {
		t.Errorf("CacheReadTokens: got %v, want 5000", got.CacheReadTokens)
	}
	// total = input + output + cache_creation + cache_read = 2 + 100 + 189194 + 5000 = 194296
	if !uint32PtrEqual(got.TotalTokens, uint32Ptr(194296)) {
		t.Errorf("TotalTokens: got %v, want 194296", got.TotalTokens)
	}
	// Normalized keys should be present on the attributes map too.
	if got.Attributes["gen_ai.usage.cache_creation_input_tokens"] != "189194" {
		t.Errorf("normalized cache_creation key missing: %v", got.Attributes["gen_ai.usage.cache_creation_input_tokens"])
	}
}

// TestTranslateSpanNoTokens ensures non-LLM spans leave all token columns nil.
func TestTranslateSpanNoTokens(t *testing.T) {
	span := &tracepb.Span{
		TraceId:    make([]byte, 16),
		SpanId:     make([]byte, 8),
		Name:       "http.request",
		Attributes: []*commonpb.KeyValue{intKV("http.method", 0)},
	}
	got := translateSpan(span)
	if got.InputTokens != nil || got.OutputTokens != nil || got.TotalTokens != nil ||
		got.CacheCreationTokens != nil || got.CacheReadTokens != nil {
		t.Errorf("expected nil token columns for non-LLM span, got input=%v output=%v total=%v cc=%v cr=%v",
			got.InputTokens, got.OutputTokens, got.TotalTokens, got.CacheCreationTokens, got.CacheReadTokens)
	}
}

// freePort returns a currently-free TCP port on localhost. The listener is
// closed before returning, so there is a small race window before the caller
// re-binds it; this is the standard technique for test port allocation and is
// acceptable for test reliability.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// dialCheck verifies that something is listening on the given localhost port.
func dialCheck(port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// TestStartCustomPorts verifies Start binds to the provided gRPC and HTTP ports
// (not the hardcoded 4317/4318) and that both listeners actually accept connections.
func TestStartCustomPorts(t *testing.T) {
	grpcPort := freePort(t)
	httpPort := freePort(t)

	store := &memTestStore{}
	r := New(nil, nil, store) // pipeline/metrics nil: we only test binding, not export
	if err := r.Start(grpcPort, httpPort); err != nil {
		t.Fatalf("Start(%d, %d): %v", grpcPort, httpPort, err)
	}
	defer r.Shutdown(context.Background())

	if err := dialCheck(grpcPort); err != nil {
		t.Errorf("gRPC port %d not listening: %v", grpcPort, err)
	}
	if err := dialCheck(httpPort); err != nil {
		t.Errorf("HTTP port %d not listening: %v", httpPort, err)
	}
}

// TestStartFailFastOnConflict verifies that a port conflict returns an error
// from Start (rather than silently starting a non-listening server).
func TestStartFailFastOnConflict(t *testing.T) {
	// Reserve a port on the same address Start binds (0.0.0.0) and keep it held
	// so Start's bind is an exact collision.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("reserve listener: %v", err)
	}
	defer ln.Close()
	conflictPort := ln.Addr().(*net.TCPAddr).Port
	httpPort := freePort(t)

	store := &memTestStore{}
	r := New(nil, nil, store)
	if err := r.Start(conflictPort, httpPort); err == nil {
		r.Shutdown(context.Background())
		t.Errorf("expected error when gRPC port %d is in use, got nil", conflictPort)
	}
}

// TestStartFailFastOnHTTPConflict verifies that when the HTTP port is already
// in use, Start returns an error AND releases the gRPC listener it had just
// bound (no listener leak on partial failure).
func TestStartFailFastOnHTTPConflict(t *testing.T) {
	// gRPC port is free and will bind successfully first.
	grpcPort := freePort(t)
	// Reserve the HTTP port on the same address Start binds (0.0.0.0) and hold it.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("reserve listener: %v", err)
	}
	defer ln.Close()
	httpPort := ln.Addr().(*net.TCPAddr).Port

	store := &memTestStore{}
	r := New(nil, nil, store)
	if err := r.Start(grpcPort, httpPort); err == nil {
		r.Shutdown(context.Background())
		t.Errorf("expected error when HTTP port %d is in use, got nil", httpPort)
	}

	// The gRPC listener Start bound must have been released so it can be
	// re-bound immediately. If grpcLis.Close() was skipped, this bind fails.
	ln2, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", grpcPort))
	if err != nil {
		t.Errorf("gRPC port %d was not released after HTTP-conflict failure: %v", grpcPort, err)
	} else {
		ln2.Close()
	}
}

// logCaptureStore wraps memTestStore and records logs passed to InsertLogs,
// so HTTP handler tests can assert that a parsed payload actually reached storage.
type logCaptureStore struct {
	*memTestStore
	logs []storage.LogRecord
}

func (s *logCaptureStore) InsertLogs(_ context.Context, logs []storage.LogRecord) error {
	s.logs = append(s.logs, logs...)
	return nil
}

// TestHTTPLogsJSON verifies the OTLP HTTP /v1/logs endpoint accepts JSON
// (protojson) payloads, not just protobuf. The OTLP/HTTP spec requires
// servers to accept application/json.
func TestHTTPLogsJSON(t *testing.T) {
	jsonPayload := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "log-svc"}}
				]
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "1608238394254000000",
					"severityNumber": 9,
					"severityText": "INFO",
					"traceId": "MWYyZDRlNjcxY2VkNGI3NjgwMDAwMDAw",
					"spanId": "YWJjMTIzNDU2Nzg5",
					"body": {"stringValue": "hello from json"},
					"attributes": [
						{"key": "event.name", "value": {"stringValue": "user.login"}}
					]
				}]
			}]
		}]
	}`

	store := &logCaptureStore{memTestStore: &memTestStore{}}
	r := New(nil, nil, store)

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(jsonPayload)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.handleHTTPLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	if len(store.logs) != 1 {
		t.Fatalf("expected 1 log stored, got %d", len(store.logs))
	}
	got := store.logs[0]
	if got.Body != "hello from json" {
		t.Errorf("body: got %q, want %q", got.Body, "hello from json")
	}
	if got.Severity != "INFO" {
		t.Errorf("severity: got %q, want %q", got.Severity, "INFO")
	}
	if got.EventName != "user.login" {
		t.Errorf("event_name: got %q, want %q", got.EventName, "user.login")
	}
	if got.Timestamp != 1608238394254 {
		t.Errorf("timestamp: got %d, want %d", got.Timestamp, uint64(1608238394254))
	}
}

// TestHTTPLogsJSONWithCharset verifies JSON payloads with a charset parameter
// (e.g. "application/json; charset=utf-8") are accepted.
func TestHTTPLogsJSONWithCharset(t *testing.T) {
	jsonPayload := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"1608238394254000000","severityNumber":9,"body":{"stringValue":"x"}}]}]}]}`

	store := &logCaptureStore{memTestStore: &memTestStore{}}
	r := New(nil, nil, store)

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(jsonPayload)))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()

	r.handleHTTPLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for charset Content-Type, got %d; body: %s", rec.Code, rec.Body.String())
	}
	if len(store.logs) != 1 {
		t.Errorf("expected 1 log stored, got %d", len(store.logs))
	}
}

// TestSeverityTextFallback verifies that when SeverityNumber is UNSPECIFIED,
// the severity is derived from SeverityText instead of falling back to the
// "SEVERITY_NUMBER_0" placeholder. The OTLP spec requires this derivation.
func TestSeverityTextFallback(t *testing.T) {
	lr := &logspb.LogRecord{
		SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED,
		SeverityText:   "WARNING",
	}
	rec := translateLogRecord(lr, nil, nil)
	if rec.Severity != "WARN" {
		t.Errorf("severity from text: got %q, want %q", rec.Severity, "WARN")
	}
}

// TestSeverityTextFallbackCaseInsensitive verifies the text match is case-insensitive.
func TestSeverityTextFallbackCaseInsensitive(t *testing.T) {
	lr := &logspb.LogRecord{
		SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED,
		SeverityText:   "error",
	}
	rec := translateLogRecord(lr, nil, nil)
	if rec.Severity != "ERROR" {
		t.Errorf("severity from lowercase text: got %q, want %q", rec.Severity, "ERROR")
	}
}

// TestSeverityTextFallbackUnknown verifies that an unrecognized severity text
// is preserved uppercased rather than becoming "SEVERITY_NUMBER_0".
func TestSeverityTextFallbackUnknown(t *testing.T) {
	lr := &logspb.LogRecord{
		SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED,
		SeverityText:   "notice",
	}
	rec := translateLogRecord(lr, nil, nil)
	if rec.Severity != "NOTICE" {
		t.Errorf("unknown severity text: got %q, want %q", rec.Severity, "NOTICE")
	}
}

// TestSeverityNumberPrecedence verifies that an explicit SeverityNumber wins
// over SeverityText (text is only a fallback when number is UNSPECIFIED).
func TestSeverityNumberPrecedence(t *testing.T) {
	lr := &logspb.LogRecord{
		SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_ERROR,
		SeverityText:   "warning", // mismatched; number must take precedence
	}
	rec := translateLogRecord(lr, nil, nil)
	if rec.Severity != "ERROR" {
		t.Errorf("number should take precedence: got %q, want %q", rec.Severity, "ERROR")
	}
}

// TestLogTimestampObservedTimeFallback verifies that when TimeUnixNano is
// absent (0), the timestamp is derived from ObservedTimeUnixNano. The OTLP
// spec says TimeUnixNano is optional and ObservedTimeUnixNano is the fallback.
func TestLogTimestampObservedTimeFallback(t *testing.T) {
	lr := &logspb.LogRecord{
		TimeUnixNano:         0,
		ObservedTimeUnixNano: 1608238394254000000,
	}
	rec := translateLogRecord(lr, nil, nil)
	want := uint64(1608238394254)
	if rec.Timestamp != want {
		t.Errorf("timestamp fallback: got %d, want %d", rec.Timestamp, want)
	}
}

// TestLogTimestampPrecedence verifies that TimeUnixNano takes precedence over
// ObservedTimeUnixNano when both are set.
func TestLogTimestampPrecedence(t *testing.T) {
	lr := &logspb.LogRecord{
		TimeUnixNano:         1608238394254000000,
		ObservedTimeUnixNano: 1700000000000000000, // different; must be ignored
	}
	rec := translateLogRecord(lr, nil, nil)
	want := uint64(1608238394254)
	if rec.Timestamp != want {
		t.Errorf("time should take precedence: got %d, want %d", rec.Timestamp, want)
	}
}

// TestTranslateSpanIgnoresSelfReportedTotal verifies that a self-reported
// gen_ai.usage.total_tokens is IGNORED — total is always the bucket sum.
func TestTranslateSpanIgnoresSelfReportedTotal(t *testing.T) {
	span := &tracepb.Span{
		TraceId: make([]byte, 16),
		SpanId:  make([]byte, 8),
		Name:    "claude_code.llm_request",
		Attributes: []*commonpb.KeyValue{
			intKV("input_tokens", 100),
			intKV("output_tokens", 50),
			intKV("total_tokens", 999),
		},
	}
	got := translateSpan(span)
	// total = 100 + 50 = 150 (the 999 must be ignored)
	if !uint32PtrEqual(got.TotalTokens, uint32Ptr(150)) {
		t.Errorf("TotalTokens: got %v, want 150 (self-reported 999 ignored)", got.TotalTokens)
	}
}
