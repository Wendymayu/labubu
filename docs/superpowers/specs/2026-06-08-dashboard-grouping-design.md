# Dashboard Grouping — Design Spec

**Date:** 2026-06-08  
**Status:** approved

## Overview

Allow users to create multiple named dashboards, each containing its own set of metric panels. Currently all panels live on a single flat page — this adds organization and isolation by data source, project, or topic.

## Motivation

- Users run multiple systems (e.g., Claude Code and Jiuwenclaw) and want to view their metrics separately
- Current single-page panel grid doesn't scale beyond ~10 panels
- Grafana-like multi-dashboard model is the standard pattern in observability

## Architecture

```
┌─ Dashboard 页面 ───────────────────────────────────────────────┐
│  ┌─ Tab 栏 ────────────────────────────────────────────────┐   │
│  │  [Default] [Claude Code] [Jiuwenclaw]  [+ New Tab]     │   │
│  └─────────────────────────────────────────────────────────┘   │
│  ┌─ 工具栏 ────────────────────────────────────────────────┐   │
│  │  [1h] [6h] [24h] [Custom]  [Rename] [Delete] [+ Panel] │   │
│  └─────────────────────────────────────────────────────────┘   │
│  ┌─ 面板网格 (2-col) ──────────────────────────────────────┐   │
│  │  ┌──────────┐  ┌──────────┐                             │   │
│  │  │ Panel 1  │  │ Panel 2  │  ...                        │   │
│  │  └──────────┘  └──────────┘                             │   │
│  └─────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

**Tech Stack:** Go `net/http`, Vue 3 Composition API, Chart.js 4, JSON files.

## Data Model

### Dashboard

```json
{
  "id": "uuid-v4",
  "name": "Claude Code",
  "created_at": "2026-06-08T10:00:00Z",
  "panels": []
}
```

### PanelConfig (existing, plus `dashboard_id`)

```json
{
  "id": "uuid-v4",
  "title": "Token Usage",
  "metric": "gen_ai_client_token_usage",
  "labels": {"model": "claude-opus-4-8"},
  "chartType": "line",
  "step": 60
}
```

### Directory Structure

```
data/dashboards/
├── index.json                          ← dashboard index (list of dashboards with metadata)
└── {dashboard_id}/
    └── panels/
        ├── {panel_id}.json             ← one file per panel
        └── ...
```

## API Design

| Method | Path | Request Body | Response | Description |
|--------|------|-------------|----------|-------------|
| `GET` | `/api/v1/dashboards` | — | `{"dashboards": [...]}` | List all dashboards with nested panels |
| `POST` | `/api/v1/dashboards` | `{"name": "..."}` | Dashboard object | Create a dashboard |
| `PUT` | `/api/v1/dashboards/{id}` | `{"name": "..."}` | Dashboard object | Rename a dashboard |
| `DELETE` | `/api/v1/dashboards/{id}` | — | `{"status":"ok"}` | Delete dashboard + all its panels |
| `POST` | `/api/v1/dashboards/{dashId}/panels` | PanelConfig (no id) | PanelConfig (with id) | Create panel in dashboard |
| `PUT` | `/api/v1/dashboards/{dashId}/panels/{panelId}` | PanelConfig | PanelConfig | Update a panel |
| `DELETE` | `/api/v1/dashboards/{dashId}/panels/{panelId}` | — | `{"status":"ok"}` | Delete a panel |

### GET /api/v1/dashboards Response

```json
{
  "dashboards": [
    {
      "id": "uuid-1",
      "name": "Default",
      "created_at": "2026-06-08T10:00:00Z",
      "panels": [
        {
          "id": "panel-uuid",
          "title": "Token Usage",
          "metric": "gen_ai_client_token_usage",
          "labels": {},
          "chartType": "line",
          "step": 60
        }
      ]
    },
    {
      "id": "uuid-2",
      "name": "Claude Code",
      "created_at": "2026-06-08T11:00:00Z",
      "panels": []
    }
  ]
}
```

## Frontend Design

### Route

| Path | Name | Component |
|------|------|-----------|
| `/dashboards` | `dashboards` | `Dashboard.vue` |

### Dashboard.vue — State

- `dashboards: Dashboard[]` — all dashboards with nested panels
- `activeDashboardId: string` — currently selected dashboard
- `panels: PanelConfig[]` — computed: panels of active dashboard
- `timeRange`, `timePreset` — per dashboard (reset on tab switch)

### Tab Bar

- Horizontal tab row above the toolbar
- Each tab shows dashboard name
- Active tab highlighted with accent color
- Rightmost: "+" button — click → tiny input popover, type name, Enter to create
- Tab switches call `POST /api/v1/dashboards` (create) or just set `activeDashboardId`

### Toolbar

Extends existing toolbar with two additional buttons (after time presets):

- **Rename** — opens inline input or popover, pre-filled with current name, Enter to save → `PUT /api/v1/dashboards/{id}`
- **Delete** — confirm dialog, then `DELETE /api/v1/dashboards/{id}` → switch to first remaining dashboard
- **+ New Panel** — unchanged, but creates panel under `activeDashboardId` via `POST /api/v1/dashboards/{dashId}/panels`

### Empty States

- **No dashboards at all:** centered message "No dashboards yet. Create your first dashboard." with a create button
- **Dashboard exists but no panels:** existing "No panels yet. + New Panel" state unchanged
- **All dashboards deleted:** auto-create a "Default" dashboard to avoid dead-end empty state

### PanelForm.vue

Unchanged — still opens as modal. Only the API endpoint it calls changes (panel CRUD routes gain the `dashboard_id` prefix).

## Edge Cases

| Case | Behavior |
|------|----------|
| No dashboards exist | Show "Create your first dashboard" prompt |
| Delete last dashboard | Auto-create a "Default" dashboard (empty) |
| Delete dashboard with panels | Cascade delete all panels in that dashboard |
| Empty dashboard name on create | Reject with validation error |
| Duplicate dashboard name | Allowed (no uniqueness constraint) |
| Rename to same name | Allowed (no-op) |
| Tab overflow (many dashboards) | Horizontal scroll on tab bar |

## Out of Scope

- Legacy data migration (no backward compatibility for existing panels)
- Dashboard reordering via drag-and-drop
- Panel reordering via drag-and-drop
- Dashboard-level time range per-user persistence
- Dashboard import/export
- Role-based access control
