package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupDashboardTestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "labubu-test-dashboards-"+t.Name())
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestDashboardHandler_CreateList(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create a panel.
	createBody := map[string]interface{}{
		"title":     "Test Panel",
		"metric":    "cpu_usage",
		"labels":    map[string]string{"host": "a"},
		"chartType": "line",
		"step":      60,
	}
	b, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboards", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var created PanelConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if created.ID == "" {
		t.Error("expected non-empty ID")
	}
	if created.Title != "Test Panel" {
		t.Errorf("expected title 'Test Panel', got %q", created.Title)
	}

	// List panels.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboards", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var listResp struct {
		Panels []PanelConfig `json:"panels"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if len(listResp.Panels) != 1 {
		t.Errorf("expected 1 panel, got %d", len(listResp.Panels))
	}
}

func TestDashboardHandler_UpdateDelete(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create first.
	createBody := map[string]interface{}{
		"title":     "Old Title",
		"metric":    "cpu_usage",
		"labels":    nil,
		"chartType": "bar",
	}
	b, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboards", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var created PanelConfig
	json.Unmarshal(rec.Body.Bytes(), &created)

	// Update.
	updateBody := PanelConfig{
		ID:        created.ID,
		Title:     "Updated Title",
		Metric:    "memory_usage",
		Labels:    map[string]string{"region": "us"},
		ChartType: "stat",
	}
	b2, _ := json.Marshal(updateBody)
	req2 := httptest.NewRequest(http.MethodPut, "/api/v1/dashboards/"+created.ID, bytes.NewReader(b2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var updated PanelConfig
	json.Unmarshal(rec2.Body.Bytes(), &updated)
	if updated.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", updated.Title)
	}
}

func TestDashboardHandler_Delete(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create first.
	createBody := map[string]interface{}{
		"title":     "ToDelete",
		"metric":    "cpu_usage",
		"labels":    nil,
		"chartType": "stat",
	}
	b, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboards", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var created PanelConfig
	json.Unmarshal(rec.Body.Bytes(), &created)

	// Delete.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/dashboards/"+created.ID, nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// Verify list is empty.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboards", nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	var listResp struct {
		Panels []PanelConfig `json:"panels"`
	}
	json.Unmarshal(rec3.Body.Bytes(), &listResp)
	if len(listResp.Panels) != 0 {
		t.Errorf("expected 0 panels after delete, got %d", len(listResp.Panels))
	}
}

func TestDashboardHandler_NotFound(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Update non-existent.
	req := httptest.NewRequest(http.MethodPut, "/api/v1/dashboards/nonexistent", bytes.NewReader([]byte(`{"id":"nonexistent","title":"x","metric":"x","chartType":"stat"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent panel, got %d", rec.Code)
	}

	// Delete non-existent.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/dashboards/nonexistent", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent panel, got %d", rec2.Code)
	}
}

func TestDashboardHandler_InvalidJSON(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboards", bytes.NewReader([]byte(`not json`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}
