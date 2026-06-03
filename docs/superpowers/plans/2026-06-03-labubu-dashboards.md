# Metrics Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Grafana-like metrics dashboard page — users create panel configs via UI form, each panel queries the Prometheus-compatible API and renders a chart (line/bar/stat).

**Architecture:** Backend stores panel configs as JSON files under `data/dashboards/`. Frontend Vue 3 page at `/dashboards` loads all configs, renders each as a Chart.js chart or stat card, with a shared time-range selector. One JSON file = one panel.

**Tech Stack:** Go `net/http`, Vue 3 Composition API, Chart.js 4, JSON file persistence.

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/metrics/tstorage_store.go` | Modify | Track `__name__` in label index during Insert |
| `internal/api/metrics_handler.go` | Modify | Add `MetricNames` handler |
| `internal/api/dashboard_handler.go` | Create | Panel CRUD: list/create/update/delete JSON files |
| `internal/api/dashboard_handler_test.go` | Create | Tests for dashboard CRUD |
| `internal/api/router.go` | Modify | Register dashboard and metric-names routes |
| `web/src/api/client.ts` | Modify | Add dashboard types + API functions |
| `web/src/components/PanelChart.vue` | Create | Chart rendering (line/bar/stat) with API fetch |
| `web/src/components/PanelForm.vue` | Create | Modal form for create/edit panel |
| `web/src/views/Dashboard.vue` | Create | Dashboard page: time range + grid of charts |
| `web/src/router.ts` | Modify | Add `/dashboards` route |
| `web/src/App.vue` | Modify | Add "Metrics" sidebar link |

---

### Task 1: Track `__name__` in label index

**Files:**
- Modify: `internal/metrics/tstorage_store.go`

- [ ] **Step 1: Add `__name__` tracking to Insert**

In `Insert`, below the existing label index update (after line 81), add __name__ tracking. Read the file at `internal/metrics/tstorage_store.go` and replace the Insert method body. The full new Insert method:

```go
// Insert writes metric data points to the store.
func (s *TStorageStore) Insert(ctx context.Context, points []MetricPoint) error {
	rows := make([]tstorage.Row, 0, len(points))
	for _, p := range points {
		labels := make([]tstorage.Label, 0, len(p.Labels))
		for k, v := range p.Labels {
			labels = append(labels, tstorage.Label{Name: k, Value: v})
		}
		rows = append(rows, tstorage.Row{
			Metric: p.Name,
			Labels: labels,
			DataPoint: tstorage.DataPoint{
				Value:     p.Value,
				Timestamp: p.Timestamp,
			},
		})
	}

	if err := s.storage.InsertRows(rows); err != nil {
		return fmt.Errorf("tstorage insert: %w", err)
	}

	// Update label index.
	s.mu.Lock()
	for _, p := range points {
		// Track metric name as __name__ label.
		if s.labelIdx["__name__"] == nil {
			s.labelIdx["__name__"] = make(map[string]struct{})
		}
		s.labelIdx["__name__"][p.Name] = struct{}{}

		for k, v := range p.Labels {
			if s.labelIdx[k] == nil {
				s.labelIdx[k] = make(map[string]struct{})
			}
			s.labelIdx[k][v] = struct{}{}
		}
	}
	s.mu.Unlock()

	return nil
}
```

- [ ] **Step 2: Run existing tests to verify no regression**

Run: `go test ./internal/metrics/... -v`
Expected: all tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/metrics/tstorage_store.go
git commit -m "feat: track __name__ in metrics label index for metric name discovery"
```

---

### Task 2: Add metric-names API endpoint

**Files:**
- Modify: `internal/api/metrics_handler.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add MetricNames handler to MetricsHandler**

In `internal/api/metrics_handler.go`, add the following method to `MetricsHandler` (place it after the `Metadata` method, before `IngestOTLP`):

```go
// MetricNames handles GET /api/v1/metric-names
func (h *MetricsHandler) MetricNames(w http.ResponseWriter, r *http.Request) {
	values, err := h.store.LabelValues(r.Context(), "__name__")
	if err != nil {
		log.Printf("metrics: metric-names error: %v", err)
		writeJSON(w, http.StatusInternalServerError, prometheusResponse{
			Status: "error",
			Error:  fmt.Sprintf("metric names failed: %v", err),
		})
		return
	}
	if values == nil {
		values = []string{}
	}
	writeLabelsJSON(w, http.StatusOK, values)
}
```

- [ ] **Step 2: Register route in router.go**

In `internal/api/router.go`, inside the `if metricsHandler != nil` block (after the existing routes), add:

```go
mux.HandleFunc("/api/v1/metric-names", metricsHandler.MetricNames)
```

- [ ] **Step 3: Write test**

In `internal/api/metrics_handler_test.go`, add:

```go
func TestMetricsHandler_MetricNames(t *testing.T) {
	store := &metricsMockStore{
		labelValues: []string{"cpu_usage", "memory_usage"},
	}

	handler := NewMetricsHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metric-names", nil)
	rec := httptest.NewRecorder()

	handler.MetricNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "success" {
		t.Errorf("expected status 'success', got %v", resp["status"])
	}
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data to be array, got %T", resp["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 metric names, got %d", len(data))
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/... -run TestMetric -v`
Expected: ALL PASS (including new TestMetricsHandler_MetricNames)

- [ ] **Step 5: Commit**

```bash
git add internal/api/metrics_handler.go internal/api/router.go internal/api/metrics_handler_test.go
git commit -m "feat: add GET /api/v1/metric-names endpoint"
```

---

### Task 3: Dashboard CRUD handler (backend JSON file persistence)

**Files:**
- Create: `internal/api/dashboard_handler.go`
- Create: `internal/api/dashboard_handler_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/api/dashboard_handler_test.go`:

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestDashboard -v`
Expected: compilation error — `undefined: DashboardHandler`, `undefined: PanelConfig`

- [ ] **Step 3: Implement dashboard_handler.go**

Create `internal/api/dashboard_handler.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

// DashboardHandler persists panel configs as JSON files under a directory.
type DashboardHandler struct {
	dir string
}

// NewDashboardHandler creates a DashboardHandler.
func NewDashboardHandler(dir string) *DashboardHandler {
	return &DashboardHandler{dir: dir}
}

// ServeHTTP routes dashboard requests based on method and path.
func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Path: /api/v1/dashboards[/{id}]
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboards")
	if path == "" || path == "/" {
		switch r.Method {
		case http.MethodGet:
			h.listPanels(w, r)
		case http.MethodPost:
			h.createPanel(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	id := strings.TrimPrefix(path, "/")
	switch r.Method {
	case http.MethodPut:
		h.updatePanel(w, r, id)
	case http.MethodDelete:
		h.deletePanel(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *DashboardHandler) panelPath(id string) string {
	return filepath.Join(h.dir, id+".json")
}

func (h *DashboardHandler) listPanels(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"panels": []interface{}{}})
			return
		}
		log.Printf("dashboards: read dir error: %v", err)
		http.Error(w, "failed to list dashboards", http.StatusInternalServerError)
		return
	}

	panels := make([]PanelConfig, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		p := filepath.Join(h.dir, entry.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			log.Printf("dashboards: read file error: %v", err)
			continue
		}
		var pc PanelConfig
		if err := json.Unmarshal(data, &pc); err != nil {
			log.Printf("dashboards: unmarshal error for %s: %v", entry.Name(), err)
			continue
		}
		panels = append(panels, pc)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"panels": panels})
}

func (h *DashboardHandler) createPanel(w http.ResponseWriter, r *http.Request) {
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

	if err := h.savePanel(&pc); err != nil {
		log.Printf("dashboards: save error: %v", err)
		http.Error(w, "failed to save panel", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, pc)
}

func (h *DashboardHandler) updatePanel(w http.ResponseWriter, r *http.Request, id string) {
	var pc PanelConfig
	if err := json.NewDecoder(r.Body).Decode(&pc); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Ensure the panel exists.
	if _, err := os.Stat(h.panelPath(id)); os.IsNotExist(err) {
		http.Error(w, "panel not found", http.StatusNotFound)
		return
	}

	pc.ID = id
	if err := h.savePanel(&pc); err != nil {
		log.Printf("dashboards: save error: %v", err)
		http.Error(w, "failed to save panel", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, pc)
}

func (h *DashboardHandler) deletePanel(w http.ResponseWriter, r *http.Request, id string) {
	p := h.panelPath(id)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		http.Error(w, "panel not found", http.StatusNotFound)
		return
	}
	if err := os.Remove(p); err != nil {
		log.Printf("dashboards: delete error: %v", err)
		http.Error(w, "failed to delete panel", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *DashboardHandler) savePanel(pc *PanelConfig) error {
	// Ensure directory exists.
	if err := os.MkdirAll(h.dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(h.panelPath(pc.ID), data, 0644); err != nil {
		return fmt.Errorf("writefile: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/... -run TestDashboard -v`
Expected: ALL PASS (5 tests: CreateList, UpdateDelete, Delete, NotFound, InvalidJSON)

- [ ] **Step 5: Commit**

```bash
git add internal/api/dashboard_handler.go internal/api/dashboard_handler_test.go
git commit -m "feat: add dashboard panel CRUD handler with JSON file persistence"
```

---

### Task 4: Register dashboard routes and wire up in main.go

**Files:**
- Modify: `internal/api/router.go`
- Modify: `cmd/labubu/main.go`

- [ ] **Step 1: Add DashboardHandler to NewRouter signature**

In `internal/api/router.go`, change the `NewRouter` function signature and add dashboard routes. Full updated file:

```go
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NewRouter creates the HTTP handler with API routes and static file serving.
func NewRouter(traceHandler *TraceHandler, metricsHandler *MetricsHandler, dashboardHandler *DashboardHandler) http.Handler {
	mux := http.NewServeMux()

	// API routes.
	mux.HandleFunc("/api/v1/traces/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/traces")
		if path == "" || path == "/" {
			traceHandler.ListTraces(w, r)
			return
		}
		traceIDHex := strings.TrimPrefix(path, "/")
		traceHandler.GetTrace(w, r, traceIDHex)
	})
	mux.HandleFunc("/api/v1/services", traceHandler.GetServices)

	// API routes — metrics (Prometheus API).
	if metricsHandler != nil {
		mux.HandleFunc("/api/v1/query", metricsHandler.InstantQuery)
		mux.HandleFunc("/api/v1/query_range", metricsHandler.RangeQuery)
		mux.HandleFunc("/api/v1/labels", metricsHandler.Labels)
		mux.HandleFunc("/api/v1/label/", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/api/v1/label/")
			name := strings.TrimSuffix(path, "/values")
			metricsHandler.LabelValues(w, r, name)
		})
		mux.HandleFunc("/api/v1/metadata", metricsHandler.Metadata)
		mux.HandleFunc("/api/v1/metric-names", metricsHandler.MetricNames)
		mux.HandleFunc("/api/v1/otlp/v1/metrics", metricsHandler.IngestOTLP)
	}

	// API routes — dashboards.
	if dashboardHandler != nil {
		mux.HandleFunc("/api/v1/dashboards/", dashboardHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/dashboards", dashboardHandler.ServeHTTP)
	}

	// Health check.
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Serve Vue SPA from filesystem. In production, web/dist contains the built frontend.
	// In development, the Vite dev server proxies /api requests.
	distPath := filepath.Join("web", "dist")
	if _, err := os.Stat(distPath); err == nil {
		spa := spaHandler{staticDir: distPath}
		mux.Handle("/", spa)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(devFallbackHTML))
		})
	}

	return mux
}

// ... rest of file unchanged (spaHandler, devFallbackHTML stay the same)
```

- [ ] **Step 2: Wire up DashboardHandler in main.go**

In `cmd/labubu/main.go`, add the dashboard data directory flag and wire up the handler. Add this flag in the `var` block (after existing metrics flags):

```go
dashboardsDir = flag.String("dashboards-dir", "./data/dashboards", "dashboard panel configs directory")
```

In the API initialization section (replacing the `NewRouter` call), find:

```go
// Initialize API router.
traceHandler := api.NewTraceHandler(store)
var metricsHandler *api.MetricsHandler
if metricStore != nil {
	metricsHandler = api.NewMetricsHandler(metricStore)
}
router := api.NewRouter(traceHandler, metricsHandler)
```

Replace with:

```go
// Initialize API router.
traceHandler := api.NewTraceHandler(store)
var metricsHandler *api.MetricsHandler
if metricStore != nil {
	metricsHandler = api.NewMetricsHandler(metricStore)
}
dashboardHandler := api.NewDashboardHandler(*dashboardsDir)
router := api.NewRouter(traceHandler, metricsHandler, dashboardHandler)
```

Update the import if needed — `"github.com/google/uuid"` is already needed by the dashboard handler, but `cmd/labubu/main.go` doesn't need it directly (it just passes the dir string). No new imports required in main.go.

- [ ] **Step 3: Verify compilation**

Run: `go build ./cmd/labubu/...`
Expected: compilation succeeds

- [ ] **Step 4: Run all tests**

Run: `go test ./... 2>&1 | grep -E "^(ok|FAIL|---)"`
Expected: all packages PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/router.go cmd/labubu/main.go
git commit -m "feat: wire dashboard routes and --dashboards-dir flag"
```

---

### Task 5: Frontend API layer — types and fetch functions

**Files:**
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add dashboard types and API functions**

In `web/src/api/client.ts`, append after the existing `getServices` function:

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

export async function listDashboards(): Promise<{ panels: PanelConfig[] }> {
  return get<{ panels: PanelConfig[] }>(`${BASE_URL}/dashboards`)
}

export async function createDashboard(panel: Omit<PanelConfig, 'id'>): Promise<PanelConfig> {
  const res = await fetch(`${BASE_URL}/dashboards`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(panel),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function updateDashboard(panel: PanelConfig): Promise<PanelConfig> {
  const res = await fetch(`${BASE_URL}/dashboards/${panel.id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(panel),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function deleteDashboard(id: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/dashboards/${id}`, { method: 'DELETE' })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

export async function getMetricNames(): Promise<string[]> {
  const data = await get<{ status: string; data: string[] }>(`${BASE_URL}/metric-names`)
  return data.data || []
}

export async function getLabels(): Promise<string[]> {
  const data = await get<{ status: string; data: string[] }>(`${BASE_URL}/labels`)
  return data.data || []
}

export async function getLabelValues(name: string): Promise<string[]> {
  const data = await get<{ status: string; data: string[] }>(`${BASE_URL}/label/${name}/values`)
  return data.data || []
}

// Prometheus API response types for query results.
export interface QueryResult {
  status: string
  data: {
    resultType: 'vector' | 'matrix'
    result: Array<{
      metric: Record<string, string>
      value?: [number, string]
      values?: Array<[number, string]>
    }>
  }
  error?: string
}
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat: add dashboard API types and fetch functions to frontend"
```

---

### Task 6: PanelChart component

**Files:**
- Create: `web/src/components/PanelChart.vue`

- [ ] **Step 1: Create PanelChart.vue**

Create `web/src/components/PanelChart.vue`:

```vue
<template>
  <div class="panel-chart">
    <div class="panel-header">
      <h3 class="panel-title">{{ panel.title }}</h3>
      <div class="panel-actions">
        <button class="btn-icon" title="Edit" @click="$emit('edit', panel)">&#9998;</button>
        <button class="btn-icon" title="Delete" @click="$emit('delete', panel)">&#10005;</button>
      </div>
    </div>
    <div class="panel-body">
      <div v-if="loading" class="panel-state">Loading...</div>
      <div v-else-if="error" class="panel-state panel-error">{{ error }}</div>
      <div v-else-if="noData" class="panel-state">No data</div>
      <div v-else-if="panel.chartType === 'stat'" class="stat-value">
        <span class="stat-number">{{ formatValue(statValue) }}</span>
        <span class="stat-metric">{{ panel.metric }}</span>
      </div>
      <canvas v-else ref="canvasRef"></canvas>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { Chart, LineController, BarController, CategoryScale, LinearScale, TimeScale, PointElement, LineElement, BarElement, Tooltip, Legend, Filler } from 'chart.js'
import type { PanelConfig, QueryResult } from '../api/client'
import { getServices } from '../api/client'

// Register Chart.js components.
Chart.register(LineController, BarController, CategoryScale, LinearScale, TimeScale, PointElement, LineElement, BarElement, Tooltip, Legend, Filler)

const props = defineProps<{
  panel: PanelConfig
  timeRange: { start: number; end: number }
}>()

defineEmits<{
  edit: [panel: PanelConfig]
  delete: [panel: PanelConfig]
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
const loading = ref(false)
const error = ref('')
const noData = ref(false)
const statValue = ref(0)
let chart: Chart | null = null

function buildPromQL(): string {
  const labels = props.panel.labels || {}
  const labelPairs = Object.entries(labels)
    .map(([k, v]) => `${k}="${v}"`)
    .join(',')
  if (labelPairs) {
    return `${props.panel.metric}{${labelPairs}}`
  }
  return props.panel.metric
}

async function fetchData() {
  loading.value = true
  error.value = ''
  noData.value = false

  try {
    const query = buildPromQL()

    if (props.panel.chartType === 'stat') {
      const res = await fetch(`/api/v1/query?query=${encodeURIComponent(query)}`)
      const data: QueryResult = await res.json()
      if (data.status === 'error') {
        error.value = data.error || 'Query error'
        return
      }
      const results = data.data?.result || []
      if (results.length === 0 || !results[0].value) {
        noData.value = true
        return
      }
      statValue.value = parseFloat(results[0].value[1])
    } else {
      const step = props.panel.step || 60
      const startSec = Math.floor(props.timeRange.start / 1000)
      const endSec = Math.floor(props.timeRange.end / 1000)
      const url = `/api/v1/query_range?query=${encodeURIComponent(query)}&start=${startSec}&end=${endSec}&step=${step}`
      const res = await fetch(url)
      const data: QueryResult = await res.json()
      if (data.status === 'error') {
        error.value = data.error || 'Query error'
        return
      }
      const results = data.data?.result || []
      if (results.length === 0 || !results[0].values || results[0].values.length === 0) {
        noData.value = true
        return
      }
      await nextTick()
      renderChart(results[0].values)
    }
  } catch (e: any) {
    error.value = e.message || 'Failed to fetch data'
  } finally {
    loading.value = false
  }
}

function renderChart(values: Array<[number, string]>) {
  if (!canvasRef.value) return

  if (chart) {
    chart.destroy()
    chart = null
  }

  const labels = values.map(v => {
    const d = new Date(v[0] * 1000)
    return d.toLocaleTimeString()
  })
  const data = values.map(v => parseFloat(v[1]))

  chart = new Chart(canvasRef.value, {
    type: props.panel.chartType === 'bar' ? 'bar' : 'line',
    data: {
      labels,
      datasets: [{
        label: props.panel.title,
        data,
        borderColor: '#38bdf8',
        backgroundColor: props.panel.chartType === 'bar' ? '#38bdf888' : '#38bdf822',
        borderWidth: 2,
        fill: props.panel.chartType === 'line',
        tension: 0.3,
        pointRadius: 0,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          mode: 'index',
          intersect: false,
        },
      },
      scales: {
        x: {
          ticks: { color: '#94a3b8', maxTicksLimit: 8, font: { size: 10 } },
          grid: { color: '#1e293b' },
        },
        y: {
          ticks: { color: '#94a3b8', font: { size: 10 } },
          grid: { color: '#1e293b' },
        },
      },
    },
  })
}

function formatValue(v: number): string {
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(1) + 'M'
  if (v >= 1_000) return (v / 1_000).toFixed(1) + 'k'
  if (v === Math.floor(v)) return v.toString()
  return v.toFixed(2)
}

onMounted(fetchData)
watch(() => [props.timeRange, props.panel], fetchData, { deep: true })

onUnmounted(() => {
  if (chart) {
    chart.destroy()
    chart = null
  }
})
</script>

<style scoped>
.panel-chart {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  overflow: hidden;
}
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid #334155;
}
.panel-title { font-size: 14px; font-weight: 600; color: #e2e8f0; margin: 0; }
.panel-actions { display: flex; gap: 4px; }
.btn-icon {
  background: none; border: none; color: #64748b; cursor: pointer;
  font-size: 14px; padding: 4px; border-radius: 4px; line-height: 1;
}
.btn-icon:hover { color: #e2e8f0; background: #334155; }
.panel-body { padding: 16px; height: 280px; position: relative; }
.panel-body canvas { width: 100% !important; height: 100% !important; }
.panel-state {
  display: flex; align-items: center; justify-content: center;
  height: 100%; color: #94a3b8; font-size: 14px;
}
.panel-error { color: #f87171; }
.stat-value {
  display: flex; flex-direction: column; align-items: center;
  justify-content: center; height: 100%;
}
.stat-number { font-size: 48px; font-weight: 700; color: #38bdf8; line-height: 1.2; }
.stat-metric { font-size: 12px; color: #94a3b8; margin-top: 8px; }
</style>
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/PanelChart.vue
git commit -m "feat: add PanelChart component with line/bar/stat rendering"
```

---

### Task 7: PanelForm component (create/edit modal)

**Files:**
- Create: `web/src/components/PanelForm.vue`

- [ ] **Step 1: Create PanelForm.vue**

Create `web/src/components/PanelForm.vue`:

```vue
<template>
  <div class="modal-overlay" @click.self="$emit('cancel')">
    <div class="modal-content">
      <h3 class="modal-title">{{ isEdit ? 'Edit Panel' : 'New Panel' }}</h3>

      <form @submit.prevent="handleSubmit">
        <div class="form-group">
          <label for="pf-title">Title</label>
          <input id="pf-title" v-model="form.title" type="text" required placeholder="e.g. Token Usage" />
        </div>

        <div class="form-group">
          <label for="pf-metric">Metric</label>
          <select id="pf-metric" v-model="form.metric" required>
            <option value="" disabled>Select a metric...</option>
            <option v-for="m in metricNames" :key="m" :value="m">{{ m }}</option>
          </select>
        </div>

        <div class="form-group">
          <label>Labels (optional)</label>
          <div v-for="(item, idx) in form.labelEntries" :key="idx" class="label-row">
            <select v-model="item.key" @change="onLabelKeyChange(idx)">
              <option value="">-- key --</option>
              <option v-for="ln in sortedLabelNames" :key="ln" :value="ln">{{ ln }}</option>
            </select>
            <select v-model="item.value">
              <option value="">-- value --</option>
              <option v-for="lv in labelValuesCache[item.key] || []" :key="lv" :value="lv">{{ lv }}</option>
            </select>
            <button type="button" class="btn-remove" @click="removeLabel(idx)">x</button>
          </div>
          <button type="button" class="btn-add-label" @click="addLabel">+ Add label</button>
        </div>

        <div class="form-group">
          <label for="pf-charttype">Chart Type</label>
          <select id="pf-charttype" v-model="form.chartType" required>
            <option value="line">Line</option>
            <option value="bar">Bar</option>
            <option value="stat">Stat Card</option>
          </select>
        </div>

        <div class="form-group" v-if="form.chartType !== 'stat'">
          <label for="pf-step">Step (seconds)</label>
          <input id="pf-step" v-model.number="form.step" type="number" min="1" />
        </div>

        <div class="modal-actions">
          <button type="button" class="btn btn-secondary" @click="$emit('cancel')">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? 'Saving...' : 'Save' }}
          </button>
        </div>
        <p v-if="saveError" class="form-error">{{ saveError }}</p>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { getMetricNames, getLabels, getLabelValues } from '../api/client'
import type { PanelConfig } from '../api/client'

const props = defineProps<{
  panel?: PanelConfig | null
}>()

const emit = defineEmits<{
  saved: [panel: PanelConfig]
  cancel: []
}>()

const isEdit = computed(() => !!props.panel)

const form = reactive({
  title: props.panel?.title || '',
  metric: props.panel?.metric || '',
  labelEntries: [] as Array<{ key: string; value: string }>,
  chartType: (props.panel?.chartType || 'line') as 'line' | 'bar' | 'stat',
  step: props.panel?.step || 60,
})

// Populate initial label entries from panel.
if (props.panel?.labels) {
  form.labelEntries = Object.entries(props.panel.labels).map(([k, v]) => ({ key: k, value: v }))
}

const metricNames = ref<string[]>([])
const allLabelNames = ref<string[]>([])
const labelValuesCache = reactive<Record<string, string[]>>({})
const saving = ref(false)
const saveError = ref('')

const sortedLabelNames = computed(() => {
  return allLabelNames.value.filter(n => n !== '__name__').sort()
})

function addLabel() {
  form.labelEntries.push({ key: '', value: '' })
}

function removeLabel(idx: number) {
  form.labelEntries.splice(idx, 1)
}

async function onLabelKeyChange(idx: number) {
  const key = form.labelEntries[idx].key
  if (key && !labelValuesCache[key]) {
    try {
      const values = await getLabelValues(key)
      labelValuesCache[key] = values
    } catch {
      labelValuesCache[key] = []
    }
  }
}

async function handleSubmit() {
  saveError.value = ''
  if (!form.title || !form.metric) return

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
    if (isEdit.value && props.panel) {
      const res = await (await import('../api/client')).updateDashboard({
        ...panel,
        id: props.panel.id,
      })
      emit('saved', res)
    } else {
      const res = await (await import('../api/client')).createDashboard(panel)
      emit('saved', res)
    }
  } catch (e: any) {
    saveError.value = e.message || 'Save failed'
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try {
    const [names, labels] = await Promise.all([getMetricNames(), getLabels()])
    metricNames.value = names.sort()
    allLabelNames.value = labels
  } catch {
    // populating dropdowns is best-effort
  }
})
</script>

<style scoped>
.modal-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.7);
  display: flex; align-items: center; justify-content: center;
  z-index: 1000;
}
.modal-content {
  background: #1e293b; border: 1px solid #334155; border-radius: 12px;
  padding: 24px; width: 480px; max-height: 90vh; overflow-y: auto;
}
.modal-title { font-size: 18px; font-weight: 600; color: #e2e8f0; margin: 0 0 20px 0; }
.form-group { margin-bottom: 16px; }
.form-group label { display: block; font-size: 13px; color: #94a3b8; margin-bottom: 6px; }
.form-group input, .form-group select {
  width: 100%; padding: 8px 12px; background: #0f172a; border: 1px solid #334155;
  border-radius: 6px; color: #e2e8f0; font-size: 14px; box-sizing: border-box;
}
.form-group input:focus, .form-group select:focus { border-color: #38bdf8; outline: none; }
.label-row { display: flex; gap: 8px; margin-bottom: 8px; }
.label-row select { flex: 1; }
.btn-remove {
  background: none; border: none; color: #f87171; cursor: pointer;
  font-size: 16px; padding: 0 8px;
}
.btn-add-label {
  background: none; border: 1px dashed #334155; color: #94a3b8;
  padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; width: 100%;
}
.btn-add-label:hover { border-color: #38bdf8; color: #38bdf8; }
.modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 20px; }
.btn {
  padding: 8px 20px; border-radius: 6px; border: none;
  font-size: 14px; cursor: pointer; font-weight: 500;
}
.btn-primary { background: #38bdf8; color: #000; }
.btn-primary:hover { background: #7dd3fc; }
.btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-secondary { background: #334155; color: #e2e8f0; }
.btn-secondary:hover { background: #475569; }
.form-error { color: #f87171; font-size: 13px; margin-top: 8px; }
</style>
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/PanelForm.vue
git commit -m "feat: add PanelForm modal component for create/edit"
```

---

### Task 8: Dashboard page + routing + sidebar

**Files:**
- Create: `web/src/views/Dashboard.vue`
- Modify: `web/src/router.ts`
- Modify: `web/src/App.vue`

- [ ] **Step 1: Create Dashboard.vue**

Create `web/src/views/Dashboard.vue`:

```vue
<template>
  <div class="dashboard-page">
    <div class="dashboard-toolbar">
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
      </div>
      <button class="btn btn-primary" @click="openCreate">+ New Panel</button>
    </div>

    <div v-if="loading" class="page-state">Loading...</div>
    <div v-else-if="loadError" class="page-state page-error">{{ loadError }}</div>
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
        @edit="openEdit"
        @delete="confirmDelete"
      />
    </div>

    <PanelForm
      v-if="showForm"
      :panel="editingPanel"
      @saved="onSaved"
      @cancel="closeForm"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { listDashboards, deleteDashboard } from '../api/client'
import type { PanelConfig } from '../api/client'
import PanelChart from '../components/PanelChart.vue'
import PanelForm from '../components/PanelForm.vue'

const panels = ref<PanelConfig[]>([])
const loading = ref(true)
const loadError = ref('')

const showForm = ref(false)
const editingPanel = ref<PanelConfig | null>(null)

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

function setPreset(label: string, duration: number) {
  activePreset.value = label
  if (label === 'custom') {
    const end = new Date()
    const start = new Date(end.getTime() - 3600000)
    customEnd.value = end.toISOString().slice(0, 16)
    customStart.value = start.toISOString().slice(0, 16)
  }
}

async function loadPanels() {
  loading.value = true
  loadError.value = ''
  try {
    const data = await listDashboards()
    panels.value = data.panels || []
  } catch (e: any) {
    loadError.value = e.message || 'Failed to load dashboards'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingPanel.value = null
  showForm.value = true
}

function openEdit(panel: PanelConfig) {
  editingPanel.value = panel
  showForm.value = true
}

function closeForm() {
  showForm.value = false
  editingPanel.value = null
}

function onSaved(_panel: PanelConfig) {
  closeForm()
  loadPanels()
}

async function confirmDelete(panel: PanelConfig) {
  if (!confirm(`Delete panel "${panel.title}"?`)) return
  try {
    await deleteDashboard(panel.id)
    panels.value = panels.value.filter(p => p.id !== panel.id)
  } catch (e: any) {
    alert(`Delete failed: ${e.message}`)
  }
}

onMounted(loadPanels)
</script>

<style scoped>
.dashboard-page { max-width: 1400px; margin: 0 auto; }
.dashboard-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 24px; flex-wrap: wrap; gap: 12px;
}
.time-presets { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.btn-preset {
  padding: 6px 16px; border: 1px solid #334155; background: #1e293b;
  color: #94a3b8; border-radius: 6px; cursor: pointer; font-size: 13px;
}
.btn-preset.active { background: #38bdf8; color: #000; border-color: #38bdf8; }
.btn-preset:hover:not(.active) { border-color: #38bdf8; color: #38bdf8; }
.custom-range { display: flex; align-items: center; gap: 8px; }
.custom-range input {
  background: #0f172a; border: 1px solid #334155; border-radius: 6px;
  color: #e2e8f0; padding: 6px 10px; font-size: 13px;
}
.custom-range span { color: #94a3b8; font-size: 13px; }
.btn {
  padding: 8px 20px; border-radius: 6px; border: none;
  font-size: 14px; cursor: pointer; font-weight: 500;
}
.btn-primary { background: #38bdf8; color: #000; }
.btn-primary:hover { background: #7dd3fc; }
.panel-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 20px;
}
@media (max-width: 960px) {
  .panel-grid { grid-template-columns: 1fr; }
}
.page-state {
  text-align: center; padding: 80px 20px; color: #94a3b8; font-size: 15px;
}
.page-error { color: #f87171; }
</style>
```

- [ ] **Step 2: Add route to router.ts**

In `web/src/router.ts`, add the import and route. Replace the entire file with:

```typescript
import { createRouter, createWebHistory } from 'vue-router'
import TraceList from './views/TraceList.vue'
import TraceDetail from './views/TraceDetail.vue'
import Dashboard from './views/Dashboard.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/traces' },
    { path: '/traces', name: 'trace-list', component: TraceList },
    { path: '/traces/:id', name: 'trace-detail', component: TraceDetail },
    { path: '/dashboards', name: 'dashboards', component: Dashboard },
  ]
})
```

- [ ] **Step 3: Add sidebar link to App.vue**

In `web/src/App.vue`, add the "Metrics" nav link below the existing "Trace" link. Change the `<nav>` block from:

```html
<nav class="app-nav">
  <router-link to="/traces">Trace</router-link>
</nav>
```

To:

```html
<nav class="app-nav">
  <router-link to="/traces">Trace</router-link>
  <router-link to="/dashboards">Metrics</router-link>
</nav>
```

- [ ] **Step 4: Build frontend for production**

Run: `cd web && npm run build`
Expected: build succeeds, output in `web/dist/`

- [ ] **Step 5: Commit**

```bash
git add web/src/views/Dashboard.vue web/src/router.ts web/src/App.vue web/dist/
git commit -m "feat: add Dashboard page with time range, chart grid, and sidebar link"
```

---

### Self-Review

**1. Spec coverage:**
- Panel config CRUD → Task 3 (backend handler) + Task 5 (frontend API)
- Metric names API → Task 2
- `__name__` label tracking → Task 1
- Dashboard page with time range selector → Task 8
- PanelChart (line/bar/stat) → Task 6
- PanelForm (modal with label discovery) → Task 7
- Route registration → Task 4 (backend) + Task 8 (frontend)

**2. Placeholder scan:** No TBD/TODO. All code is complete.

**3. Type consistency:**
- `PanelConfig` struct matches between Go (`dashboard_handler.go`) and TypeScript (`client.ts`)
- Chart types: `line` | `bar` | `stat` consistent across all layers
- API response shape `{"panels": [...]}` consistent between handler and frontend function
- Route path `/api/v1/dashboards/` and `/api/v1/dashboards` both registered to handle prefix routing
