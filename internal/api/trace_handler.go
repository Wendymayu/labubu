package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labubu/labubu/internal/storage"
)

// TraceHandler holds HTTP handlers for trace endpoints.
type TraceHandler struct {
	store storage.Store
}

// NewTraceHandler creates a new TraceHandler.
func NewTraceHandler(store storage.Store) *TraceHandler {
	return &TraceHandler{store: store}
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

// writeJSON serializes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Printf("api: json encode error: %v\n", err)
	}
}
