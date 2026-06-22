package receiver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labubu/labubu/internal/pipeline"
	"github.com/labubu/labubu/internal/storage"
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
func (m *memTestStore) GetSession(_ context.Context, _ string) (*storage.SessionDetail, error) {
	return nil, nil
}
func (m *memTestStore) Purge(_ context.Context, _ time.Duration, _ int) (int, int, error) {
	return 0, 0, nil
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
	// Verify that fallback keys produce typed token columns
	// after normalizeAttributes copies them to standard keys.
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
			expectInput:  nil,
			expectOutput: nil,
			expectTotal:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := make(map[string]string, len(tt.input))
			for k, v := range tt.input {
				attrs[k] = v
			}
			normalizeAttributes(attrs)

			inputTokens := getUint32AttrFromMap(attrs, "gen_ai.usage.input_tokens")
			outputTokens := getUint32AttrFromMap(attrs, "gen_ai.usage.output_tokens")
			var totalTokens *uint32
			if inputTokens != nil || outputTokens != nil {
				var sum uint32
				if inputTokens != nil {
					sum += *inputTokens
				}
				if outputTokens != nil {
					sum += *outputTokens
				}
				if t := getUint32AttrFromMap(attrs, "gen_ai.usage.total_tokens"); t != nil {
					sum = *t
				}
				totalTokens = &sum
			}

			if !uint32PtrEqual(inputTokens, tt.expectInput) {
				t.Errorf("inputTokens: got %v, want %v", inputTokens, tt.expectInput)
			}
			if !uint32PtrEqual(outputTokens, tt.expectOutput) {
				t.Errorf("outputTokens: got %v, want %v", outputTokens, tt.expectOutput)
			}
			if !uint32PtrEqual(totalTokens, tt.expectTotal) {
				t.Errorf("totalTokens: got %v, want %v", totalTokens, tt.expectTotal)
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
