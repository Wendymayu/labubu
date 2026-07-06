package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/labubu/labubu/internal/receiver"
	"github.com/labubu/labubu/internal/storage"
)

// TraceHandler holds HTTP handlers for trace endpoints.
type TraceHandler struct {
	store      storage.Store
	inFlightMu sync.Mutex
	inFlight   map[[16]byte]struct{}
}

// NewTraceHandler creates a new TraceHandler.
func NewTraceHandler(store storage.Store) *TraceHandler {
	return &TraceHandler{
		store:    store,
		inFlight: make(map[[16]byte]struct{}),
	}
}

// ListTraces handles GET /api/v1/traces.
func (h *TraceHandler) ListTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	startMS, _ := strconv.ParseUint(q.Get("start"), 10, 64)
	endMS, _ := strconv.ParseUint(q.Get("end"), 10, 64)
	minDuration, _ := strconv.ParseUint(q.Get("min_duration"), 10, 64)
	maxDuration, _ := strconv.ParseUint(q.Get("max_duration"), 10, 64)
	minSpans, _ := strconv.ParseUint(q.Get("min_spans"), 10, 16)
	maxSpans, _ := strconv.ParseUint(q.Get("max_spans"), 10, 16)
	minCost, _ := strconv.ParseFloat(q.Get("min_cost"), 64)
	maxCost, _ := strconv.ParseFloat(q.Get("max_cost"), 64)

	query := storage.TraceQuery{
		Page:         page,
		PageSize:     pageSize,
		Service:      q.Get("service"),
		Status:       q.Get("status"),
		Query:        q.Get("q"),
		StartTimeMS:  startMS,
		EndTimeMS:    endMS,
		MinDuration:  minDuration,
		MaxDuration:  maxDuration,
		MinSpanCount: uint16(minSpans),
		MaxSpanCount: uint16(maxSpans),
		MinCost:      minCost,
		MaxCost:      maxCost,
	}

	result, err := h.store.ListTraces(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("list traces: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// GetTrace handles GET /api/v1/traces/{traceIdHex}.
func (h *TraceHandler) GetTrace(w http.ResponseWriter, r *http.Request, traceIDHex string) {
	if len(traceIDHex) != 32 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace_id must be a 32-character hex string"})
		return
	}

	traceIDBytes, err := hex.DecodeString(traceIDHex)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid hex trace_id: %v", err)})
		return
	}

	var traceID [16]byte
	copy(traceID[:], traceIDBytes)

	detail, err := h.store.GetTrace(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get trace: %v", err)})
		return
	}
	if detail == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trace not found"})
		return
	}

	if r.URL.Query().Get("format") == "otlp" {
		td := convertToProto(detail)
		jsonBytes, err := marshalOTLPJSON(td)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("marshal otlp: %v", err)})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jsonBytes)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"trace": detail})
}

// GetServices handles GET /api/v1/services.
func (h *TraceHandler) GetServices(w http.ResponseWriter, r *http.Request) {
	services, err := h.store.GetServices(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get services: %v", err)})
		return
	}
	if services == nil {
		services = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"services": services})
}

// ExportTraces handles POST /api/v1/traces/export.
// Accepts a list of trace IDs and returns them as a single OTLP TracesData JSON envelope.
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

	// Collect all ResourceSpans into a single TracesData.
	allResourceSpans := make([]*tracepb.ResourceSpans, 0, len(req.TraceIDs))
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
			continue // silently skip missing traces
		}

		td := convertToProto(detail)
		allResourceSpans = append(allResourceSpans, td.ResourceSpans...)
	}

	tracesData := &tracepb.TracesData{
		ResourceSpans: allResourceSpans,
	}
	jsonBytes, err := marshalOTLPJSON(tracesData)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("marshal otlp: %v", err)})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonBytes)
}

// DeleteTraces handles POST /api/v1/traces/delete.
// Removes the given traces and their associated spans, logs, and diagnosis
// results. Unknown IDs are silently ignored.
func (h *TraceHandler) DeleteTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST allowed"})
		return
	}

	var req struct {
		TraceIDs []string `json:"trace_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	if len(req.TraceIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace_ids must not be empty"})
		return
	}
	if len(req.TraceIDs) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max 100 traces per delete"})
		return
	}

	traceIDs := make([][16]byte, 0, len(req.TraceIDs))
	for _, hexID := range req.TraceIDs {
		if len(hexID) != 32 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid trace_id length: %s (must be 32 hex chars)", hexID)})
			return
		}
		b, err := hex.DecodeString(hexID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid hex trace_id %s: %v", hexID, err)})
			return
		}
		var id [16]byte
		copy(id[:], b)
		traceIDs = append(traceIDs, id)
	}

	deletedTraces, deletedLogs, err := h.store.DeleteTraces(r.Context(), traceIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("delete traces: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{
		"deleted_traces": deletedTraces,
		"deleted_logs":   deletedLogs,
	})
}

// GetDiagnosis handles GET /api/v1/traces/{traceIdHex}/diagnosis.
func (h *TraceHandler) GetDiagnosis(w http.ResponseWriter, r *http.Request, traceIDHex string) {
	if len(traceIDHex) != 32 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace_id must be a 32-character hex string"})
		return
	}

	traceIDBytes, err := hex.DecodeString(traceIDHex)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid hex trace_id: %v", err)})
		return
	}
	var traceID [16]byte
	copy(traceID[:], traceIDBytes)

	result, err := h.store.GetDiagnosisResult(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get diagnosis: %v", err)})
		return
	}
	if result == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_diagnosis"})
		return
	}

	// Check staleness.
	detail, err := h.store.GetTrace(r.Context(), traceID)
	if err != nil || detail == nil {
		// Can't check staleness — return result as-is.
		result.TraceIDHex = traceIDHex
		writeJSON(w, http.StatusOK, result)
		return
	}
	currentSnapshot := computeSpanSnapshot(detail.Spans)
	result.Stale = (currentSnapshot != result.SpansSnapshot)
	result.TraceIDHex = traceIDHex

	writeJSON(w, http.StatusOK, result)
}

// DiagnoseTrace handles POST /api/v1/traces/{traceIdHex}/diagnose.
func (h *TraceHandler) DiagnoseTrace(w http.ResponseWriter, r *http.Request, traceIDHex string) {
	if len(traceIDHex) != 32 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace_id must be a 32-character hex string"})
		return
	}

	traceIDBytes, err := hex.DecodeString(traceIDHex)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid hex trace_id: %v", err)})
		return
	}
	var traceID [16]byte
	copy(traceID[:], traceIDBytes)

	force := r.URL.Query().Get("force") == "true"
	locale := r.URL.Query().Get("locale") // "zh" or "en"

	// Check for cached result (unless force).
	if !force {
		existing, err := h.store.GetDiagnosisResult(r.Context(), traceID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get diagnosis: %v", err)})
			return
		}
		if existing != nil {
			detail, err := h.store.GetTrace(r.Context(), traceID)
			if err == nil && detail != nil {
				currentSnapshot := computeSpanSnapshot(detail.Spans)
				if currentSnapshot == existing.SpansSnapshot {
					existing.Stale = false
					existing.TraceIDHex = traceIDHex
					writeJSON(w, http.StatusOK, existing)
					return
				}
			}
		}
	}

	// In-flight check to prevent duplicate LLM calls.
	h.inFlightMu.Lock()
	if _, ok := h.inFlight[traceID]; ok {
		h.inFlightMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "diagnosis_in_flight"})
		return
	}
	h.inFlight[traceID] = struct{}{}
	h.inFlightMu.Unlock()
	defer func() {
		h.inFlightMu.Lock()
		delete(h.inFlight, traceID)
		h.inFlightMu.Unlock()
	}()

	// Load trace data.
	detail, err := h.store.GetTrace(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get trace: %v", err)})
		return
	}
	if detail == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trace_not_found"})
		return
	}

	logs, _ := h.store.GetLogsByTrace(r.Context(), traceID) // logs are optional

	// Get default LLM config.
	configs, err := h.store.GetLLMConfigs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get llm configs: %v", err)})
		return
	}
	var defaultConfig *storage.LLMConfig
	for i := range configs {
		if configs[i].IsDefault {
			defaultConfig = &configs[i]
			break
		}
	}
	if defaultConfig == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no_default_model"})
		return
	}

	// Build prompt.
	systemPrompt := buildDiagnosisSystemPrompt(locale)
	userPrompt := buildDiagnosisUserPrompt(detail, logs)

	// Call LLM with a detached context so the call completes even if the client disconnects.
	// This avoids wasting an LLM API call when the user navigates away mid-diagnosis.
	llmCtx, llmCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer llmCancel()
	diagResp, rawResponse, err := callLLMForDiagnosis(llmCtx, defaultConfig, systemPrompt, userPrompt)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("llm_call_failed: %v", err)})
		return
	}

	// Build result.
	snapshot := computeSpanSnapshot(detail.Spans)
	result := &storage.DiagnosisResult{
		TraceID:       traceID,
		TraceIDHex:    traceIDHex,
		ModelName:     defaultConfig.ModelName,
		Scores:        diagResp.Scores,
		OverallScore:  uint8(diagResp.OverallScore),
		Findings:      diagResp.Findings,
		Summary:       diagResp.Summary,
		SpansSnapshot: snapshot,
		RawResponse:   rawResponse,
		CreatedAt:     time.Now(),
		Stale:         false,
	}

	// Store result.
	if err := h.store.UpsertDiagnosisResult(r.Context(), result); err != nil {
		// Log but don't fail — return the result even if storage fails.
		fmt.Printf("api: failed to store diagnosis result: %v\n", err)
	}

	writeJSON(w, http.StatusOK, result)
}

// computeSpanSnapshot creates a deterministic fingerprint of trace spans for staleness detection.
func computeSpanSnapshot(spans []storage.SpanDetail) string {
	parts := make([]string, 0, len(spans)+1)
	parts = append(parts, fmt.Sprintf("%d", len(spans)))
	for _, s := range spans {
		parts = append(parts, fmt.Sprintf("%s:%s:%d", s.SpanID, s.Status, s.DurationMS))
	}
	return strings.Join(parts, "|")
}

// writeJSON serializes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Printf("api: json encode error: %v\n", err)
	}
}

const maxImportSize = 10 * 1024 * 1024 // 10MB

// ImportTraces handles POST /api/v1/traces/import.
// Accepts OTLP JSON (TracesData or ExportTraceServiceRequest format),
// deserializes with protojson, and ingests new traces via the storage pipeline.
// Traces that already exist in the DB are skipped entirely.
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

			// Determine which trace_ids in this batch are new.
			// A trace that already exists in the DB is skipped ENTIRELY — its
			// spans are not re-inserted. Re-inserting only a subset (the import
			// batch is ordered by start_time_ms, so the root span comes first and
			// would be the one dropped) corrupts the stored trace: the root
			// vanishes and the trace ends up with no root span.
			newSpans := make([]storage.Span, 0, len(spans))
			seenTraceIDs := make(map[[16]byte]bool)  // trace already added in this batch
			skipTraceIDs := make(map[[16]byte]bool) // trace exists in DB — skip all its spans
			for _, span := range spans {
				if skipTraceIDs[span.TraceID] {
					continue
				}
				if seenTraceIDs[span.TraceID] {
					newSpans = append(newSpans, span)
					continue
				}
				seenTraceIDs[span.TraceID] = true

				existing, err := h.store.GetTrace(r.Context(), span.TraceID)
				if err != nil || existing != nil {
					skipTraceIDs[span.TraceID] = true
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