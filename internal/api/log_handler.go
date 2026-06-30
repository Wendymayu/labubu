package api

import (
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/labubu/labubu/internal/storage"
)

// LogHandler handles log-related API endpoints.
type LogHandler struct {
	store storage.Store
}

// NewLogHandler creates a new LogHandler.
func NewLogHandler(store storage.Store) *LogHandler {
	return &LogHandler{store: store}
}

// ServeHTTP dispatches requests to the appropriate handler based on URL path.
func (h *LogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/logs")
	if path == "" || path == "/" {
		h.ListLogs(w, r)
		return
	}
	traceIDHex := strings.TrimPrefix(path, "/")
	if strings.HasSuffix(traceIDHex, "/counts") {
		id := strings.TrimSuffix(traceIDHex, "/counts")
		h.GetLogCountsByTrace(w, r, id)
		return
	}
	h.GetLogsByTrace(w, r, traceIDHex)
}

// ListLogs handles GET /api/v1/logs with filtering and pagination.
func (h *LogHandler) ListLogs(w http.ResponseWriter, r *http.Request) {
	q := parseLogQuery(r)

	result, err := h.store.ListLogs(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if result == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"logs":       []interface{}{},
			"pagination": storage.Pagination{Page: q.Page, PageSize: q.PageSize, Total: 0},
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// GetLogsByTrace handles GET /api/v1/logs/{traceIdHex}.
func (h *LogHandler) GetLogsByTrace(w http.ResponseWriter, r *http.Request, traceIDHex string) {
	traceID, err := hex.DecodeString(traceIDHex)
	if err != nil || len(traceID) != 16 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trace_id"})
		return
	}

	var tid [16]byte
	copy(tid[:], traceID)

	logs, err := h.store.GetLogsByTrace(r.Context(), tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if logs == nil {
		logs = make([]storage.LogListItem, 0)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs": logs,
	})
}

// GetLogCountsByTrace handles GET /api/v1/logs/{traceIdHex}/counts.
func (h *LogHandler) GetLogCountsByTrace(w http.ResponseWriter, r *http.Request, traceIDHex string) {
	traceID, err := hex.DecodeString(traceIDHex)
	if err != nil || len(traceID) != 16 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trace_id"})
		return
	}

	var tid [16]byte
	copy(tid[:], traceID)

	counts, err := h.store.GetLogCountsByTrace(r.Context(), tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if counts == nil {
		counts = map[string]int{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"counts": counts,
	})
}

// GetEventNames handles GET /api/v1/log-event-names.
func (h *LogHandler) GetEventNames(w http.ResponseWriter, r *http.Request) {
	names, err := h.store.GetLogEventNames(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if names == nil {
		names = make([]string, 0)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"event_names": names,
	})
}

// parseLogQuery extracts LogQuery from HTTP request parameters.
func parseLogQuery(r *http.Request) storage.LogQuery {
	q := storage.LogQuery{
		Page:     1,
		PageSize: 20,
	}

	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Page = n
		}
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			q.PageSize = n
		}
	}

	q.Severity = r.URL.Query().Get("severity")
	q.EventName = r.URL.Query().Get("event_name")
	q.Query = r.URL.Query().Get("q")

	if v := r.URL.Query().Get("trace_id"); v != "" {
		if b, err := hex.DecodeString(v); err == nil && len(b) == 16 {
			copy(q.TraceID[:], b)
		}
	}
	if v := r.URL.Query().Get("span_id"); v != "" {
		if b, err := hex.DecodeString(v); err == nil && len(b) == 8 {
			copy(q.SpanID[:], b)
		}
	}

	if v := r.URL.Query().Get("start"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.StartTime = uint64(n)
		}
	}
	if v := r.URL.Query().Get("end"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.EndTime = uint64(n)
		}
	}

	if v := r.URL.Query().Get("asc"); v == "true" || v == "1" {
		q.Asc = true
	}

	return q
}
