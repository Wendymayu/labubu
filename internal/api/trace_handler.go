package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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

	query := storage.TraceQuery{
		Page:        page,
		PageSize:    pageSize,
		Service:     q.Get("service"),
		Status:      q.Get("status"),
		Query:       q.Get("q"),
		StartTimeMS: startMS,
		EndTimeMS:   endMS,
		MinDuration: minDuration,
		MaxDuration: maxDuration,
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
		writeOTLPResponse(w, detail)
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
// Accepts a list of trace IDs and returns them as an OTLP JSON array.
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

	results := make([]otlpTraceResponse, 0, len(req.TraceIDs))
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

		results = append(results, convertToOTLP(detail))
	}

	writeJSON(w, http.StatusOK, results)
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

	// Call LLM.
	diagResp, rawResponse, err := callLLMForDiagnosis(r.Context(), defaultConfig, systemPrompt, userPrompt)
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