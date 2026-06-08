# Dashboard Grouping — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add named dashboards with tab navigation, each containing its own set of metric panels.

**Architecture:** Backend refactors `DashboardHandler` to manage a dashboard index (`index.json`) with subdirectories per dashboard for panel files. Frontend adds a tab bar above the toolbar with create/rename/delete dashboard actions.

**Tech Stack:** Go 1.19+ (net/http), Vue 3 + TypeScript, Chart.js, JSON file persistence

---

### Task 1: Refactor backend — dashboard data model and API

**Files:**
- Modify: `internal/api/dashboard_handler.go`

- [ ] **Step 1: Add Dashboard type and index management**

Replace the entire file content:

```go
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// PanelConfig is the stored configuration for a single dashboard panel.
type PanelConfig struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Metric    string            `json:"metric"`
	Labels    map[string]string `json:"labels"`
	ChartType string            `json:"chartType"`
	Step      int               `json:"step,omitempty"`
}

// Dashboard represents a named container of panels.
type Dashboard struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	CreatedAt string        `json:"created_at"`
	Panels    []PanelConfig `json:"panels"`
}

// DashboardHandler persists dashboards and their panels as JSON files.
type DashboardHandler struct {
	dir string
}

// NewDashboardHandler creates a DashboardHandler.
// If dir is empty, "./data/dashboards" is used as the default.
func NewDashboardHandler(dir string) *DashboardHandler {
	if dir == "" {
		dir = filepath.Join("data", "dashboards")
	}
	return &DashboardHandler{dir: dir}
}

// indexPath returns the path to the dashboard index file.
func (h *DashboardHandler) indexPath() string {
	return filepath.Join(h.dir, "index.json")
}

// loadIndex reads the dashboard index from disk.
// Returns an empty list if the file does not exist.
func (h *DashboardHandler) loadIndex() ([]Dashboard, error) {
	data, err := os.ReadFile(h.indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []Dashboard{}, nil
		}
		return nil, fmt.Errorf("read index: %w", err)
	}
	var dashboards []Dashboard
	if err := json.Unmarshal(data, &dashboards); err != nil {
		return nil, fmt.Errorf("unmarshal index: %w", err)
	}
	return dashboards, nil
}

// saveIndex writes the dashboard index to disk.
func (h *DashboardHandler) saveIndex(dashboards []Dashboard) error {
	if err := os.MkdirAll(h.dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(dashboards, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(h.indexPath(), data, 0644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	return nil
}

// panelsDir returns the directory for a dashboard's panel files.
func (h *DashboardHandler) panelsDir(dashboardID string) string {
	return filepath.Join(h.dir, dashboardID, "panels")
}

// panelPath returns the full path for a panel JSON file.
func (h *DashboardHandler) panelPath(dashboardID, panelID string) string {
	return filepath.Join(h.panelsDir(dashboardID), panelID+".json")
}

// loadPanels reads all panel files for a given dashboard.
func (h *DashboardHandler) loadPanels(dashboardID string) ([]PanelConfig, error) {
	pDir := h.panelsDir(dashboardID)
	entries, err := os.ReadDir(pDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []PanelConfig{}, nil
		}
		return nil, err
	}

	panels := make([]PanelConfig, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pDir, entry.Name()))
		if err != nil {
			log.Printf("dashboards: read panel error: %v", err)
			continue
		}
		var pc PanelConfig
		if err := json.Unmarshal(data, &pc); err != nil {
			log.Printf("dashboards: unmarshal panel error for %s: %v", entry.Name(), err)
			continue
		}
		panels = append(panels, pc)
	}
	return panels, nil
}

// savePanel writes a single panel JSON file.
func (h *DashboardHandler) savePanel(dashboardID string, pc *PanelConfig) error {
	if err := os.MkdirAll(h.panelsDir(dashboardID), 0755); err != nil {
		return fmt.Errorf("mkdir panels: %w", err)
	}
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal panel: %w", err)
	}
	if err := os.WriteFile(h.panelPath(dashboardID, pc.ID), data, 0644); err != nil {
		return fmt.Errorf("write panel: %w", err)
	}
	return nil
}

// deleteDashboardDir removes a dashboard's entire directory tree.
func (h *DashboardHandler) deleteDashboardDir(dashboardID string) error {
	return os.RemoveAll(filepath.Join(h.dir, dashboardID))
}

// ServeHTTP routes dashboard requests.
func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboards")
	if path == "" || path == "/" {
		switch r.Method {
		case http.MethodGet:
			h.listDashboards(w, r)
		case http.MethodPost:
			h.createDashboard(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Path: /{dashId} or /{dashId}/panels or /{dashId}/panels/{panelId}
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 3)
	dashID := parts[0]

	if len(parts) == 1 {
		// /{dashId}
		switch r.Method {
		case http.MethodPut:
			h.updateDashboard(w, r, dashID)
		case http.MethodDelete:
			h.deleteDashboard(w, r, dashID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) >= 2 && parts[1] == "panels" {
		if len(parts) == 2 {
			// /{dashId}/panels
			switch r.Method {
			case http.MethodPost:
				h.createPanel(w, r, dashID)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		if len(parts) == 3 {
			// /{dashId}/panels/{panelId}
			panelID := parts[2]
			switch r.Method {
			case http.MethodPut:
				h.updatePanel(w, r, dashID, panelID)
			case http.MethodDelete:
				h.deletePanel(w, r, dashID, panelID)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
	}

	http.NotFound(w, r)
}
```

- [ ] **Step 2: Add dashboard list handler**

Append after the `ServeHTTP` method:

```go
func (h *DashboardHandler) listDashboards(w http.ResponseWriter, r *http.Request) {
	dashboards, err := h.loadIndex()
	if err != nil {
		log.Printf("dashboards: load index error: %v", err)
		http.Error(w, "failed to list dashboards", http.StatusInternalServerError)
		return
	}

	// Load panels for each dashboard.
	type dashboardWithPanels struct {
		ID        string        `json:"id"`
		Name      string        `json:"name"`
		CreatedAt string        `json:"created_at"`
		Panels    []PanelConfig `json:"panels"`
	}

	result := make([]dashboardWithPanels, 0, len(dashboards))
	for _, d := range dashboards {
		panels, err := h.loadPanels(d.ID)
		if err != nil {
			log.Printf("dashboards: load panels for %s error: %v", d.ID, err)
			panels = []PanelConfig{}
		}
		// Sort panels by ID for stable ordering.
		sort.Slice(panels, func(i, j int) bool { return panels[i].ID < panels[j].ID })
		result = append(result, dashboardWithPanels{
			ID:        d.ID,
			Name:      d.Name,
			CreatedAt: d.CreatedAt,
			Panels:    panels,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"dashboards": result})
}
```

- [ ] **Step 3: Add dashboard create handler**

Append:

```go
func (h *DashboardHandler) createDashboard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	dashboards, err := h.loadIndex()
	if err != nil {
		log.Printf("dashboards: load index error: %v", err)
		http.Error(w, "failed to create dashboard", http.StatusInternalServerError)
		return
	}

	d := Dashboard{
		ID:        uuid.NewString(),
		Name:      req.Name,
		CreatedAt: r.Header.Get("X-Now"), // optional, for test determinism
	}
	if d.CreatedAt == "" {
		d.CreatedAt = "" // will be omitted in JSON when empty
	}

	dashboards = append(dashboards, d)
	if err := h.saveIndex(dashboards); err != nil {
		log.Printf("dashboards: save index error: %v", err)
		http.Error(w, "failed to save dashboard", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         d.ID,
		"name":       d.Name,
		"created_at": d.CreatedAt,
		"panels":     []PanelConfig{},
	})
}
```

- [ ] **Step 4: Add dashboard update (rename) handler**

Append:

```go
func (h *DashboardHandler) updateDashboard(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	dashboards, err := h.loadIndex()
	if err != nil {
		log.Printf("dashboards: load index error: %v", err)
		http.Error(w, "failed to update dashboard", http.StatusInternalServerError)
		return
	}

	found := false
	for i, d := range dashboards {
		if d.ID == id {
			dashboards[i].Name = req.Name
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "dashboard not found", http.StatusNotFound)
		return
	}

	if err := h.saveIndex(dashboards); err != nil {
		log.Printf("dashboards: save index error: %v", err)
		http.Error(w, "failed to save dashboard", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 5: Add dashboard delete handler**

Append:

```go
func (h *DashboardHandler) deleteDashboard(w http.ResponseWriter, r *http.Request, id string) {
	dashboards, err := h.loadIndex()
	if err != nil {
		log.Printf("dashboards: load index error: %v", err)
		http.Error(w, "failed to delete dashboard", http.StatusInternalServerError)
		return
	}

	found := false
	filtered := make([]Dashboard, 0, len(dashboards))
	for _, d := range dashboards {
		if d.ID == id {
			found = true
		} else {
			filtered = append(filtered, d)
		}
	}
	if !found {
		http.Error(w, "dashboard not found", http.StatusNotFound)
		return
	}

	// Delete the dashboard's directory (including all panels).
	if err := h.deleteDashboardDir(id); err != nil {
		log.Printf("dashboards: delete dir error: %v", err)
	}

	if err := h.saveIndex(filtered); err != nil {
		log.Printf("dashboards: save index error: %v", err)
		http.Error(w, "failed to save after delete", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 6: Add panel create/update/delete handlers**

Append:

```go
func (h *DashboardHandler) createPanel(w http.ResponseWriter, r *http.Request, dashboardID string) {
	var pc PanelConfig
	if err := json.NewDecoder(r.Body).Decode(&pc); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if pc.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if pc.Metric == "" {
		http.Error(w, "metric is required", http.StatusBadRequest)
		return
	}
	if pc.ChartType != "line" && pc.ChartType != "bar" && pc.ChartType != "stat" {
		http.Error(w, "chartType must be line, bar, or stat", http.StatusBadRequest)
		return
	}

	pc.ID = uuid.NewString()

	if err := h.savePanel(dashboardID, &pc); err != nil {
		log.Printf("dashboards: save panel error: %v", err)
		http.Error(w, "failed to save panel", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, pc)
}

func (h *DashboardHandler) updatePanel(w http.ResponseWriter, r *http.Request, dashboardID, panelID string) {
	var pc PanelConfig
	if err := json.NewDecoder(r.Body).Decode(&pc); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Ensure the panel exists.
	if _, err := os.Stat(h.panelPath(dashboardID, panelID)); os.IsNotExist(err) {
		http.Error(w, "panel not found", http.StatusNotFound)
		return
	}

	pc.ID = panelID
	if err := h.savePanel(dashboardID, &pc); err != nil {
		log.Printf("dashboards: save panel error: %v", err)
		http.Error(w, "failed to save panel", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, pc)
}

func (h *DashboardHandler) deletePanel(w http.ResponseWriter, r *http.Request, dashboardID, panelID string) {
	p := h.panelPath(dashboardID, panelID)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		http.Error(w, "panel not found", http.StatusNotFound)
		return
	}
	if err := os.Remove(p); err != nil {
		log.Printf("dashboards: delete panel error: %v", err)
		http.Error(w, "failed to delete panel", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 7: Run Go build and tests**

```bash
go build ./...
```

Expected: Exit code 0.

```bash
go test ./internal/api/ -v -run Dashboard
```

Expected: All existing dashboard tests pass (they use the old ServeHTTP routing which we replaced — they may fail. Note failures and proceed to Task 2).

- [ ] **Step 8: Commit**

```bash
git add internal/api/dashboard_handler.go
git commit -m "refactor: dashboard handler supports named dashboards with panel subdirectories"
```

---

### Task 2: Rewrite backend tests for dashboard grouping

**Files:**
- Modify: `internal/api/dashboard_handler_test.go`

- [ ] **Step 1: Replace test file with new tests**

Replace the entire file content:

```go
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

func doJSON(t *testing.T, method, url string, body interface{}) *httptest.ResponseRecorder {
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

	doJSON(t, http.MethodPost, "/api/v1/dashboards/"+dash.ID+"/panels", map[string]interface{}{
		"title":     "P1",
		"metric":    "cpu",
		"chartType": "stat",
	})

	// Delete dashboard.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/dashboards/"+dash.ID, nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("delete dashboard: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// List should be empty.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboards", nil)
	handler.ServeHTTP(rec3, req3)
	var listResp struct {
		Dashboards []interface{} `json:"dashboards"`
	}
	json.Unmarshal(rec3.Body.Bytes(), &listResp)
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
```

- [ ] **Step 2: Run the tests**

```bash
go test ./internal/api/ -v -run Dashboard
```

Expected: All 8 tests pass (CreateAndListDashboards, CreatePanelInDashboard, UpdatePanel, DeletePanel, RenameDashboard, DeleteDashboardCascades, NotFound, EmptyDashboardName).

- [ ] **Step 3: Commit**

```bash
git add internal/api/dashboard_handler_test.go
git commit -m "test: rewrite dashboard tests for grouping model"
```

---

### Task 3: Verify router compatibility (no changes needed)

**Files:**
- Verify: `internal/api/router.go`

- [ ] **Step 1: Confirm existing routes work for new paths**

Current routes in `router.go`:
```go
mux.HandleFunc("/api/v1/dashboards/", dashboardHandler.ServeHTTP)
mux.HandleFunc("/api/v1/dashboards", dashboardHandler.ServeHTTP)
```

These already delegate all dashboard-related paths to `ServeHTTP`, which now handles the new nested routes (`/api/v1/dashboards/{id}/panels/{panelId}`) internally. No router changes required.

- [ ] **Step 2: Verify with a quick smoke check**

```bash
go build ./...
```

Expected: Exit code 0. Router is compatible without modification.

- [ ] **Step 3: No commit needed (no changes)**

---

### Task 4: Update frontend API client

**Files:**
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add Dashboard type and replace dashboard API functions**

In `web/src/api/client.ts`, replace the Dashboard section (lines 132-181) with:

```typescript
// --- Dashboard types and API ---

export interface PanelConfig {
  id: string
  title: string
  metric: string
  labels: Record<string, string>
  chartType: 'line' | 'bar' | 'stat'
  step?: number
}

export interface DashboardItem {
  id: string
  name: string
  created_at: string
  panels: PanelConfig[]
}

export interface DashboardListResponse {
  dashboards: DashboardItem[]
}

export async function listDashboards(): Promise<DashboardListResponse> {
  return get<DashboardListResponse>(`${BASE_URL}/dashboards`)
}

export async function createDashboard(name: string): Promise<DashboardItem> {
  const res = await fetch(`${BASE_URL}/dashboards`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function renameDashboard(id: string, name: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/dashboards/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

export async function deleteDashboard(id: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/dashboards/${id}`, { method: 'DELETE' })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

export async function createPanel(dashboardId: string, panel: Omit<PanelConfig, 'id'>): Promise<PanelConfig> {
  const res = await fetch(`${BASE_URL}/dashboards/${dashboardId}/panels`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(panel),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function updatePanel(dashboardId: string, panel: PanelConfig): Promise<PanelConfig> {
  const res = await fetch(`${BASE_URL}/dashboards/${dashboardId}/panels/${panel.id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(panel),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function deletePanel(dashboardId: string, panelId: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/dashboards/${dashboardId}/panels/${panelId}`, { method: 'DELETE' })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}
```

Remove the old `listDashboards`, `createDashboard`, `updateDashboard`, `deleteDashboard` functions (they now have different signatures and behaviors).

Keep `getMetricNames()`, `getLabels()`, `getLabelValues()` unchanged.

- [ ] **Step 2: Run TypeScript check**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: Errors in Dashboard.vue and PanelForm.vue (they still reference old function signatures). These will be fixed in Tasks 6 and 7.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "refactor: update API client for dashboard grouping"
```

---

### Task 5: Add i18n strings

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

- [ ] **Step 1: Add `cancel` to common block in English**

In `web/src/i18n/locales/en.ts`, add `cancel` to the `common` block (after `allServices`):

```typescript
    cancel: 'Cancel',
```

Then add a `dashboard` block (in the default export object, alongside `traceList`, `common`, etc.):

```typescript
  dashboard: {
    newDashboard: 'New Dashboard',
    dashboardName: 'Dashboard name',
    createDashboard: 'Create Dashboard',
    renameDashboard: 'Rename Dashboard',
    deleteDashboard: 'Delete Dashboard',
    deleteDashboardConfirm: 'Delete dashboard "{name}" and all its panels?',
    noDashboards: 'No dashboards yet.',
    createFirstDashboard: 'Create your first dashboard',
  },
```

- [ ] **Step 2: Add `cancel` to common block and dashboard strings in Chinese**

In `web/src/i18n/locales/zh.ts`, add `cancel` to the `common` block (after `allServices`):

```typescript
    cancel: '取消',
```

Then add a `dashboard` block:

```typescript
  dashboard: {
    newDashboard: '新建仪表盘',
    dashboardName: '仪表盘名称',
    createDashboard: '创建仪表盘',
    renameDashboard: '重命名仪表盘',
    deleteDashboard: '删除仪表盘',
    deleteDashboardConfirm: '确定删除仪表盘 "{name}" 及其所有面板？',
    noDashboards: '暂无仪表盘。',
    createFirstDashboard: '创建你的第一个仪表盘',
  },
```

- [ ] **Step 3: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add dashboard grouping i18n strings"
```

---

### Task 6: Update Dashboard.vue — tab bar and dashboard CRUD

**Files:**
- Modify: `web/src/views/Dashboard.vue`

- [ ] **Step 1: Replace the template**

Replace the `<template>` block entirely:

```html
<template>
  <div class="dashboard-page">
    <!-- Tab bar -->
    <div class="tab-bar">
      <div class="tab-list">
        <button
          v-for="dash in dashboards"
          :key="dash.id"
          :class="['tab-item', { active: dash.id === activeDashboardId }]"
          @click="switchDashboard(dash.id)"
        >{{ dash.name }}</button>
        <button class="tab-item tab-add" @click="showCreateDashboard = true" :title="t('dashboard.newDashboard')">+</button>
      </div>
    </div>

    <!-- Create dashboard popover -->
    <div v-if="showCreateDashboard" class="popover-overlay" @click.self="closeCreateDashboard">
      <div class="popover-box">
        <input
          ref="createInputRef"
          v-model="newDashboardName"
          type="text"
          :placeholder="t('dashboard.dashboardName')"
          class="popover-input"
          @keyup.enter="doCreateDashboard"
          @keyup.escape="closeCreateDashboard"
        />
        <div class="popover-actions">
          <button class="btn btn-sm" @click="closeCreateDashboard">{{ t('common.cancel') }}</button>
          <button class="btn btn-sm btn-primary" @click="doCreateDashboard" :disabled="!newDashboardName.trim()">{{ t('dashboard.createDashboard') }}</button>
        </div>
      </div>
    </div>

    <!-- Rename dashboard popover -->
    <div v-if="showRenameDashboard" class="popover-overlay" @click.self="closeRenameDashboard">
      <div class="popover-box">
        <input
          ref="renameInputRef"
          v-model="renameName"
          type="text"
          :placeholder="t('dashboard.dashboardName')"
          class="popover-input"
          @keyup.enter="doRenameDashboard"
          @keyup.escape="closeRenameDashboard"
        />
        <div class="popover-actions">
          <button class="btn btn-sm" @click="closeRenameDashboard">{{ t('common.cancel') }}</button>
          <button class="btn btn-sm btn-primary" @click="doRenameDashboard" :disabled="!renameName.trim()">{{ t('dashboard.renameDashboard') }}</button>
        </div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="dashboard-toolbar" v-if="activeDashboardId">
      <div class="time-presets">
        <button
          v-for="p in presets"
          :key="p.label"
          :class="['btn-preset', { active: activePreset === p.label }]"
          @click="setPreset(p.label, p.duration)"
        >{{ p.label }}</button>
        <div v-if="activePreset === 'custom'" class="custom-range">
          <input type="datetime-local" v-model="customStart" />
          <span>to</span>
          <input type="datetime-local" v-model="customEnd" />
        </div>
        <button class="btn-preset btn-refresh" @click="refreshAll">&#8635;</button>
      </div>
      <div class="toolbar-actions">
        <button class="btn" @click="openRenameDashboard">{{ t('dashboard.renameDashboard') }}</button>
        <button class="btn" @click="confirmDeleteDashboard">{{ t('dashboard.deleteDashboard') }}</button>
        <button class="btn btn-primary" @click="openCreatePanel">+ New Panel</button>
      </div>
    </div>

    <!-- Content -->
    <div v-if="loading" class="page-state">Loading...</div>
    <div v-else-if="loadError" class="page-state page-error">{{ loadError }}</div>
    <div v-else-if="dashboards.length === 0" class="page-state">
      <p>{{ t('dashboard.noDashboards') }}</p>
      <button class="btn btn-primary" @click="showCreateDashboard = true">{{ t('dashboard.createFirstDashboard') }}</button>
    </div>
    <div v-else-if="panels.length === 0" class="page-state">
      <p>No panels yet.</p>
      <p>Click "+ New Panel" to create your first dashboard panel.</p>
    </div>
    <div v-else class="panel-grid">
      <PanelChart
        v-for="panel in panels"
        :key="panel.id"
        :panel="panel"
        :timeRange="computedTimeRange"
        :refreshKey="refreshKey"
        @edit="openEditPanel"
        @delete="confirmDeletePanel"
      />
    </div>

    <!-- Panel form modal -->
    <PanelForm
      v-if="showPanelForm"
      :panel="editingPanel"
      :dashboardId="activeDashboardId"
      @saved="onPanelSaved"
      @cancel="closePanelForm"
    />
  </div>
</template>
```

- [ ] **Step 2: Replace the script block**

Replace the `<script setup>` block:

```typescript
import { ref, computed, onMounted, nextTick } from 'vue'
import { listDashboards, createDashboard, renameDashboard, deleteDashboard, deletePanel, type DashboardItem, type PanelConfig } from '../api/client'
import { useI18n } from 'vue-i18n'
import PanelChart from '../components/PanelChart.vue'
import PanelForm from '../components/PanelForm.vue'

const { t } = useI18n()

const dashboards = ref<DashboardItem[]>([])
const activeDashboardId = ref<string>('')
const loading = ref(true)
const loadError = ref('')

const panels = computed(() => {
  const dash = dashboards.value.find(d => d.id === activeDashboardId.value)
  return dash?.panels || []
})

// Dashboard CRUD state
const showCreateDashboard = ref(false)
const newDashboardName = ref('')
const createInputRef = ref<HTMLInputElement | null>(null)

const showRenameDashboard = ref(false)
const renameName = ref('')
const renameInputRef = ref<HTMLInputElement | null>(null)

// Panel form state
const showPanelForm = ref(false)
const editingPanel = ref<PanelConfig | null>(null)

// Time presets
const presets = [
  { label: '1h', duration: 3600 * 1000 },
  { label: '6h', duration: 6 * 3600 * 1000 },
  { label: '24h', duration: 24 * 3600 * 1000 },
  { label: 'custom', duration: 0 },
]

const activePreset = ref('1h')
const customStart = ref('')
const customEnd = ref('')

const computedTimeRange = computed(() => {
  if (activePreset.value === 'custom') {
    const start = customStart.value ? new Date(customStart.value).getTime() : Date.now() - 3600000
    const end = customEnd.value ? new Date(customEnd.value).getTime() : Date.now()
    return { start, end }
  }
  const end = Date.now()
  const preset = presets.find(p => p.label === activePreset.value)
  const start = end - (preset?.duration || 3600000)
  return { start, end }
})

const refreshKey = ref(0)

function refreshAll() { refreshKey.value++ }

function setPreset(label: string, duration: number) {
  activePreset.value = label
  if (label === 'custom') {
    const end = new Date()
    const start = new Date(end.getTime() - 3600000)
    customEnd.value = end.toISOString().slice(0, 16)
    customStart.value = start.toISOString().slice(0, 16)
  }
}

async function loadAll() {
  loading.value = true
  loadError.value = ''
  try {
    const data = await listDashboards()
    dashboards.value = data.dashboards || []
    // Auto-select first dashboard or auto-create a default.
    if (dashboards.value.length === 0) {
      activeDashboardId.value = ''
    } else if (!dashboards.value.find(d => d.id === activeDashboardId.value)) {
      activeDashboardId.value = dashboards.value[0].id
    }
  } catch (e: any) {
    loadError.value = e.message || 'Failed to load dashboards'
  } finally {
    loading.value = false
  }
}

function switchDashboard(id: string) {
  activeDashboardId.value = id
}

// Create dashboard
async function doCreateDashboard() {
  const name = newDashboardName.value.trim()
  if (!name) return
  try {
    const dash = await createDashboard(name)
    dashboards.value.push(dash)
    activeDashboardId.value = dash.id
    closeCreateDashboard()
  } catch (e: any) {
    alert(`Create dashboard failed: ${e.message}`)
  }
}

function closeCreateDashboard() {
  showCreateDashboard.value = false
  newDashboardName.value = ''
}

// Rename dashboard
function openRenameDashboard() {
  const dash = dashboards.value.find(d => d.id === activeDashboardId.value)
  if (!dash) return
  renameName.value = dash.name
  showRenameDashboard.value = true
  nextTick(() => renameInputRef.value?.focus())
}

async function doRenameDashboard() {
  const name = renameName.value.trim()
  if (!name || !activeDashboardId.value) return
  try {
    await renameDashboard(activeDashboardId.value, name)
    const dash = dashboards.value.find(d => d.id === activeDashboardId.value)
    if (dash) dash.name = name
    closeRenameDashboard()
  } catch (e: any) {
    alert(`Rename failed: ${e.message}`)
  }
}

function closeRenameDashboard() {
  showRenameDashboard.value = false
  renameName.value = ''
}

// Delete dashboard
async function confirmDeleteDashboard() {
  const dash = dashboards.value.find(d => d.id === activeDashboardId.value)
  if (!dash) return
  if (!confirm(t('dashboard.deleteDashboardConfirm', { name: dash.name }))) return
  try {
    await deleteDashboard(dash.id)
    dashboards.value = dashboards.value.filter(d => d.id !== dash.id)
    // Switch to first remaining dashboard, or empty state.
    if (dashboards.value.length > 0) {
      activeDashboardId.value = dashboards.value[0].id
    } else {
      activeDashboardId.value = ''
    }
  } catch (e: any) {
    alert(`Delete failed: ${e.message}`)
  }
}

// Panel operations
function openCreatePanel() {
  editingPanel.value = null
  showPanelForm.value = true
}

function openEditPanel(panel: PanelConfig) {
  editingPanel.value = panel
  showPanelForm.value = true
}

function closePanelForm() {
  showPanelForm.value = false
  editingPanel.value = null
}

function onPanelSaved() {
  closePanelForm()
  loadAll() // Reload to get updated panels list.
}

async function confirmDeletePanel(panel: PanelConfig) {
  if (!confirm(`Delete panel "${panel.title}"?`)) return
  try {
    await deletePanel(activeDashboardId.value, panel.id)
    loadAll()
  } catch (e: any) {
    alert(`Delete failed: ${e.message}`)
  }
}

onMounted(loadAll)
```

- [ ] **Step 3: Replace the style block**

Replace the `<style scoped>` block:

```css
.dashboard-page { max-width: 1400px; margin: 0 auto; }

/* Tab bar */
.tab-bar {
  margin-bottom: 16px;
  border-bottom: 1px solid var(--border-default);
}
.tab-list {
  display: flex;
  align-items: center;
  gap: 2px;
  overflow-x: auto;
}
.tab-item {
  padding: 8px 16px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-secondary);
  font-size: 14px;
  cursor: pointer;
  white-space: nowrap;
  transition: color 0.15s, border-color 0.15s;
}
.tab-item:hover:not(.tab-add) {
  color: var(--text-primary);
}
.tab-item.active {
  color: var(--accent-blue);
  border-bottom-color: var(--accent-blue);
}
.tab-add {
  font-size: 18px;
  padding: 8px 12px;
  color: var(--text-muted);
}
.tab-add:hover {
  color: var(--accent-blue);
}

/* Popover */
.popover-overlay {
  position: fixed; inset: 0; z-index: 1000;
  display: flex; align-items: flex-start; justify-content: center;
  padding-top: 80px;
}
.popover-box {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 16px;
  min-width: 300px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.3);
}
.popover-input {
  width: 100%;
  padding: 8px 12px;
  background: var(--bg-surface-deep);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 14px;
  box-sizing: border-box;
}
.popover-input:focus { border-color: var(--accent-blue); outline: none; }
.popover-actions {
  display: flex; gap: 8px; justify-content: flex-end;
  margin-top: 12px;
}

/* Toolbar */
.dashboard-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 24px; flex-wrap: wrap; gap: 12px;
}
.time-presets { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.btn-preset {
  padding: 6px 16px; border: 1px solid var(--border-group); background: var(--bg-secondary);
  color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 13px;
}
.btn-preset.active { background: var(--accent-blue); color: var(--bg-primary); border-color: var(--accent-blue); }
.btn-preset:hover:not(.active) { border-color: var(--accent-blue); color: var(--accent-blue); }
.custom-range { display: flex; align-items: center; gap: 8px; }
.custom-range input {
  background: var(--bg-primary); border: 1px solid var(--border-group); border-radius: 6px;
  color: var(--text-primary); padding: 6px 10px; font-size: 13px;
}
.custom-range span { color: var(--text-secondary); font-size: 13px; }
.toolbar-actions {
  display: flex; gap: 8px; align-items: center;
}
.btn {
  padding: 8px 16px; border: 1px solid var(--border-strong); border-radius: 6px;
  background: var(--bg-surface-hover); color: var(--text-primary); cursor: pointer; font-size: 14px;
}
.btn:hover { background: var(--border-strong); }
.btn-sm { padding: 4px 12px; font-size: 13px; }
.btn-primary { background: var(--accent-blue); border-color: var(--accent-blue); color: var(--bg-primary); }
.btn-primary:hover { background: var(--accent-light); }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-refresh {
  background: var(--bg-secondary); border: 1px solid var(--border-group); color: var(--text-primary);
}
.btn-refresh:hover { background: var(--bg-surface-hover-subtle); border-color: var(--accent-blue); }

/* Panel grid */
.panel-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 20px;
}
@media (max-width: 960px) {
  .panel-grid { grid-template-columns: 1fr; }
}

/* Page states */
.page-state {
  text-align: center; padding: 80px 20px; color: var(--text-secondary); font-size: 15px;
}
.page-error { color: var(--status-error-accent); }
```

- [ ] **Step 4: Run TypeScript check**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: Errors only in PanelForm.vue (will be fixed in Task 7).

- [ ] **Step 5: Commit**

```bash
git add web/src/views/Dashboard.vue
git commit -m "feat: add tab bar and dashboard CRUD to Dashboard page"
```

---

### Task 7: Update PanelForm.vue to use new API

**Files:**
- Modify: `web/src/components/PanelForm.vue`

- [ ] **Step 1: Add dashboardId prop and update API calls**

In `web/src/components/PanelForm.vue`:

Add `dashboardId` prop (no default — required from parent):

```typescript
const props = defineProps<{
  panel?: PanelConfig | null
  dashboardId?: string
}>()
```

Update the `import` line to include the new functions:

```typescript
import { getMetricNames, getLabels, getLabelValues, createPanel, updatePanel } from '../api/client'
```

Update `handleSubmit` to use `createPanel`/`updatePanel` with `dashboardId`:

```typescript
async function handleSubmit() {
  saveError.value = ''
  if (!form.title || !form.metric) return
  if (!props.dashboardId) {
    saveError.value = 'No dashboard selected'
    return
  }

  const labels: Record<string, string> = {}
  for (const entry of form.labelEntries) {
    if (entry.key && entry.value) {
      labels[entry.key] = entry.value
    }
  }

  const panel: Omit<PanelConfig, 'id'> = {
    title: form.title,
    metric: form.metric,
    labels,
    chartType: form.chartType,
  }
  if (form.chartType !== 'stat') {
    panel.step = form.step
  }

  saving.value = true
  try {
    let result: PanelConfig
    if (isEdit.value && props.panel) {
      result = await updatePanel(props.dashboardId, {
        ...panel,
        id: props.panel.id,
      })
    } else {
      result = await createPanel(props.dashboardId, panel)
    }
    emit('saved', result)
  } catch (e: any) {
    saveError.value = e.message || 'Save failed'
  } finally {
    saving.value = false
  }
}
```

Also adjust the emit type — `saved` now only fires (no longer passes the panel back — or keep it as-is, Dashboard.vue ignores the arg):

Keep `emit` unchanged:
```typescript
const emit = defineEmits<{
  saved: [panel: PanelConfig]
  cancel: []
}>()
```

- [ ] **Step 2: Run TypeScript check**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: Exit code 0, no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/PanelForm.vue
git commit -m "feat: update PanelForm to use dashboard-scoped API"
```

---

### Task 8: Build and verify

**Files:**
- None (verification only)

- [ ] **Step 1: Full build**

```bash
make build
```

Expected: Frontend builds, Go binary compiles successfully.

- [ ] **Step 2: Start server and smoke test**

```bash
./bin/labubu serve --data-dir /tmp/labubu-test --metrics-enabled=false
```

From another terminal:

```bash
# Create a dashboard
curl -s -X POST http://localhost:8080/api/v1/dashboards \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Dashboard"}'

# Expected: {"id":"...", "name":"Test Dashboard", "panels":[]}

# List dashboards
curl -s http://localhost:8080/api/v1/dashboards

# Expected: {"dashboards":[{"id":"...", "name":"Test Dashboard", "created_at":"", "panels":[]}]}

# Create a panel in the dashboard (replace DASH_ID with actual id)
curl -s -X POST http://localhost:8080/api/v1/dashboards/DASH_ID/panels \
  -H "Content-Type: application/json" \
  -d '{"title":"CPU","metric":"cpu","chartType":"line","step":60}'

# Expected: panel JSON with id

# Rename dashboard
curl -s -X PUT http://localhost:8080/api/v1/dashboards/DASH_ID \
  -H "Content-Type: application/json" \
  -d '{"name":"Renamed"}'

# Expected: {"status":"ok"}

# Delete dashboard (cascades)
curl -s -X DELETE http://localhost:8080/api/v1/dashboards/DASH_ID

# Expected: {"status":"ok"}
```

- [ ] **Step 3: Verify frontend in browser**

Open `http://localhost:8080/dashboards`:
- Empty state shows "Create your first dashboard" button
- Create dashboard via "+" tab → popover input → Enter
- Tab bar shows all dashboards, click to switch
- "+ New Panel" creates panel in active dashboard
- Rename button opens popover, saves on Enter
- Delete button shows confirmation, then removes dashboard and panels
- Tab switches to remaining dashboard after delete
- Time presets work per dashboard
- Navigate between tabs — panels load independently

- [ ] **Step 4: Commit any fixes if needed**

```bash
git add -A && git commit -m "chore: final fixes from manual verification"
```
