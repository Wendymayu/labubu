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

func doJSON(t *testing.T, method, url string, body interface{}) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	return rec, req
}

func TestDashboardHandler_CreateAndListDashboards(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create first dashboard.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Claude Code"})
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create dashboard: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Error("expected non-empty dashboard ID")
	}
	if created.Name != "Claude Code" {
		t.Errorf("expected name 'Claude Code', got %q", created.Name)
	}

	// Create second dashboard.
	rec2, req2 := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Jiuwenclaw"})
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("create second dashboard: expected 200, got %d", rec2.Code)
	}

	// List dashboards.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboards", nil)
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("list dashboards: expected 200, got %d: %s", rec3.Code, rec3.Body.String())
	}

	var listResp struct {
		Dashboards []struct {
			ID     string        `json:"id"`
			Name   string        `json:"name"`
			Panels []PanelConfig `json:"panels"`
		} `json:"dashboards"`
	}
	json.Unmarshal(rec3.Body.Bytes(), &listResp)
	if len(listResp.Dashboards) != 2 {
		t.Errorf("expected 2 dashboards, got %d", len(listResp.Dashboards))
	}
	for _, d := range listResp.Dashboards {
		if len(d.Panels) != 0 {
			t.Errorf("expected 0 panels for new dashboard %q, got %d", d.Name, len(d.Panels))
		}
	}
}

func TestDashboardHandler_CreatePanelInDashboard(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create a dashboard first.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Test"})
	handler.ServeHTTP(rec, req)
	var dash struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(rec.Body.Bytes(), &dash)

	// Create a panel in the dashboard.
	panelBody := map[string]interface{}{
		"title":     "CPU Usage",
		"metric":    "cpu_usage",
		"labels":    map[string]string{"host": "a"},
		"chartType": "line",
		"step":      60,
	}
	rec2, req2 := doJSON(t, http.MethodPost, "/api/v1/dashboards/"+dash.ID+"/panels", panelBody)
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("create panel: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var panel PanelConfig
	json.Unmarshal(rec2.Body.Bytes(), &panel)
	if panel.ID == "" {
		t.Error("expected non-empty panel ID")
	}
	if panel.Title != "CPU Usage" {
		t.Errorf("expected title 'CPU Usage', got %q", panel.Title)
	}

	// List dashboards to verify panel is nested.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboards", nil)
	handler.ServeHTTP(rec3, req3)

	var listResp struct {
		Dashboards []struct {
			ID     string        `json:"id"`
			Panels []PanelConfig `json:"panels"`
		} `json:"dashboards"`
	}
	json.Unmarshal(rec3.Body.Bytes(), &listResp)
	if len(listResp.Dashboards) != 1 {
		t.Fatalf("expected 1 dashboard, got %d", len(listResp.Dashboards))
	}
	if len(listResp.Dashboards[0].Panels) != 1 {
		t.Errorf("expected 1 panel, got %d", len(listResp.Dashboards[0].Panels))
	}
}

func TestDashboardHandler_UpdatePanel(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create dashboard.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Test"})
	handler.ServeHTTP(rec, req)
	var dash struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &dash)

	// Create panel.
	rec2, req2 := doJSON(t, http.MethodPost, "/api/v1/dashboards/"+dash.ID+"/panels", map[string]interface{}{
		"title":     "Old",
		"metric":    "cpu",
		"chartType": "bar",
	})
	handler.ServeHTTP(rec2, req2)
	var panel PanelConfig
	json.Unmarshal(rec2.Body.Bytes(), &panel)

	// Update panel.
	updateBody := PanelConfig{
		ID:        panel.ID,
		Title:     "Updated",
		Metric:    "memory",
		Labels:    map[string]string{"region": "us"},
		ChartType: "stat",
	}
	rec3, req3 := doJSON(t, http.MethodPut, "/api/v1/dashboards/"+dash.ID+"/panels/"+panel.ID, updateBody)
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("update panel: expected 200, got %d: %s", rec3.Code, rec3.Body.String())
	}

	var updated PanelConfig
	json.Unmarshal(rec3.Body.Bytes(), &updated)
	if updated.Title != "Updated" {
		t.Errorf("expected title 'Updated', got %q", updated.Title)
	}
}

func TestDashboardHandler_DeletePanel(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Setup.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Test"})
	handler.ServeHTTP(rec, req)
	var dash struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &dash)

	rec2, req2 := doJSON(t, http.MethodPost, "/api/v1/dashboards/"+dash.ID+"/panels", map[string]interface{}{
		"title":     "ToDelete",
		"metric":    "cpu",
		"chartType": "stat",
	})
	handler.ServeHTTP(rec2, req2)
	var panel PanelConfig
	json.Unmarshal(rec2.Body.Bytes(), &panel)

	// Delete panel.
	req3 := httptest.NewRequest(http.MethodDelete, "/api/v1/dashboards/"+dash.ID+"/panels/"+panel.ID, nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("delete panel: expected 200, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// Verify list is empty.
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboards", nil)
	handler.ServeHTTP(rec4, req4)
	var listResp struct {
		Dashboards []struct {
			Panels []PanelConfig `json:"panels"`
		} `json:"dashboards"`
	}
	json.Unmarshal(rec4.Body.Bytes(), &listResp)
	if len(listResp.Dashboards[0].Panels) != 0 {
		t.Errorf("expected 0 panels after delete, got %d", len(listResp.Dashboards[0].Panels))
	}
}

func TestDashboardHandler_RenameDashboard(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Old Name"})
	handler.ServeHTTP(rec, req)
	var dash struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &dash)

	// Rename.
	rec2, req2 := doJSON(t, http.MethodPut, "/api/v1/dashboards/"+dash.ID, map[string]string{"name": "New Name"})
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// List to verify.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboards", nil)
	handler.ServeHTTP(rec3, req3)
	var listResp struct {
		Dashboards []struct{ Name string } `json:"dashboards"`
	}
	json.Unmarshal(rec3.Body.Bytes(), &listResp)
	if listResp.Dashboards[0].Name != "New Name" {
		t.Errorf("expected 'New Name', got %q", listResp.Dashboards[0].Name)
	}
}

func TestDashboardHandler_DeleteDashboardCascades(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create dashboard with a panel.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "ToDelete"})
	handler.ServeHTTP(rec, req)
	var dash struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &dash)

	rec2, req2 := doJSON(t, http.MethodPost, "/api/v1/dashboards/"+dash.ID+"/panels", map[string]interface{}{
		"title":     "P1",
		"metric":    "cpu",
		"chartType": "stat",
	})
	handler.ServeHTTP(rec2, req2)

	// Delete dashboard.
	req3 := httptest.NewRequest(http.MethodDelete, "/api/v1/dashboards/"+dash.ID, nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("delete dashboard: expected 200, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// List should be empty.
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboards", nil)
	handler.ServeHTTP(rec4, req4)
	var listResp struct {
		Dashboards []interface{} `json:"dashboards"`
	}
	json.Unmarshal(rec4.Body.Bytes(), &listResp)
	if len(listResp.Dashboards) != 0 {
		t.Errorf("expected 0 dashboards after delete, got %d", len(listResp.Dashboards))
	}
}

func TestDashboardHandler_NotFound(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Rename non-existent dashboard.
	rec, req := doJSON(t, http.MethodPut, "/api/v1/dashboards/nonexistent", map[string]string{"name": "x"})
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent dashboard rename, got %d", rec.Code)
	}

	// Delete non-existent dashboard.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/dashboards/nonexistent", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent dashboard delete, got %d", rec2.Code)
	}

	// Update non-existent panel.
	rec3, req3 := doJSON(t, http.MethodPut, "/api/v1/dashboards/d1/panels/nonexistent", PanelConfig{
		ID:        "nonexistent",
		Title:     "x",
		Metric:    "x",
		ChartType: "stat",
	})
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent panel update, got %d", rec3.Code)
	}

	// Delete non-existent panel.
	req4 := httptest.NewRequest(http.MethodDelete, "/api/v1/dashboards/d1/panels/nonexistent", nil)
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent panel delete, got %d", rec4.Code)
	}
}

func TestDashboardHandler_EmptyDashboardName(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create with empty name.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": ""})
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", rec.Code)
	}

	// Rename with empty name (need a dashboard first).
	rec2, req2 := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Valid"})
	handler.ServeHTTP(rec2, req2)
	var dash struct{ ID string }
	json.Unmarshal(rec2.Body.Bytes(), &dash)

	rec3, req3 := doJSON(t, http.MethodPut, "/api/v1/dashboards/"+dash.ID, map[string]string{"name": ""})
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty rename, got %d", rec3.Code)
	}
}

func TestDashboardHandler_RatioPanelValidation(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create dashboard.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Test"})
	handler.ServeHTTP(rec, req)
	var dash struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &dash)

	tests := []struct {
		name string
		body map[string]interface{}
		code int
	}{
		{
			"valid ratio panel",
			map[string]interface{}{
				"title": "Success Rate", "expressionType": "ratio",
				"metric": "total_requests", "numeratorMetric": "success_requests",
				"func": "rate", "aggregation": "none", "chartType": "line", "step": 60,
			},
			http.StatusOK,
		},
		{
			"ratio missing numeratorMetric",
			map[string]interface{}{
				"title": "Bad Ratio", "expressionType": "ratio",
				"metric": "total_requests", "chartType": "line",
			},
			http.StatusBadRequest,
		},
		{
			"single panel defaults",
			map[string]interface{}{
				"title": "CPU", "metric": "cpu_usage", "chartType": "line",
			},
			http.StatusOK,
		},
		{
			"invalid expressionType",
			map[string]interface{}{
				"title": "Bad", "expressionType": "unknown",
				"metric": "cpu", "chartType": "line",
			},
			http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards/"+dash.ID+"/panels", tt.body)
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, rec.Code, rec.Body.String())
			}
		})
	}
}
