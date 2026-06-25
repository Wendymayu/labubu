// Package receiver handles OTLP trace ingestion via gRPC and HTTP.
package receiver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	ilog "github.com/labubu/labubu/internal/log"
	"github.com/labubu/labubu/internal/metrics"
	"github.com/labubu/labubu/internal/pipeline"
	"github.com/labubu/labubu/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Receiver listens for OTLP trace data on gRPC and HTTP endpoints.
type Receiver struct {
	pipeline    *pipeline.Pipeline
	metricStore metrics.Store
	store       storage.Store
	grpcSrv     *grpc.Server
	httpSrv     *http.Server
}

// New creates a new Receiver.
func New(p *pipeline.Pipeline, ms metrics.Store, s storage.Store) *Receiver {
	return &Receiver{
		pipeline:    p,
		metricStore: ms,
		store:       s,
	}
}

// Start begins listening on the given gRPC and HTTP ports for OTLP data.
// Both listeners are bound synchronously so a port conflict fails fast and
// returns an error, rather than starting a server that silently does not listen.
func (r *Receiver) Start(grpcPort, httpPort int) error {
	// gRPC server.
	r.grpcSrv = grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(r.grpcSrv, &traceService{pipeline: r.pipeline})
	if r.metricStore != nil {
		colmetricspb.RegisterMetricsServiceServer(r.grpcSrv, &metricsService{metricStore: r.metricStore})
	}
	if r.store != nil {
		collogspb.RegisterLogsServiceServer(r.grpcSrv, &logsService{store: r.store})
	}

	// HTTP server for OTLP HTTP (/v1/traces, /v1/metrics, /v1/logs).
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", r.handleHTTPTraces)
	if r.metricStore != nil {
		mux.HandleFunc("/v1/metrics", r.handleHTTPMetrics)
	}
	if r.store != nil {
		mux.HandleFunc("/v1/logs", r.handleHTTPLogs)
	}
	r.httpSrv = &http.Server{Handler: mux}

	// Bind synchronously so port conflicts fail fast.
	grpcLis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", grpcPort))
	if err != nil {
		return fmt.Errorf("OTLP gRPC listen on :%d: %w", grpcPort, err)
	}
	httpLis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", httpPort))
	if err != nil {
		grpcLis.Close()
		return fmt.Errorf("OTLP HTTP listen on :%d: %w", httpPort, err)
	}

	// Serve in goroutines; listeners are already bound.
	go func() {
		fmt.Printf("OTLP gRPC listening on %s\n", grpcLis.Addr())
		if err := r.grpcSrv.Serve(grpcLis); err != nil {
			fmt.Printf("receiver: gRPC serve error: %v\n", err)
		}
	}()
	go func() {
		fmt.Printf("OTLP HTTP listening on %s\n", httpLis.Addr())
		if err := r.httpSrv.Serve(httpLis); err != nil && err != http.ErrServerClosed {
			fmt.Printf("receiver: HTTP serve error: %v\n", err)
		}
	}()

	return nil
}

// Shutdown gracefully stops the receiver.
func (r *Receiver) Shutdown(ctx context.Context) error {
	if r.grpcSrv != nil {
		r.grpcSrv.GracefulStop()
	}
	if r.httpSrv != nil {
		return r.httpSrv.Shutdown(ctx)
	}
	return nil
}

// traceService implements the OTLP gRPC TraceService.
type traceService struct {
	coltracepb.UnimplementedTraceServiceServer
	pipeline *pipeline.Pipeline
}

// Export receives trace data via gRPC.
func (s *traceService) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	for _, resourceSpan := range req.ResourceSpans {
		resource := TranslateResource(resourceSpan.Resource)
		if resourceSpan.SchemaUrl != "" {
			resource.SchemaURL = resourceSpan.SchemaUrl
		}
		for _, scopeSpan := range resourceSpan.ScopeSpans {
			scope := TranslateScope(scopeSpan.Scope)
			if scopeSpan.SchemaUrl != "" {
				scope.SchemaURL = scopeSpan.SchemaUrl
			}
			spans := TranslateSpans(scopeSpan.Spans)

			if len(spans) == 0 {
				continue
			}

			batch := &pipeline.Batch{
				Resource: resource,
				Scope:    scope,
				Spans:    spans,
			}

			if err := s.pipeline.Ingest(batch); err != nil {
				return &coltracepb.ExportTraceServiceResponse{
					PartialSuccess: &coltracepb.ExportTracePartialSuccess{
						RejectedSpans: int64(len(spans)),
						ErrorMessage:  "pipeline buffer full",
					},
				}, nil
			}
		}
	}
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

// metricsService implements the OTLP gRPC MetricsService.
type metricsService struct {
	colmetricspb.UnimplementedMetricsServiceServer
	metricStore metrics.Store
}

// Export receives metrics data via gRPC.
func (s *metricsService) Export(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) (*colmetricspb.ExportMetricsServiceResponse, error) {
	points := TranslateMetrics(req)
	ilog.Debug("metrics gRPC: received %d metric points", len(points))
	for i, p := range points {
		if i >= 10 {
			ilog.Debug("metrics gRPC: ... (%d more points omitted)", len(points)-10)
			break
		}
		ilog.Debug("metrics gRPC:   %s{%v} = %f @ %d", p.Name, p.Labels, p.Value, p.Timestamp)
	}
	if len(points) == 0 {
		return &colmetricspb.ExportMetricsServiceResponse{}, nil
	}
	if err := s.metricStore.Insert(ctx, points); err != nil {
		return &colmetricspb.ExportMetricsServiceResponse{
			PartialSuccess: &colmetricspb.ExportMetricsPartialSuccess{
				RejectedDataPoints: int64(len(points)),
				ErrorMessage:       "store insert failed",
			},
		}, nil
	}
	return &colmetricspb.ExportMetricsServiceResponse{}, nil
}

// logsService implements the OTLP gRPC LogsService.
type logsService struct {
	collogspb.UnimplementedLogsServiceServer
	store storage.Store
}

// Export receives log data via gRPC.
func (s *logsService) Export(ctx context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
	logs := translateLogs(req.ResourceLogs)
	if len(logs) == 0 {
		return &collogspb.ExportLogsServiceResponse{}, nil
	}
	if err := s.store.InsertLogs(ctx, logs); err != nil {
		return &collogspb.ExportLogsServiceResponse{
			PartialSuccess: &collogspb.ExportLogsPartialSuccess{
				RejectedLogRecords: int64(len(logs)),
				ErrorMessage:       "store insert failed",
			},
		}, nil
	}
	return &collogspb.ExportLogsServiceResponse{}, nil
}

// handleHTTPTraces handles OTLP HTTP POST /v1/traces.
func (r *Receiver) handleHTTPTraces(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer req.Body.Close()

	var exportReq coltracepb.ExportTraceServiceRequest

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

	for _, resourceSpan := range exportReq.ResourceSpans {
		resource := TranslateResource(resourceSpan.Resource)
		if resourceSpan.SchemaUrl != "" {
			resource.SchemaURL = resourceSpan.SchemaUrl
		}
		for _, scopeSpan := range resourceSpan.ScopeSpans {
			scope := TranslateScope(scopeSpan.Scope)
			if scopeSpan.SchemaUrl != "" {
				scope.SchemaURL = scopeSpan.SchemaUrl
			}
			spans := TranslateSpans(scopeSpan.Spans)

			if len(spans) == 0 {
				continue
			}

			batch := &pipeline.Batch{
				Resource: resource,
				Scope:    scope,
				Spans:    spans,
			}
			if err := r.pipeline.Ingest(batch); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]string{"error": "pipeline full"})
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"partialSuccess": map[string]interface{}{},
	})
}

// handleHTTPMetrics handles OTLP HTTP POST /v1/metrics.
func (r *Receiver) handleHTTPMetrics(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer req.Body.Close()

	var exportReq colmetricspb.ExportMetricsServiceRequest
	if err := proto.Unmarshal(body, &exportReq); err != nil {
		http.Error(w, fmt.Sprintf("failed to unmarshal protobuf: %v", err), http.StatusBadRequest)
		return
	}

	points := TranslateMetrics(&exportReq)
	ilog.Debug("metrics HTTP: received %d metric points", len(points))
	for i, p := range points {
		if i >= 10 {
			ilog.Debug("metrics HTTP: ... (%d more points omitted)", len(points)-10)
			break
		}
		ilog.Debug("metrics HTTP:   %s{%v} = %f @ %d", p.Name, p.Labels, p.Value, p.Timestamp)
	}
	if len(points) == 0 {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"partialSuccess": map[string]interface{}{}})
		return
	}

	if err := r.metricStore.Insert(req.Context(), points); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "store insert failed"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"partialSuccess": map[string]interface{}{},
	})
}

// handleHTTPLogs handles OTLP HTTP POST /v1/logs.
func (r *Receiver) handleHTTPLogs(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer req.Body.Close()

	var exportReq collogspb.ExportLogsServiceRequest
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

	logs := translateLogs(exportReq.ResourceLogs)
	if len(logs) == 0 {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"partialSuccess": map[string]interface{}{}})
		return
	}

	if err := r.store.InsertLogs(req.Context(), logs); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "store insert failed"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"partialSuccess": map[string]interface{}{},
	})
}

// --- Translation helpers ---

func TranslateResource(resource *resourcepb.Resource) storage.ResourceInfo {
	if resource == nil {
		return storage.ResourceInfo{}
	}
	return storage.ResourceInfo{
		Attributes: keyValueToMap(resource.Attributes),
	}
}

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

func translateSpan(ps *tracepb.Span) storage.Span {
	var traceID [16]byte
	copy(traceID[:], ps.TraceId)

	var spanID [8]byte
	copy(spanID[:], ps.SpanId)

	var parentSpanID [8]byte
	copy(parentSpanID[:], ps.ParentSpanId)

	startMS := ps.StartTimeUnixNano / 1_000_000
	endMS := ps.EndTimeUnixNano / 1_000_000
	if endMS < startMS {
		endMS = startMS
	}
	durationMS := endMS - startMS

	// Normalize attributes BEFORE extracting typed columns,
	// so fallback keys (e.g. "input_tokens" from Claude Code)
	// are copied to standard keys (e.g. "gen_ai.usage.input_tokens")
	// that getUint32Attr/getStringAttr look for.
	attrs := normalizeAttributes(keyValueToMap(ps.Attributes))

	inputTokens := getUint32AttrFromMap(attrs, "gen_ai.usage.input_tokens")
	outputTokens := getUint32AttrFromMap(attrs, "gen_ai.usage.output_tokens")
	cacheCreationTokens := getUint32AttrFromMap(attrs, "gen_ai.usage.cache_creation_input_tokens")
	cacheReadTokens := getUint32AttrFromMap(attrs, "gen_ai.usage.cache_read_input_tokens")
	var totalTokens *uint32
	if inputTokens != nil || outputTokens != nil || cacheCreationTokens != nil || cacheReadTokens != nil {
		var sum uint32
		if inputTokens != nil {
			sum += *inputTokens
		}
		if outputTokens != nil {
			sum += *outputTokens
		}
		if cacheCreationTokens != nil {
			sum += *cacheCreationTokens
		}
		if cacheReadTokens != nil {
			sum += *cacheReadTokens
		}
		if tt := getUint32AttrFromMap(attrs, "gen_ai.usage.total_tokens"); tt != nil {
			sum = *tt
		}
		totalTokens = &sum
	}
	genAIModel := getStringAttrFromMap(attrs, "gen_ai.request.model")

	eventsJSON := serializeEvents(ps.Events)
	linksJSON := serializeLinks(ps.Links)

	return storage.Span{
		TraceID:           traceID,
		SpanID:            spanID,
		ParentSpanID:      parentSpanID,
		Name:              ps.Name,
		Kind:              int32(ps.Kind),
		StartTimeMS:       startMS,
		EndTimeMS:         endMS,
		DurationMS:        durationMS,
		Attributes:        attrs,
		Events:            eventsJSON,
		Links:             linksJSON,
		StatusCode:        int32(ps.Status.GetCode()),
		StatusMessage:     ps.Status.GetMessage(),
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		TotalTokens:       totalTokens,
		CacheCreationTokens: cacheCreationTokens,
		CacheReadTokens:     cacheReadTokens,
		GenAIRequestModel: genAIModel,
		TraceState:        ps.TraceState,
	}
}

// --- Attribute helpers ---

// attrAliases maps a standard OTel attribute key to fallback keys
// that various agents use instead. If the standard key is absent but
// a fallback key exists, the fallback value is copied to the standard key.
// Adding a new agent only requires appending its key names to the fallback lists.
var attrAliases = map[string][]string{
	// Token usage (GenAI semconv → Claude Code → OpenInference)
	"gen_ai.usage.input_tokens":  {"input_tokens", "llm.usage.input_tokens"},
	"gen_ai.usage.output_tokens": {"output_tokens", "llm.usage.output_tokens"},
	"gen_ai.usage.total_tokens":  {"total_tokens", "llm.usage.total_tokens"},
	// Prompt-caching tokens (Claude Code / Anthropic). These dominate real
	// token consumption but are reported as separate attributes, so they must
	// be folded into total_tokens to reflect true throughput.
	"gen_ai.usage.cache_creation_input_tokens": {"cache_creation_tokens", "cache_creation_input_tokens"},
	"gen_ai.usage.cache_read_input_tokens":     {"cache_read_tokens", "cache_read_input_tokens"},
	// Request model
	"gen_ai.request.model":       {"model", "llm.request.model"},
	// Session ID (JiuwenClaw → Claude Code → Codex)
	"jiuwenclaw.session.id":      {"session.id", "codex.session.id"},
}

// normalizeAttributes copies values from fallback keys to standard keys
// when the standard key is absent. Original keys are preserved.
// This makes downstream extraction (typed columns, session, etc.)
// work regardless of which agent produced the data.
func normalizeAttributes(attrs map[string]string) map[string]string {
	for stdKey, fallbacks := range attrAliases {
		if _, exists := attrs[stdKey]; exists {
			continue
		}
		for _, fb := range fallbacks {
			if v, exists := attrs[fb]; exists {
				attrs[stdKey] = v
				break
			}
		}
	}
	return attrs
}

func keyValueToMap(attrs []*commonpb.KeyValue) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv == nil || kv.Key == "" {
			continue
		}
		result[kv.Key] = anyValueToString(kv.Value)
	}
	return result
}

func anyValueToString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_IntValue:
		return fmt.Sprintf("%d", val.IntValue)
	case *commonpb.AnyValue_DoubleValue:
		return fmt.Sprintf("%f", val.DoubleValue)
	case *commonpb.AnyValue_BoolValue:
		return fmt.Sprintf("%t", val.BoolValue)
	case *commonpb.AnyValue_ArrayValue:
		b, _ := json.Marshal(val.ArrayValue.Values)
		return string(b)
	case *commonpb.AnyValue_KvlistValue:
		b, _ := json.Marshal(val.KvlistValue.Values)
		return string(b)
	default:
		return ""
	}
}

func getStringAttr(attrs []*commonpb.KeyValue, key string) *string {
	for _, kv := range attrs {
		if kv != nil && kv.Key == key {
			if sv := kv.Value.GetStringValue(); sv != "" {
				return &sv
			}
		}
	}
	return nil
}

func getUint32Attr(attrs []*commonpb.KeyValue, key string) *uint32 {
	for _, kv := range attrs {
		if kv != nil && kv.Key == key {
			if iv := kv.Value.GetIntValue(); iv > 0 {
				v := uint32(iv)
				return &v
			}
		}
	}
	return nil
}

// getUint32AttrFromMap reads a uint32 value from a normalized attributes map.
// Used after normalizeAttributes so fallback keys are already copied to standard keys.
func getUint32AttrFromMap(attrs map[string]string, key string) *uint32 {
	v, ok := attrs[key]
	if !ok {
		return nil
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil || n == 0 {
		return nil
	}
	u := uint32(n)
	return &u
}

// getStringAttrFromMap reads a string value from a normalized attributes map.
func getStringAttrFromMap(attrs map[string]string, key string) *string {
	v, ok := attrs[key]
	if !ok || v == "" {
		return nil
	}
	return &v
}

func serializeEvents(events []*tracepb.Span_Event) string {
	if len(events) == 0 {
		return "[]"
	}
	type eventJSON struct {
		TimeMS     uint64            `json:"time_ms"`
		Name       string            `json:"name"`
		Attributes map[string]string `json:"attributes"`
	}
	out := make([]eventJSON, 0, len(events))
	for _, e := range events {
		if e == nil {
			continue
		}
		out = append(out, eventJSON{
			TimeMS:     e.TimeUnixNano / 1_000_000,
			Name:       e.Name,
			Attributes: keyValueToMap(e.Attributes),
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func serializeLinks(links []*tracepb.Span_Link) string {
	if len(links) == 0 {
		return "[]"
	}
	type linkJSON struct {
		TraceID    string            `json:"trace_id"`
		SpanID     string            `json:"span_id"`
		Attributes map[string]string `json:"attributes"`
	}
	out := make([]linkJSON, 0, len(links))
	for _, l := range links {
		if l == nil {
			continue
		}
		out = append(out, linkJSON{
			TraceID:    fmt.Sprintf("%x", l.TraceId),
			SpanID:     fmt.Sprintf("%x", l.SpanId),
			Attributes: keyValueToMap(l.Attributes),
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}
