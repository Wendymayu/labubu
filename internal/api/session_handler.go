package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/labubu/labubu/internal/storage"
)

// SessionHandler holds HTTP handlers for session endpoints.
type SessionHandler struct {
	store storage.Store
}

// NewSessionHandler creates a new SessionHandler.
func NewSessionHandler(store storage.Store) *SessionHandler {
	return &SessionHandler{store: store}
}

// ListSessions handles GET /api/v1/sessions.
func (h *SessionHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
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

	query := storage.SessionQuery{
		Page:        page,
		PageSize:    pageSize,
		Service:     q.Get("service"),
		Query:       q.Get("q"),
		StartTimeMS: startMS,
		EndTimeMS:   endMS,
	}

	result, err := h.store.ListSessions(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("list sessions: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// GetSession handles GET /api/v1/sessions/:sessionId.
func (h *SessionHandler) GetSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	detail, err := h.store.GetSession(r.Context(), sessionID, page, pageSize)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get session: %v", err)})
		return
	}
	if detail == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// GetAgentStats handles GET /api/v1/sessions/{sessionId}/agent-stats.
func (h *SessionHandler) GetAgentStats(w http.ResponseWriter, r *http.Request, sessionID string) {
	result, err := h.store.GetSessionAgentStats(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get agent stats: %v", err)})
		return
	}
	if result == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_agent_data"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GetSessionContext handles GET /api/v1/sessions/{sessionId}/context.
// Returns the main agent's LLM spans (subagent spans excluded) for the
// session context bar chart.
func (h *SessionHandler) GetSessionContext(w http.ResponseWriter, r *http.Request, sessionID string) {
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}
	spans, err := h.store.GetSessionContextSpans(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get session context: %v", err)})
		return
	}
	if spans == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_context_data"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"spans": spans})
}
