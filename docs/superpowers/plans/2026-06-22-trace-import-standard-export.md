# Trace Import & Standardized OTLP Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add trace import functionality and standardize the OTLP export format to be compatible with other observability tools.

**Architecture:** Replace hand-rolled OTLP JSON structs with protojson.Marshal on standard proto types (tracepb.TracesData). Add import endpoint that uses protojson.Unmarshal to ingest any standard OTLP JSON. Fix the OTLP HTTP receiver to use protojson for JSON payloads. Frontend adds an Import button on the trace list page.

**Tech Stack:** Go (protojson, tracepb.TracesData), Vue 3 (TypeScript, vue-i18n)

---

### Task 1: Delete hand-rolled OTLP structs and create proto conversion module

**Files:**
- Delete: `internal/api/otlp_trace.go`
- Create: `internal/api/otlp_proto.go`
- Modify: `internal/api/trace_handler.go:97-99` (GetTrace otlp format branch), `:117-175` (ExportTraces)
- Modify: `internal/api/trace_handler_test.go:16` (add InsertSpans tracking to mock)

- [ ] **Step 1: Write the failing test for export format**

Add a test in `internal/api/trace_handler_test.go` that verifies the export endpoint produces protojson-compatible output with proper AnyValue types and hex-encoded traceId/spanId.

```go
func TestExportTracesProtojson(t *testing.T) {
	inputTokens := uint32(100)
	outputTokens := uint32(50)
	totalTokens := uint32(150)
	genAIModel := "gpt-4"
	store := &handlerMockStore{
		detail: &storage.TraceDetail{
			TraceIDHex: "4bf92f3577b34da6a3ce929d0e0e4736",
			RootSpanID: "abc1234567890def",
			SpanCount:  2,
			StartTimeMS: 1608238394254,
			DurationMS:  100,
			ResourceAttrs: map[string]string{
				"service.name":    "frontend",
				"service.version": "1.0.0",
			},
			Spans: []storage.SpanDetail{
				{
					SpanID:              "abc1234567890def",
					ParentSpanID:        "",
					Name:                "HTTP GET /api",
					Kind:                "SERVER",
					StartTimeMS:         1608238394254,
					DurationMS:          100,
					Attributes:          map[string]string{
						"http.method":              "GET",
						"http.status_code":         "200",
						"gen_ai.usage.input_tokens": "100",
					},
					Events:              []interface{}{},
					Links:               []interface{}{},
					Status:              "OK",
					InputTokens:         &inputTokens,
					OutputTokens:        &outputTokens,
					TotalTokens:         &totalTokens,
					GenAIRequestModel:   &genAIModel,
				},
			},
		},
	}
	handler := NewTraceHandler(store)

	body := `{"trace_ids":["4bf92f3577b34da6a3ce929d0e0e4736"],"format":"otlp"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ExportTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify traceId is hex (32 chars), not base64
	var result []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	rs := result[0]["resourceSpans"].([]interface{})[0].(map[string]interface{})
	ss := rs["scopeSpans"].([]interface{})[0].(map[string]interface{})
	spans := ss["spans"].([]interface{})
	span0 := spans[0].(map[string]interface{})

	// traceId should be 32-char hex, not base64
	traceId := span0["traceId"].(string)
	if len(traceId) != 32 {
		t.Errorf("expected hex traceId (32 chars), got %q (len %d)", traceId, len(traceId))
	}

	// Attributes should have intValue for numeric values
	attrs := span0["attributes"].([]interface{})
	for _, attr := range attrs {
		kv := attr.(map[string]interface{})
		if kv["key"] == "http.status_code" {
			val := kv["value"].(map[string]interface{})
			if _, ok := val["intValue"]; !ok {
				t.Errorf("expected intValue for http.status_code, got: %v", val)
			}
		}
		if kv["key"] == "gen_ai.usage.input_tokens" {
			val := kv["value"].(map[string]interface{})
			if _, ok := val["intValue"]; !ok {
				t.Errorf("expected intValue for gen_ai.usage.input_tokens, got: %v", val)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/api/ -run TestExportTracesProtojson -tags '!local_engine'`
Expected: FAIL — `convertToOTLP` and `otlpTraceResponse` types still exist, but produce `stringValue` for all attributes and hex traceId. The test will fail because `intValue` is not present in the output.

Actually, this test will compile but fail on the `intValue` assertions since the current code outputs all values as `stringValue`. The `traceId` check may pass since current code already uses hex. So the key failure is the `intValue` assertion.

- [ ] **Step 3: Delete `otlp_trace.go` and create `otlp_proto.go`**

Delete the entire file `internal/api/otlp_trace.go`.

Create `internal/api/otlp_proto.go`:

```go
package api

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/labubu/labubu/internal/storage"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// convertToProto converts an internal TraceDetail to a TracesData proto struct.
func convertToProto(t *storage.TraceDetail) *tracepb.TracesData {
	resource := &resourcepb.Resource{
		Attributes:             mapToProtoKeyValues(t.ResourceAttrs),
		DroppedAttributesCount: 0,
	}

	scopeSpans := &tracepb.ScopeSpans{
		Scope: &commonpb.InstrumentationScope{
			Name:       t.Scope.Name,
			Version:    t.Scope.Version,
			Attributes: mapToProtoKeyValues(t.Scope.Attributes),
		},
		Spans: make([]*tracepb.Span, 0, len(t.Spans)),
		SchemaUrl: t.ResourceSchemaURL,
	}

	for _, s := range t.Spans {
		scopeSpans.Spans = append(scopeSpans.Spans, spanToProto(s, t.TraceIDHex))
	}

	return &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{
			{
				Resource:   resource,
				ScopeSpans: []*tracepb.ScopeSpans{scopeSpans},
				SchemaUrl:  t.ResourceSchemaURL,
			},
		},
	}
}

func spanToProto(s storage.SpanDetail, traceIDHex string) *tracepb.Span {
	traceIDBytes, _ := hex.DecodeString(traceIDHex)
	spanIDBytes, _ := hex.DecodeString(s.SpanID)
	parentSpanIDBytes, _ := hex.DecodeString(s.ParentSpanID)

	endTimeMS := s.StartTimeMS + s.DurationMS

	return &tracepb.Span{
		TraceId:                traceIDBytes,
		SpanId:                 spanIDBytes,
		ParentSpanId:           parentSpanIDBytes,
		Name:                   s.Name,
		Kind:                   kindToProtoEnum(s.Kind),
		StartTimeUnixNano:      s.StartTimeMS * 1_000_000,
		EndTimeUnixNano:        endTimeMS * 1_000_000,
		Attributes:             mapToProtoKeyValues(s.Attributes),
		DroppedAttributesCount: 0,
		Events:                 eventsToProto(s.Events),
		DroppedEventsCount:     0,
		Links:                  linksToProto(s.Links),
		DroppedLinksCount:      0,
		Status:                 statusToProto(s.Status, s.StatusMessage),
	}
}

func kindToProtoEnum(kind string) tracepb.Span_SpanKind {
	switch kind {
	case "UNSPECIFIED":
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	case "INTERNAL":
		return tracepb.Span_SPAN_KIND_INTERNAL
	case "SERVER":
		return tracepb.Span_SPAN_KIND_SERVER
	case "CLIENT":
		return tracepb.Span_SPAN_KIND_CLIENT
	case "PRODUCER":
		return tracepb.Span_SPAN_KIND_PRODUCER
	case "CONSUMER":
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_INTERNAL
	}
}

func statusToProto(status string, message string) *tracepb.Status {
	code := tracepb.Status_STATUS_CODE_UNSET
	switch status {
	case "OK":
		code = tracepb.Status_STATUS_CODE_OK
	case "ERROR":
		code = tracepb.Status_STATUS_CODE_ERROR
	}
	return &tracepb.Status{Code: code, Message: message}
}

// inferAnyValue determines the best AnyValue type for a string value.
// Tries int → float → bool → string to preserve original type info.
func inferAnyValue(s string) *commonpb.AnyValue {
	// Try int64
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v}}
	}
	// Try float64 (but not if it looks like an integer string already handled above)
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v}}
	}
	// Try bool
	if s == "true" {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}
	}
	if s == "false" {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: false}}
	}
	// Default: string
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

func mapToProtoKeyValues(m map[string]string) []*commonpb.KeyValue {
	if len(m) == 0 {
		return []*commonpb.KeyValue{}
	}
	result := make([]*commonpb.KeyValue, 0, len(m))
	for k, v := range m {
		result = append(result, &commonpb.KeyValue{
			Key:   k,
			Value: inferAnyValue(v),
		})
	}
	return result
}

func eventsToProto(events []interface{}) []*tracepb.Span_Event {
	if len(events) == 0 {
		return []*tracepb.Span_Event{}
	}
	result := make([]*tracepb.Span_Event, 0, len(events))
	for _, e := range events {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		timeMS, _ := m["time_ms"].(float64)
		attrs := mapFromInterface(m["attributes"])

		result = append(result, &tracepb.Span_Event{
			TimeUnixNano:           uint64(timeMS) * 1_000_000,
			Name:                   name,
			Attributes:             mapToProtoKeyValues(attrs),
			DroppedAttributesCount: 0,
		})
	}
	return result
}

func linksToProto(links []interface{}) []*tracepb.Span_Link {
	if len(links) == 0 {
		return []*tracepb.Span_Link{}
	}
	result := make([]*tracepb.Span_Link, 0, len(links))
	for _, l := range links {
		m, ok := l.(map[string]interface{})
		if !ok {
			continue
		}
		traceIDHex, _ := m["trace_id"].(string)
		spanIDHex, _ := m["span_id"].(string)
		traceIDBytes, _ := hex.DecodeString(traceIDHex)
		spanIDBytes, _ := hex.DecodeString(spanIDHex)
		attrs := mapFromInterface(m["attributes"])

		result = append(result, &tracepb.Span_Link{
			TraceId:                traceIDBytes,
			SpanId:                 spanIDBytes,
			Attributes:             mapToProtoKeyValues(attrs),
			DroppedAttributesCount: 0,
		})
	}
	return result
}

func mapFromInterface(attrsRaw interface{}) map[string]string {
	if attrsRaw == nil {
		return nil
	}
	m, ok := attrsRaw.(map[string]interface{})
	if !ok {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

// marshalOTLPJSON converts TracesData to JSON with hex-encoded traceId/spanId.
// protojson.Marshal outputs base64 for bytes fields; we post-process to hex.
func marshalOTLPJSON(td *tracepb.TracesData) ([]byte, error) {
	opts := protojson.MarshalOptions{UseProtoNames: false, EmitUnpopulated: true}
	b, err := opts.Marshal(td)
	if err != nil {
		return nil, err
	}
	return replaceBase64WithHex(b), nil
}

// replaceBase64WithHex replaces base64-encoded traceId/spanId/parentSpanId
// values in protojson output with hex strings for human readability.
func replaceBase64WithHex(jsonBytes []byte) []byte {
	// Regex to match "traceId":"<base64>", "spanId":"<base64>", "parentSpanId":"<base64>"
	re := regexp.MustCompile(`"(traceId|spanId|parentSpanId)":\s*"([A-Za-z0-9+/=]+)"`)
	return re.ReplaceAllFunc(jsonBytes, func(match []byte) []byte {
		sub := re.FindSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		fieldName := string(sub[1])
		base64Val := string(sub[2])
		rawBytes, err := base64.StdEncoding.DecodeString(base64Val)
		if err != nil {
			// Try URL-safe base64
			rawBytes, err = base64.URLEncoding.DecodeString(base64Val)
			if err != nil {
				return match // not valid base64, leave unchanged
			}
		}
		hexVal := hex.EncodeToString(rawBytes)
		return []byte(fmt.Sprintf(`"%s":"%s"`, fieldName, hexVal))
	})
}

// replaceHexWithBase64 converts hex-encoded traceId/spanId values to base64
// so protojson.Unmarshal can parse them (protojson expects base64 for bytes).
func replaceHexWithBase64(jsonBytes []byte) []byte {
	re := regexp.MustCompile(`"(traceId|spanId|parentSpanId)":\s*"([0-9a-fA-F]+)"`)
	return re.ReplaceAllFunc(jsonBytes, func(match []byte) []byte {
		sub := re.FindSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		fieldName := string(sub[1])
		hexVal := string(sub[2])
		rawBytes, err := hex.DecodeString(hexVal)
		if err != nil {
			return match // not valid hex, leave unchanged
		}
		base64Val := base64.StdEncoding.EncodeToString(rawBytes)
		return []byte(fmt.Sprintf(`"%s":"%s"`, fieldName, base64Val))
	})
}
```

- [ ] **Step 4: Update `trace_handler.go` — GetTrace and ExportTraces**

In `internal/api/trace_handler.go`, replace references to the old `otlp_trace.go` functions:

1. Replace `writeOTLPResponse(w, detail)` (line 98) with:

```go
td := convertToProto(detail)
jsonBytes, err := marshalOTLPJSON(td)
if err != nil {
    writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("marshal otlp: %v", err)})
    return
}
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusOK)
w.Write(jsonBytes)
```

2. Replace `ExportTraces` function body (lines 117-175). Change `[]otlpTraceResponse` to `[]*tracepb.TracesData` and use `marshalOTLPJSON`:

```go
func (h *TraceHandler) ExportTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST allowed"})
		return
	}

	var req struct {
		TraceIDs []string `json:"trace_ids"`
		Format   string   `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	if req.Format != "otlp" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "format must be 'otlp'"})
		return
	}

	if len(req.TraceIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace_ids must not be empty"})
		return
	}
	if len(req.TraceIDs) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max 100 traces per export"})
		return
	}

	// Collect all TracesData into a single response array
	allSpans := make([]*tracepb.ResourceSpans, 0)
	for _, hexID := range req.TraceIDs {
		if len(hexID) != 32 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid trace_id length: %s (must be 32 hex chars)", hexID)})
			return
		}
		traceIDBytes, err := hex.DecodeString(hexID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid hex trace_id %s: %v", hexID, err)})
			return
		}
		var traceID [16]byte
		copy(traceID[:], traceIDBytes)

		detail, err := h.store.GetTrace(r.Context(), traceID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get trace %s: %v", hexID, err)})
			return
		}
		if detail == nil {
			continue
		}

		td := convertToProto(detail)
		allSpans = append(allSpans, td.ResourceSpans...)
	}

	// Build a single TracesData envelope for all exported traces
	combined := &tracepb.TracesData{ResourceSpans: allSpans}
	jsonBytes, err := marshalOTLPJSON(combined)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("marshal otlp: %v", err)})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonBytes)
}
```

Note: The export endpoint now returns a single `TracesData` envelope instead of an array of separate `otlpTraceResponse` objects. This matches the standard OTLP format.

- [ ] **Step 5: Run tests to verify everything compiles and the new test passes**

Run: `go test -v ./internal/api/ -tags '!local_engine'`
Expected: All existing tests pass, `TestExportTracesProtojson` passes with `intValue` in output.

- [ ] **Step 6: Commit**

```bash
git add internal/api/otlp_trace.go internal/api/otlp_proto.go internal/api/trace_handler.go internal/api/trace_handler_test.go
git commit -m "feat: replace hand-rolled OTLP structs with protojson for standard export format"
```

---

### Task 2: Fix OTLP HTTP JSON receiver to use protojson

**Files:**
- Modify: `internal/receiver/otlp.go:205-238` (handleHTTPTraces JSON branch)

- [ ] **Step 1: Write the failing test**

The OTLP HTTP receiver currently fails for JSON payloads. Add a test in `internal/receiver/` (or a new test file) that sends a JSON OTLP trace via HTTP to the `/v1/traces` endpoint.

Create `internal/receiver/otlp_test.go`:

```go
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

func TestHTTPTracesJSON(t *testing.T) {
	// Create a pipeline with a store to capture ingested spans
	store := &memTestStore{}
	p := pipeline.New(store, pipeline.Config{FlushInterval: 50 * time.Millisecond, BufferSize: 100})
	go p.Run(context.Background())
	defer p.Shutdown()

	r := New(p, nil, nil)

	// Standard OTLP JSON payload (with base64-encoded traceId/spanId)
	jsonBody := `{
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

	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewBufferString(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.handleHTTPTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for JSON payload, got %d: %s", rec.Code, rec.Body.String())
	}
}

// memTestStore is a minimal in-memory store for testing the receiver pipeline.
type memTestStore struct {
	spans []storage.Span
}

func (s *memTestStore) InsertSpans(ctx context.Context, r storage.ResourceInfo, scope storage.ScopeInfo, spans []storage.Span) error {
	s.spans = append(s.spans, spans...)
	return nil
}
func (s *memTestStore) ListTraces(ctx context.Context, q storage.TraceQuery) (*storage.TraceListResult, error) { return nil, nil }
func (s *memTestStore) GetTrace(ctx context.Context, id [16]byte) (*storage.TraceDetail, error)                 { return nil, nil }
func (s *memTestStore) GetServices(ctx context.Context) ([]string, error)                                         { return nil, nil }
func (s *memTestStore) ListSessions(ctx context.Context, q storage.SessionQuery) (*storage.SessionListResult, error) { return nil, nil }
func (s *memTestStore) GetSession(ctx context.Context, id string) (*storage.SessionDetail, error)                 { return nil, nil }
func (s *memTestStore) Purge(ctx context.Context, maxAge time.Duration, maxCount int) (int, int, error)          { return 0, 0, nil }
func (s *memTestStore) InsertLogs(ctx context.Context, logs []storage.LogRecord) error                            { return nil }
func (s *memTestStore) ListLogs(ctx context.Context, q storage.LogQuery) (*storage.LogListResult, error)          { return nil, nil }
func (s *memTestStore) GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]storage.LogListItem, error)       { return nil, nil }
func (s *memTestStore) GetLogEventNames(ctx context.Context) ([]string, error)                                    { return nil, nil }
func (s *memTestStore) GetModelPricing(ctx context.Context) ([]storage.ModelPricing, error)                       { return nil, nil }
func (s *memTestStore) UpsertModelPricing(ctx context.Context, p storage.ModelPricing) error                      { return nil }
func (s *memTestStore) DeleteModelPricing(ctx context.Context, m string) error                                    { return nil }
func (s *memTestStore) GetLLMConfigs(ctx context.Context) ([]storage.LLMConfig, error)                            { return nil, nil }
func (s *memTestStore) CreateLLMConfig(ctx context.Context, c *storage.LLMConfig) error                           { return nil }
func (s *memTestStore) UpdateLLMConfig(ctx context.Context, c *storage.LLMConfig) error                           { return nil }
func (s *memTestStore) DeleteLLMConfig(ctx context.Context, id string) error                                      { return nil }
func (s *memTestStore) UpdateTraceCost(ctx context.Context, traceID [16]byte) error                               { return nil }
func (s *memTestStore) GetCostSummary(ctx context.Context, q storage.CostQuery) (*storage.CostSummaryResult, error) { return nil, nil }
func (s *memTestStore) GetSessionAgentStats(ctx context.Context, id string) (*storage.AgentStats, error)           { return nil, nil }
func (s *memTestStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*storage.DiagnosisResult, error) { return nil, nil }
func (s *memTestStore) UpsertDiagnosisResult(ctx context.Context, r *storage.DiagnosisResult) error               { return nil }
func (s *memTestStore) Close() error                                                                               { return nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/receiver/ -run TestHTTPTracesJSON -tags '!local_engine'`
Expected: FAIL — the current `proto.Unmarshal` on JSON text will return an error, causing a 400 response.

- [ ] **Step 3: Fix the JSON handler in `otlp.go`**

In `internal/receiver/otlp.go`, add `protojson` import and fix `handleHTTPTraces`:

Add to import block:
```go
"google.golang.org/protobuf/encoding/protojson"
```

Replace lines 221-238 (the content-type dispatch block):

```go
	contentType := req.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		if err := protojson.Unmarshal(body, &exportReq); err != nil {
			http.Error(w, fmt.Sprintf("failed to unmarshal JSON: %v", err), http.StatusBadRequest)
			return
		}
	} else {
		if err := proto.Unmarshal(body, &exportReq); err != nil {
			http.Error(w, fmt.Sprintf("failed to unmarshal protobuf: %v", err), http.StatusBadRequest)
			return
		}
	}
```

Also add `"strings"` to the import block (already imported via `fmt` usage, but check).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/receiver/ -run TestHTTPTracesJSON -tags '!local_engine'`
Expected: PASS — the JSON payload is now correctly parsed.

- [ ] **Step 5: Commit**

```bash
git add internal/receiver/otlp.go internal/receiver/otlp_test.go
git commit -m "fix: use protojson.Unmarshal for OTLP HTTP JSON payloads"
```

---

### Task 3: Add trace import backend endpoint

**Files:**
- Modify: `internal/api/trace_handler.go` (add `ImportTraces` method)
- Modify: `internal/api/router.go:17` (add import route)
- Modify: `internal/api/trace_handler_test.go` (add import tests)

- [ ] **Step 1: Write failing tests for import endpoint**

Add to `internal/api/trace_handler_test.go`:

```go
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
	req.Header.Set("Content-Type", "application/json")
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/api/ -run TestImport -tags '!local_engine'`
Expected: FAIL — `ImportTraces` method doesn't exist yet.

- [ ] **Step 3: Implement `ImportTraces` handler**

Add to `internal/api/trace_handler.go`:

```go
import (
	// ... existing imports ...
	"io"
	"google.golang.org/protobuf/encoding/protojson"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"github.com/labubu/labubu/internal/receiver"
)

const maxImportSize = 10 * 1024 * 1024 // 10MB

// ImportTraces handles POST /api/v1/traces/import.
// Accepts OTLP JSON (TracesData or ExportTraceServiceRequest) and ingests new traces.
func (h *TraceHandler) ImportTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST allowed"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxImportSize+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("read body: %v", err)})
		return
	}
	if len(body) > maxImportSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body exceeds 10MB limit"})
		return
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty request body"})
		return
	}

	// Pre-process: convert hex traceId/spanId to base64 for protojson compatibility
	body = replaceHexWithBase64(body)

	// Try TracesData first, then ExportTraceServiceRequest
	var td tracepb.TracesData
	if err := protojson.Unmarshal(body, &td); err != nil {
		// Try as ExportTraceServiceRequest (wraps TracesData)
		var exportReq coltracepb.ExportTraceServiceRequest
		if err2 := protojson.Unmarshal(body, &exportReq); err2 != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid OTLP JSON: %v (also tried as ExportTraceServiceRequest: %v)", err, err2)})
			return
		}
		td.ResourceSpans = exportReq.ResourceSpans
	}

	imported := 0
	skipped := 0

	for _, rs := range td.ResourceSpans {
		if rs == nil {
			continue
		}
		resource := translateResource(rs.Resource)
		if rs.SchemaUrl != "" {
			resource.SchemaURL = rs.SchemaUrl
		}

		for _, ss := range rs.ScopeSpans {
			if ss == nil {
				continue
			}
			scope := translateScope(ss.Scope)
			if ss.SchemaUrl != "" {
				scope.SchemaURL = ss.SchemaUrl
			}

			spans := translateSpans(ss.Spans)
			if len(spans) == 0 {
				continue
			}

			// Check which trace_ids are new vs existing
			for _, span := range spans {
				traceIDHex := storage.TraceIDToHex(span.TraceID)
				traceIDBytes, _ := hex.DecodeString(traceIDHex)
				var traceID [16]byte
				copy(traceID[:], traceIDBytes)

				existing, err := h.store.GetTrace(r.Context(), traceID)
				if err != nil || existing != nil {
					skipped++
					continue
				}

				if err := h.store.InsertSpans(r.Context(), resource, scope, []storage.Span{span}); err != nil {
					continue
				}
				imported++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"imported": imported,
		"skipped":  skipped,
	})
}
```

Wait — inserting spans one-by-one is inefficient and breaks the `aggregateTraces` grouping. The correct approach is to group spans by trace_id first, check which trace_ids are new, then call `InsertSpans` once per (resource, scope, new-trace-spans) batch. But `InsertSpans` takes all spans for potentially multiple traces in one call. Let me revise:

Actually, the simplest correct approach: collect all spans, identify unique trace_ids, check which are new, then only insert spans belonging to new traces.

```go
// ImportTraces handles POST /api/v1/traces/import.
func (h *TraceHandler) ImportTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST allowed"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxImportSize+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("read body: %v", err)})
		return
	}
	if len(body) > maxImportSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body exceeds 10MB limit"})
		return
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty request body"})
		return
	}

	// Pre-process: convert hex traceId/spanId to base64 for protojson compatibility
	body = replaceHexWithBase64(body)

	// Try TracesData first, then ExportTraceServiceRequest
	var td tracepb.TracesData
	if err := protojson.Unmarshal(body, &td); err != nil {
		var exportReq coltracepb.ExportTraceServiceRequest
		if err2 := protojson.Unmarshal(body, &exportReq); err2 != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid OTLP JSON: %v (as ExportRequest: %v)", err, err2)})
			return
		}
		td.ResourceSpans = exportReq.ResourceSpans
	}

	imported := 0
	skipped := 0

	for _, rs := range td.ResourceSpans {
		if rs == nil {
			continue
		}
		resource := receiver.TranslateResource(rs.Resource)
		if rs.SchemaUrl != "" {
			resource.SchemaURL = rs.SchemaUrl
		}

		for _, ss := range rs.ScopeSpans {
			if ss == nil {
				continue
			}
			scope := receiver.TranslateScope(ss.Scope)
			if ss.SchemaUrl != "" {
				scope.SchemaURL = ss.SchemaUrl
			}

			spans := receiver.TranslateSpans(ss.Spans)
			if len(spans) == 0 {
				continue
			}

			// Determine which trace_ids in this batch are new
			newSpans := make([]storage.Span, 0, len(spans))
			seenTraceIDs := make(map[[16]byte]bool)
			for _, span := range spans {
				if seenTraceIDs[span.TraceID] {
					newSpans = append(newSpans, span)
					continue
				}
				seenTraceIDs[span.TraceID] = true

				existing, err := h.store.GetTrace(r.Context(), span.TraceID)
				if err != nil || existing != nil {
					skipped++
					continue
				}
				newSpans = append(newSpans, span)
				imported++
			}

			if len(newSpans) == 0 {
				continue
			}

			if err := h.store.InsertSpans(r.Context(), resource, scope, newSpans); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("insert spans: %v", err)})
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"imported": imported,
		"skipped":  skipped,
	})
}
```

But there's a dependency issue: `translateResource`, `translateScope`, `translateSpans` are defined in `internal/receiver/otlp.go` and are unexported. We need to either:
- Export them (rename to `TranslateResource`, etc.)
- Or duplicate the logic

The cleanest approach is to export them. In `internal/receiver/otlp.go`, rename:
- `translateResource` → `TranslateResource`
- `translateScope` → `TranslateScope`
- `translateSpans` → `TranslateSpans`

Since these are also used internally by the receiver, we need to update all internal references too.

Actually, looking at the receiver code, `translateResource`, `translateScope`, `translateSpans` are called in `traceService.Export`, `handleHTTPTraces`, `logsService.Export`. We need to update all of those references. Let me adjust:

In the import handler, we'll call the exported versions:
```go
resource := receiver.TranslateResource(rs.Resource)
scope := receiver.TranslateScope(ss.Scope)
spans := receiver.TranslateSpans(ss.Spans)
```

This requires importing the receiver package:
```go
"github.com/labubu/labubu/internal/receiver"
```

And in `internal/receiver/otlp.go`, change the function names:
- `func translateResource(...)` → `func TranslateResource(...)`
- `func translateScope(...)` → `func TranslateScope(...)`
- `func translateSpans(...)` → `func TranslateSpans(...)`

And update all call sites within `otlp.go` that reference these functions.

- [ ] **Step 4: Export receiver translation functions**

In `internal/receiver/otlp.go`, rename the three functions and update call sites:

```go
// Change func translateResource to func TranslateResource
func TranslateResource(resource *resourcepb.Resource) storage.ResourceInfo {
	if resource == nil {
		return storage.ResourceInfo{}
	}
	return storage.ResourceInfo{
		Attributes: keyValueToMap(resource.Attributes),
	}
}

// Change func translateScope to func TranslateScope
func TranslateScope(scope *commonpb.InstrumentationScope) storage.ScopeInfo {
	if scope == nil {
		return storage.ScopeInfo{}
	}
	return storage.ScopeInfo{
		Name:       scope.Name,
		Version:    scope.Version,
		Attributes: keyValueToMap(scope.Attributes),
	}
}

// Change func translateSpans to func TranslateSpans
func TranslateSpans(protoSpans []*tracepb.Span) []storage.Span {
	spans := make([]storage.Span, 0, len(protoSpans))
	for _, ps := range protoSpans {
		if ps == nil {
			continue
		}
		span := translateSpan(ps)
		spans = append(spans, span)
	}
	return spans
}
```

Update all internal call sites in `otlp.go`:
- `translateResource(resourceSpan.Resource)` → `TranslateResource(resourceSpan.Resource)`
- `translateScope(scopeSpan.Scope)` → `TranslateScope(scopeSpan.Scope)`
- `translateSpans(scopeSpan.Spans)` → `TranslateSpans(scopeSpan.Spans)`
- `translateResource(rs.Resource)` → `TranslateResource(rs.Resource)` (in HTTP handlers)
- `translateScope(ss.Scope)` → `TranslateScope(ss.Scope)` (in HTTP handlers)
- `translateSpans(ss.Spans)` → `TranslateSpans(ss.Spans)` (in HTTP handlers)

- [ ] **Step 5: Add import route to router**

In `internal/api/router.go`, add after the export route (line 17):

```go
mux.HandleFunc("/api/v1/traces/import", traceHandler.ImportTraces)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -v ./internal/api/ ./internal/receiver/ -tags '!local_engine'`
Expected: All tests pass including the import tests.

- [ ] **Step 7: Commit**

```bash
git add internal/api/trace_handler.go internal/api/trace_handler_test.go internal/api/router.go internal/receiver/otlp.go
git commit -m "feat: add trace import endpoint POST /api/v1/traces/import"
```

---

### Task 4: Add frontend import functionality

**Files:**
- Modify: `web/src/api/client.ts:120-140` (add ImportResult type and importTraces function)
- Modify: `web/src/views/TraceList.vue:22-25,94-165` (add Import button and logic)
- Modify: `web/src/i18n/locales/en.ts:53-69` (add import i18n keys)
- Modify: `web/src/i18n/locales/zh.ts:53-69` (add import i18n keys)

- [ ] **Step 1: Add i18n keys**

In `web/src/i18n/locales/en.ts`, add after `exportFailed` in the `traceList` section:

```typescript
importFailed: 'Import failed',
importBtn: 'Import',
importResult: 'Imported {imported} traces, skipped {skipped}',
```

In `web/src/i18n/locales/zh.ts`, add after `exportFailed`:

```typescript
importFailed: '导入失败',
importBtn: '导入',
importResult: '已导入 {imported} 条 trace，跳过 {skipped} 条',
```

- [ ] **Step 2: Add API client type and function**

In `web/src/api/client.ts`, add after the `exportTraces` function:

```typescript
export interface ImportResult {
  imported: number
  skipped: number
}

export async function importTraces(jsonData: string): Promise<ImportResult> {
  const res = await fetch(`${BASE_URL}/traces/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: jsonData,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Import failed: ${res.status}`)
  }
  return res.json()
}
```

- [ ] **Step 3: Add Import button and logic to TraceList.vue**

In `web/src/views/TraceList.vue`:

1. Update the import line to include `importTraces`:

```typescript
import { listTraces, getServices, exportTraces, importTraces, type TraceListItem, type Pagination, type ImportResult } from '../api/client'
```

2. Add state refs after `exportLoading`:

```typescript
const importLoading = ref(false)
const importResult = ref<ImportResult | null>(null)
const importError = ref('')
```

3. Add hidden file input ref:

```typescript
const fileInput = ref<HTMLInputElement | null>(null)
```

4. Add import function after `downloadSelected`:

```typescript
function triggerImport() {
  fileInput.value?.click()
}

async function handleImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  importLoading.value = true
  importResult.value = null
  importError.value = ''
  try {
    const text = await file.text()
    const result = await importTraces(text)
    importResult.value = result
    // Refresh trace list after successful import
    fetchTraces()
  } catch (e: any) {
    importError.value = e.message
  } finally {
    importLoading.value = false
    // Reset file input so the same file can be re-selected
    input.value = ''
  }
}
```

5. Update template — add Import button and hidden file input after the download button (line 24):

```html
<button @click="triggerImport" :disabled="importLoading" class="btn">
  {{ importLoading ? t('common.loading') : t('traceList.importBtn') }}
</button>
<input ref="fileInput" type="file" accept=".json" style="display:none" @change="handleImportFile" />

<span v-if="importResult" class="import-result">{{ t('traceList.importResult', { imported: importResult.imported, skipped: importResult.skipped }) }}</span>
<span v-if="importError" class="import-error">{{ t('traceList.importFailed') }}: {{ importError }}</span>
```

- [ ] **Step 4: Run TypeScript type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS — no type errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/client.ts web/src/views/TraceList.vue web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add trace import button on trace list page with i18n"
```

---

### Task 5: Integration test — round-trip export → import

**Files:**
- Modify: `internal/api/trace_handler_test.go` (add round-trip test)

- [ ] **Step 1: Write round-trip integration test**

Add to `internal/api/trace_handler_test.go`. This test exports a trace via `ExportTraces`, then imports the exported JSON via `ImportTraces`, verifying the imported data matches.

```go
func TestImportExportRoundTrip(t *testing.T) {
	// Create a store that can actually store and retrieve traces
	inputTokens := uint32(100)
	outputTokens := uint32(50)
	totalTokens := uint32(150)
	genAIModel := "gpt-4"

	traceIDHex := "4bf92f3577b34da6a3ce929d0e0e4736"
	traceIDBytes, _ := hex.DecodeString(traceIDHex)
	var traceID [16]byte
	copy(traceID[:], traceIDBytes)

	store := &handlerMockStore{
		detail: &storage.TraceDetail{
			TraceIDHex: traceIDHex,
			ResourceAttrs: map[string]string{
				"service.name":    "frontend",
				"service.version": "1.0.0",
			},
			ResourceSchemaURL: "https://opentelemetry.io/schemas/1.0",
			Scope: storage.ScopeDetail{
				Name:    "test-lib",
				Version: "1.0",
			},
			Spans: []storage.SpanDetail{
				{
					SpanID:              "abc1234567890def",
					ParentSpanID:        "",
					Name:                "HTTP GET /api",
					Kind:                "SERVER",
					StartTimeMS:         1608238394254,
					DurationMS:          100,
					Attributes:          map[string]string{
						"http.method":      "GET",
						"http.status_code": "200",
					},
					Events:              []interface{}{},
					Links:               []interface{}{},
					Status:              "OK",
					InputTokens:         &inputTokens,
					OutputTokens:        &outputTokens,
					TotalTokens:         &totalTokens,
					GenAIRequestModel:   &genAIModel,
				},
			},
		},
	}
	handler := NewTraceHandler(store)

	// Step 1: Export the trace
	exportBody := `{"trace_ids":["4bf92f3577b34da6a3ce929d0e0e4736"],"format":"otlp"}`
	exportReq := httptest.NewRequest(http.MethodPost, "/api/v1/traces/export", strings.NewReader(exportBody))
	exportReq.Header.Set("Content-Type", "application/json")
	exportRec := httptest.NewRecorder()

	handler.ExportTraces(exportRec, exportReq)

	if exportRec.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}

	exportedJSON := exportRec.Body.String()

	// Step 2: Import the exported JSON (mock store has GetTrace returning detail for first call, nil for second)
	// The mock needs to return nil for the import check so the trace is considered "new"
	importStore := &handlerMockStore{detail: nil}
	importHandler := NewTraceHandler(importStore)

	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/traces/import", strings.NewReader(exportedJSON))
	importReq.Header.Set("Content-Type", "application/json")
	importRec := httptest.NewRecorder()

	importHandler.ImportTraces(importRec, importRec)

	if importRec.Code != http.StatusOK {
		t.Fatalf("import: expected 200, got %d: %s", importRec.Code, importRec.Body.String())
	}

	var importResult map[string]interface{}
	json.Unmarshal(importRec.Body.Bytes(), &importResult)

	importedCount := int(importResult["imported"].(float64))
	if importedCount == 0 {
		t.Errorf("expected at least 1 imported trace, got 0")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test -v ./internal/api/ -run TestImportExportRoundTrip -tags '!local_engine'`
Expected: PASS — the exported JSON is valid protojson that can be re-imported.

- [ ] **Step 3: Commit**

```bash
git add internal/api/trace_handler_test.go
git commit -m "test: add export→import round-trip integration test"
```

---

### Task 6: Build and final verification

**Files:** None — verification only

- [ ] **Step 1: Run all Go tests**

Run: `go test -v ./internal/... ./web/... ./cmd/... -tags '!local_engine'`
Expected: All tests pass.

- [ ] **Step 2: Run TypeScript type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS — no type errors.

- [ ] **Step 3: Build the binary**

Run: `go build -tags '!local_engine' ./cmd/labubu/`
Expected: Build succeeds with no errors.

- [ ] **Step 4: Push all commits**

```bash
git push origin develop
```

- [ ] **Step 5: Manual smoke test**

1. Start Labubu: `./labubu serve`
2. Open http://localhost:8080 → trace list page
3. Verify "Import" button appears in toolbar
4. Click "Import", select a previously exported `.json` file
5. Verify toast shows "Imported N traces, skipped M"
6. Verify trace list refreshes with imported traces
7. Click on an imported trace → verify span detail shows correctly
8. Download a trace via OTLP format → verify JSON has `intValue` for numeric attributes and hex `traceId`
