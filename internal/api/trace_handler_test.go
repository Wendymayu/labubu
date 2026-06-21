package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labubu/labubu/internal/storage"
)

// mockStore is a minimal Store implementation for handler testing.
type handlerMockStore struct {
	traces            *storage.TraceListResult
	detail            *storage.TraceDetail
	services          []string
	listErr           error
	detailErr         error
	costSummary       *storage.CostSummaryResult
	costSummaryErr    error
	llmConfigs        []storage.LLMConfig
	llmConfigsErr     error
	diagnosisResult   *storage.DiagnosisResult
	diagnosisResultErr error
}

func (m *handlerMockStore) InsertSpans(ctx context.Context, r storage.ResourceInfo, s storage.ScopeInfo, spans []storage.Span) error {
	return nil
}

func (m *handlerMockStore) ListTraces(ctx context.Context, q storage.TraceQuery) (*storage.TraceListResult, error) {
	return m.traces, m.listErr
}

func (m *handlerMockStore) GetTrace(ctx context.Context, id [16]byte) (*storage.TraceDetail, error) {
	return m.detail, m.detailErr
}

func (m *handlerMockStore) GetServices(ctx context.Context) ([]string, error) {
	return m.services, nil
}

func (m *handlerMockStore) ListSessions(ctx context.Context, q storage.SessionQuery) (*storage.SessionListResult, error) {
	return nil, nil
}

func (m *handlerMockStore) GetSession(ctx context.Context, sessionID string) (*storage.SessionDetail, error) {
	return nil, nil
}

func (m *handlerMockStore) Purge(ctx context.Context, maxAge time.Duration, maxCount int) (int, int, error) {
	return 0, 0, nil
}

func (m *handlerMockStore) InsertLogs(ctx context.Context, logs []storage.LogRecord) error { return nil }

func (m *handlerMockStore) ListLogs(ctx context.Context, q storage.LogQuery) (*storage.LogListResult, error) {
	return nil, nil
}

func (m *handlerMockStore) GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]storage.LogListItem, error) {
	return nil, nil
}

func (m *handlerMockStore) GetLogEventNames(ctx context.Context) ([]string, error) { return nil, nil }

func (m *handlerMockStore) GetLLMConfigs(ctx context.Context) ([]storage.LLMConfig, error) {
	return m.llmConfigs, m.llmConfigsErr
}
func (m *handlerMockStore) CreateLLMConfig(ctx context.Context, c *storage.LLMConfig) error {
	return fmt.Errorf("not implemented")
}
func (m *handlerMockStore) UpdateLLMConfig(ctx context.Context, c *storage.LLMConfig) error {
	return fmt.Errorf("not implemented")
}
func (m *handlerMockStore) DeleteLLMConfig(ctx context.Context, id string) error {
	return fmt.Errorf("not implemented")
}

func (m *handlerMockStore) GetModelPricing(ctx context.Context) ([]storage.ModelPricing, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *handlerMockStore) UpsertModelPricing(ctx context.Context, p storage.ModelPricing) error {
	return fmt.Errorf("not implemented")
}
func (m *handlerMockStore) DeleteModelPricing(ctx context.Context, modelName string) error {
	return fmt.Errorf("not implemented")
}
func (m *handlerMockStore) UpdateTraceCost(ctx context.Context, traceID [16]byte) error {
	return fmt.Errorf("not implemented")
}
func (m *handlerMockStore) GetCostSummary(ctx context.Context, q storage.CostQuery) (*storage.CostSummaryResult, error) {
	return m.costSummary, m.costSummaryErr
}

func (m *handlerMockStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*storage.AgentStats, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *handlerMockStore) Close() error { return nil }

func (m *handlerMockStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*storage.DiagnosisResult, error) {
	return m.diagnosisResult, m.diagnosisResultErr
}

func (m *handlerMockStore) UpsertDiagnosisResult(ctx context.Context, result *storage.DiagnosisResult) error {
	return nil
}

func TestListTraces(t *testing.T) {
	store := &handlerMockStore{
		traces: &storage.TraceListResult{
			Traces: []storage.TraceListItem{
				{
					TraceIDHex:  "a1b2c3d4e5f600000000000000000000",
					RootName:    "test-trace",
					RootService: "test-service",
					DurationMS:  1234,
					SpanCount:   5,
					Status:      "OK",
				},
			},
			Pagination: storage.Pagination{Page: 1, PageSize: 20, Total: 1},
		},
	}

	handler := NewTraceHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces?page=1&page_size=20", nil)
	rec := httptest.NewRecorder()

	handler.ListTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.TraceListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(result.Traces) != 1 {
		t.Errorf("expected 1 trace, got %d", len(result.Traces))
	}
	if result.Traces[0].RootName != "test-trace" {
		t.Errorf("expected root_name 'test-trace', got '%s'", result.Traces[0].RootName)
	}
}

func TestGetTraceBadID(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/short", nil)
	rec := httptest.NewRecorder()

	handler.GetTrace(rec, req, "short")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short id, got %d", rec.Code)
	}
}

func TestGetDiagnosisNoResult(t *testing.T) {
	store := &handlerMockStore{
		diagnosisResult: nil,
	}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/a1b2c3d4e5f600000000000000000000/diagnosis", nil)
	rec := httptest.NewRecorder()

	handler.GetDiagnosis(rec, req, "a1b2c3d4e5f600000000000000000000")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "no_diagnosis" {
		t.Errorf("expected 'no_diagnosis', got '%s'", body["error"])
	}
}

func TestGetDiagnosisCached(t *testing.T) {
	var tid [16]byte
	b, _ := hex.DecodeString("a1b2c3d4e5f600000000000000000000")
	copy(tid[:], b)

	store := &handlerMockStore{
		diagnosisResult: &storage.DiagnosisResult{
			TraceID:      tid,
			TraceIDHex:   "a1b2c3d4e5f600000000000000000000",
			ModelName:    "test-model",
			OverallScore: 85,
			Scores:       storage.DiagnosisScores{Latency: 90, Cost: 80, Error: 85, Efficiency: 85},
			Summary:      "all good",
		},
		detail: nil, // staleness check skipped when trace not found
	}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/a1b2c3d4e5f600000000000000000000/diagnosis", nil)
	rec := httptest.NewRecorder()

	handler.GetDiagnosis(rec, req, "a1b2c3d4e5f600000000000000000000")

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.DiagnosisResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if result.OverallScore != 85 {
		t.Errorf("expected overall_score 85, got %d", result.OverallScore)
	}
}

func TestDiagnoseTraceNoDefaultModel(t *testing.T) {
	var tid [16]byte
	b, _ := hex.DecodeString("a1b2c3d4e5f600000000000000000000")
	copy(tid[:], b)

	store := &handlerMockStore{
		detail: &storage.TraceDetail{
			TraceIDHex: "a1b2c3d4e5f600000000000000000000",
			SpanCount:  1,
			Spans:      []storage.SpanDetail{{SpanID: "abc", Name: "test", Status: "OK", DurationMS: 100}},
		},
		llmConfigs: []storage.LLMConfig{
			{ID: "1", ModelName: "test", IsDefault: false},
		},
	}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/a1b2c3d4e5f600000000000000000000/diagnose", nil)
	rec := httptest.NewRecorder()

	handler.DiagnoseTrace(rec, req, "a1b2c3d4e5f600000000000000000000")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "no_default_model" {
		t.Errorf("expected 'no_default_model', got '%s'", body["error"])
	}
}

func TestDiagnoseTraceBadID(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/short/diagnose", nil)
	rec := httptest.NewRecorder()

	handler.DiagnoseTrace(rec, req, "short")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short id, got %d", rec.Code)
	}
}

func TestExportTracesProtojson(t *testing.T) {
	store := &handlerMockStore{
		detail: &storage.TraceDetail{
			TraceIDHex: "a1b2c3d4e5f600000000000000000000",
			SpanCount:  2,
			ResourceAttrs: map[string]string{
				"service.name": "my-service",
			},
			ResourceSchemaURL: "https://opentelemetry.io/schemas/1.0",
			Scope: storage.ScopeDetail{
				Name:    "my-instrumentation",
				Version: "1.0.0",
			},
			Spans: []storage.SpanDetail{
				{
					SpanID:       "0123456789abcdef",
					ParentSpanID: "",
					Name:         "root-span",
					Kind:         "SERVER",
					StartTimeMS:  1000,
					DurationMS:   500,
					Attributes: map[string]string{
						"http.status_code":       "200",
						"gen_ai.usage.input_tokens": "150",
					},
					Status:        "OK",
					StatusMessage: "",
				},
				{
					SpanID:       "abcdef0123456789",
					ParentSpanID: "0123456789abcdef",
					Name:         "child-span",
					Kind:         "CLIENT",
					StartTimeMS:  1100,
					DurationMS:   200,
					Attributes: map[string]string{
						"gen_ai.usage.input_tokens": "50",
					},
					Status:        "UNSET",
					StatusMessage: "",
				},
			},
		},
	}
	handler := NewTraceHandler(store)

	body := `{"trace_ids":["a1b2c3d4e5f600000000000000000000"],"format":"otlp"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ExportTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	respBody := rec.Body.String()

	// Verify traceId is hex-encoded (32-char hex, not base64)
	// protojson with UseProtoNames uses snake_case: "trace_id" not "traceId"
	if !strings.Contains(respBody, `"trace_id":"a1b2c3d4e5f600000000000000000000"`) {
		t.Errorf("expected hex-encoded trace_id in response, got: %s", respBody)
	}

	// Verify intValue for numeric attributes (not stringValue)
	if !strings.Contains(respBody, `"int_value"`) {
		t.Errorf("expected int_value for numeric attributes, got: %s", respBody)
	}

	// Verify http.status_code=200 is rendered as intValue
	if !strings.Contains(respBody, `"http.status_code"`) {
		t.Errorf("expected http.status_code key in response, got: %s", respBody)
	}

	// Verify the response is a single TracesData envelope (not an array)
	if strings.HasPrefix(respBody, "[") {
		t.Errorf("export should return a single TracesData envelope, not an array; got: %s", respBody[:20])
	}
}

func TestImportTracesInvalidJSON(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewTraceHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/import", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ImportTraces(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestImportTracesEmptyBody(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewTraceHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/import", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ImportTraces(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", rec.Code)
	}
}

func TestImportTracesMethodNotAllowed(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewTraceHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/import", nil)
	rec := httptest.NewRecorder()
	handler.ImportTraces(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", rec.Code)
	}
}
