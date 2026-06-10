package alerting

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AlertHandler serves HTTP endpoints for alert rules, states, and notifications.
type AlertHandler struct {
	store *RuleStore
}

// NewAlertHandler creates a new AlertHandler.
func NewAlertHandler(store *RuleStore) *AlertHandler {
	return &AlertHandler{store: store}
}

// ServeHTTP dispatches alert API requests.
func (h *AlertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts")
	path = strings.TrimPrefix(path, "/")

	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "rules" || path == "rules/":
		switch r.Method {
		case http.MethodGet:
			h.listRules(w, r)
		case http.MethodPost:
			h.createRule(w, r)
		default:
			writeAlertJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case strings.HasPrefix(path, "rules/"):
		ruleID := strings.TrimPrefix(path, "rules/")
		ruleID = strings.TrimSuffix(ruleID, "/")
		switch r.Method {
		case http.MethodGet:
			h.getRule(w, r, ruleID)
		case http.MethodPut:
			h.updateRule(w, r, ruleID)
		case http.MethodDelete:
			h.deleteRule(w, r, ruleID)
		default:
			writeAlertJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case path == "states" || strings.HasPrefix(path, "states"):
		if r.Method == http.MethodGet {
			h.listStates(w, r)
		} else {
			writeAlertJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case path == "notifications" || strings.HasPrefix(path, "notifications"):
		if r.Method == http.MethodGet {
			h.listNotifications(w, r)
		} else {
			writeAlertJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	default:
		writeAlertJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// --- Rule handlers ---

func (h *AlertHandler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListRules()
	if err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rules == nil {
		rules = []Rule{}
	}
	writeAlertJSON(w, http.StatusOK, map[string]interface{}{"rules": rules})
}

func (h *AlertHandler) createRule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string         `json:"name"`
		Metric      string         `json:"metric"`
		Conditions  []Condition    `json:"conditions"`
		ForDuration int            `json:"for_duration"`
		Interval    int            `json:"interval"`
		Notifier    NotifierConfig `json:"notifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAlertJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if input.Name == "" {
		writeAlertJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if input.Metric == "" {
		input.Metric = "total_tokens"
	}
	if input.ForDuration <= 0 {
		input.ForDuration = 300
	}
	if input.Interval <= 0 {
		input.Interval = 60
	}
	if input.Conditions == nil {
		input.Conditions = []Condition{}
	}

	now := time.Now()
	rule := Rule{
		ID:          uuid.New().String(),
		Name:        input.Name,
		Enabled:     true,
		Metric:      input.Metric,
		Conditions:  input.Conditions,
		ForDuration: input.ForDuration,
		Interval:    input.Interval,
		Notifier:    input.Notifier,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.store.CreateRule(rule); err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeAlertJSON(w, http.StatusCreated, rule)
}

func (h *AlertHandler) getRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	rule, err := h.store.GetRule(ruleID)
	if err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rule == nil {
		writeAlertJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
		return
	}
	writeAlertJSON(w, http.StatusOK, rule)
}

func (h *AlertHandler) updateRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	existing, err := h.store.GetRule(ruleID)
	if err != nil || existing == nil {
		writeAlertJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
		return
	}

	var input struct {
		Name        string         `json:"name"`
		Enabled     *bool          `json:"enabled"`
		Metric      string         `json:"metric"`
		Conditions  []Condition    `json:"conditions"`
		ForDuration int            `json:"for_duration"`
		Interval    int            `json:"interval"`
		Notifier    NotifierConfig `json:"notifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAlertJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if input.Name != "" {
		existing.Name = input.Name
	}
	if input.Enabled != nil {
		existing.Enabled = *input.Enabled
	}
	if input.Metric != "" {
		existing.Metric = input.Metric
	}
	if input.Conditions != nil {
		existing.Conditions = input.Conditions
	}
	if input.ForDuration > 0 {
		existing.ForDuration = input.ForDuration
	}
	if input.Interval > 0 {
		existing.Interval = input.Interval
	}
	if input.Notifier.Type != "" {
		existing.Notifier = input.Notifier
	}
	existing.UpdatedAt = time.Now()

	if err := h.store.UpdateRule(*existing); err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeAlertJSON(w, http.StatusOK, existing)
}

func (h *AlertHandler) deleteRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	if err := h.store.DeleteRule(ruleID); err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeAlertJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- State handlers ---

func (h *AlertHandler) listStates(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	states, err := h.store.ListAlertStates(statusFilter)
	if err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if states == nil {
		states = []AlertState{}
	}
	writeAlertJSON(w, http.StatusOK, map[string]interface{}{"states": states})
}

// --- Notification handlers ---

func (h *AlertHandler) listNotifications(w http.ResponseWriter, r *http.Request) {
	ruleIDFilter := r.URL.Query().Get("rule_id")
	notifications, err := h.store.ListNotifications(ruleIDFilter)
	if err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if notifications == nil {
		notifications = []AlertNotification{}
	}
	writeAlertJSON(w, http.StatusOK, map[string]interface{}{"notifications": notifications})
}

func writeAlertJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
