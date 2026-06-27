package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labubu/labubu/internal/storage"
)

// CostHandler holds HTTP handlers for cost summary endpoints.
type CostHandler struct {
	store storage.Store
}

// NewCostHandler creates a new CostHandler.
func NewCostHandler(store storage.Store) *CostHandler {
	return &CostHandler{store: store}
}

// ServeHTTP dispatches cost summary requests.
func (h *CostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	h.summary(w, r)
}

func (h *CostHandler) summary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	groupBy := q.Get("group_by")
	if groupBy == "" {
		groupBy = "model"
	}
	if groupBy != "model" && groupBy != "service" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group_by, use: model or service"})
		return
	}

	var startMS, endMS uint64
	var period string

	// A custom start/end range (epoch ms) overrides the preset period.
	startStr := q.Get("start")
	endStr := q.Get("end")
	if startStr != "" || endStr != "" {
		if startStr == "" || endStr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "custom range requires both start and end (epoch ms)"})
			return
		}
		s, err1 := strconv.ParseUint(startStr, 10, 64)
		e, err2 := strconv.ParseUint(endStr, 10, 64)
		if err1 != nil || err2 != nil || s > e {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid start/end epoch ms"})
			return
		}
		startMS, endMS = s, e
		period = "custom"
	} else {
		period = q.Get("period")
		if period == "" {
			period = "7d"
		}
		now := time.Now()
		endMS = uint64(now.UnixMilli())
		switch period {
		case "today":
			startMS = uint64(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli())
		case "7d":
			startMS = uint64(now.Add(-7 * 24 * time.Hour).UnixMilli())
		case "30d":
			startMS = uint64(now.Add(-30 * 24 * time.Hour).UnixMilli())
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid period, use: today, 7d, 30d"})
			return
		}
	}

	result, err := h.store.GetCostSummary(r.Context(), storage.CostQuery{
		StartTimeMS: startMS,
		EndTimeMS:   endMS,
		GroupBy:     groupBy,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Set the period label in the result.
	result.Period = period
	result.GroupBy = groupBy

	writeJSON(w, http.StatusOK, result)
}