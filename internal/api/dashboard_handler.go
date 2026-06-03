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
