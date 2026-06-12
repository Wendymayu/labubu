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
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	ExpressionType  string            `json:"expressionType"`            // "single" | "ratio"
	Metric          string            `json:"metric"`                    // Single: main metric; Ratio: denominator
	NumeratorMetric string            `json:"numeratorMetric,omitempty"` // Ratio: numerator
	Labels          map[string]string `json:"labels"`
	Func            string            `json:"func"`                      // "none" | "rate" | "increase"
	Aggregation     string            `json:"aggregation"`               // "none" | "sum" | "avg" | "max" | "min"
	GroupBy         string            `json:"groupBy,omitempty"`         // label key for aggregation grouping
	ChartType       string            `json:"chartType"`
	Step            int               `json:"step,omitempty"`
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
		ID:   uuid.NewString(),
		Name: req.Name,
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
	if pc.ExpressionType == "" {
		pc.ExpressionType = "single"
	}
	if pc.ExpressionType != "single" && pc.ExpressionType != "ratio" {
		http.Error(w, "expressionType must be single or ratio", http.StatusBadRequest)
		return
	}
	if pc.ExpressionType == "ratio" && pc.NumeratorMetric == "" {
		http.Error(w, "numeratorMetric is required for ratio expressions", http.StatusBadRequest)
		return
	}
	if pc.Func == "" {
		pc.Func = "none"
	}
	if pc.Aggregation == "" {
		pc.Aggregation = "none"
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
