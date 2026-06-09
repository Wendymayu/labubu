package alerting

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setupTestHandler(t *testing.T) (*AlertHandler, *RuleStore, func()) {
	t.Helper()
	dbPath := tempDB(t)
	store, err := NewRuleStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAlertHandler(store)
	cleanup := func() {
		store.Close()
	}
	return handler, store, cleanup
}

func TestListRules_Empty(t *testing.T) {
	handler, _, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/rules", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct{ Rules []Rule `json:"rules"` }
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Rules) != 0 {
		t.Fatalf("expected empty rules, got %d", len(resp.Rules))
	}
}

func TestCreateAndGetRule(t *testing.T) {
	handler, _, cleanup := setupTestHandler(t)
	defer cleanup()

	body := `{
		"name": "Test Rule",
		"metric": "total_tokens",
		"conditions": [{"field":"total_tokens","op":"gt","value":"100000"}],
		"for_duration": 300,
		"interval": 60,
		"notifier": {
			"type": "email",
			"smtp_host": "smtp.example.com",
			"smtp_port": 587,
			"username": "test@example.com",
			"password": "pass",
			"recipients": ["alerts@example.com"]
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/rules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created Rule
	json.NewDecoder(rec.Body).Decode(&created)
	if created.Name != "Test Rule" || created.ID == "" {
		t.Fatalf("unexpected created rule: %+v", created)
	}

	// Now list.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/rules", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	var list struct{ Rules []Rule `json:"rules"` }
	json.NewDecoder(rec2.Body).Decode(&list)
	if len(list.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(list.Rules))
	}
}

func TestCreateRule_Validation(t *testing.T) {
	handler, _, cleanup := setupTestHandler(t)
	defer cleanup()

	tests := []struct {
		name string
		body string
		code int
	}{
		{"empty name", `{"metric":"total_tokens"}`, http.StatusBadRequest},
		{"invalid json", `{bad}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/rules", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.code {
				t.Errorf("expected %d, got %d", tt.code, rec.Code)
			}
		})
	}
}

func TestUpdateRule(t *testing.T) {
	handler, store, cleanup := setupTestHandler(t)
	defer cleanup()

	r := Rule{
		ID:          "ruuid-1",
		Name:        "Original",
		Metric:      "total_tokens",
		Conditions:  []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
		ForDuration: 300,
		Interval:    60,
		Notifier:    NotifierConfig{Type: "email"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	store.CreateRule(r)

	update := `{"name":"Updated","metric":"total_tokens","conditions":[{"field":"total_tokens","op":"gt","value":"200"}],"for_duration":600,"interval":120,"notifier":{"type":"email"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alerts/rules/ruuid-1", strings.NewReader(update))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := store.GetRule("ruuid-1")
	if got.Name != "Updated" || got.ForDuration != 600 {
		t.Fatalf("unexpected rule after update: %+v", got)
	}
}

func TestDeleteRule(t *testing.T) {
	handler, store, cleanup := setupTestHandler(t)
	defer cleanup()

	r := Rule{
		ID:          "ruuid-1",
		Name:        "ToDelete",
		Metric:      "total_tokens",
		Conditions:  nil,
		ForDuration: 300,
		Interval:    60,
		Notifier:    NotifierConfig{Type: "email"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	store.CreateRule(r)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alerts/rules/ruuid-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	rules, _ := store.ListRules()
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
}

func TestListStates(t *testing.T) {
	handler, store, cleanup := setupTestHandler(t)
	defer cleanup()

	state := AlertState{
		ID:          "s1",
		RuleID:      "r1",
		TraceIDHex:  "t1",
		Status:      AlertFiring,
		TriggeredAt: time.Now(),
	}
	store.UpsertAlertState(state)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/states?status=firing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct{ States []AlertState `json:"states"` }
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.States) != 1 {
		t.Fatalf("expected 1 state, got %d", len(resp.States))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	handler, _, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/states", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}
