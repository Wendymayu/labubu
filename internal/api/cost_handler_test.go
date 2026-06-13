package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

func TestCostSummaryDefaultPeriod(t *testing.T) {
	cost := 5.0
	tokens := uint64(10000)
	store := &handlerMockStore{
		costSummary: &storage.CostSummaryResult{
			Period:   "7d",
			Currency: "USD",
			Overview: storage.CostOverview{
				TotalCost:       cost,
				TotalTokens:     tokens,
				TraceCount:      10,
				AvgCostPerTrace: 0.5,
			},
			ByModel: []storage.ModelCostItem{
				{Model: "claude-sonnet-4-5", Cost: 3.0, Tokens: 6000, TraceCount: 6, AvgCost: 0.5},
				{Model: "gpt-4o", Cost: 2.0, Tokens: 4000, TraceCount: 4, AvgCost: 0.5},
			},
		},
	}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.CostSummaryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result.Period != "7d" {
		t.Errorf("expected period '7d', got '%s'", result.Period)
	}
	if result.Overview.TotalCost != 5.0 {
		t.Errorf("expected total_cost 5.0, got %f", result.Overview.TotalCost)
	}
	if len(result.ByModel) != 2 {
		t.Errorf("expected 2 models, got %d", len(result.ByModel))
	}
}

func TestCostSummaryTodayPeriod(t *testing.T) {
	store := &handlerMockStore{
		costSummary: &storage.CostSummaryResult{
			Period:   "today",
			Currency: "USD",
			Overview: storage.CostOverview{TotalCost: 1.0, TraceCount: 2},
		},
	}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary?period=today", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.CostSummaryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Period != "today" {
		t.Errorf("expected period 'today', got '%s'", result.Period)
	}
}

func TestCostSummary30dPeriod(t *testing.T) {
	store := &handlerMockStore{
		costSummary: &storage.CostSummaryResult{
			Period:   "30d",
			Currency: "USD",
			Overview: storage.CostOverview{TotalCost: 50.0, TraceCount: 100},
		},
	}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary?period=30d", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.CostSummaryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Period != "30d" {
		t.Errorf("expected period '30d', got '%s'", result.Period)
	}
}

func TestCostSummaryZeroValues(t *testing.T) {
	store := &handlerMockStore{
		costSummary: &storage.CostSummaryResult{
			Period:   "7d",
			Currency: "",
			Overview: storage.CostOverview{},
			ByModel:  []storage.ModelCostItem{},
		},
	}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.CostSummaryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Overview.TotalCost != 0 {
		t.Errorf("expected total_cost 0, got %f", result.Overview.TotalCost)
	}
	if result.Overview.TraceCount != 0 {
		t.Errorf("expected trace_count 0, got %d", result.Overview.TraceCount)
	}
	if len(result.ByModel) != 0 {
		t.Errorf("expected 0 models, got %d", len(result.ByModel))
	}
}

func TestCostSummaryInvalidPeriod(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary?period=invalid", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid period, got %d", rec.Code)
	}
}

func TestCostSummaryMethodNotAllowed(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cost-summary", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}
}