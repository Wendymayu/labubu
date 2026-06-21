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
	spans := translateSpans(nil)
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
