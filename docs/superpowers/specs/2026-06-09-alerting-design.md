# Alerting System Design Spec

> Status: Draft | Date: 2026-06-09 | Roadmap: #21

## Overview

Add an alerting system to Labubu that evaluates traces against user-defined threshold rules and sends notifications when conditions are met. MVP focuses on token consumption alerts via email (SMTP), with architecture designed for extensibility to additional metrics, notification channels, and multi-tenant use.

## Scope & Decisions

| Dimension | Decision | Rationale |
|-----------|----------|-----------|
| Alert metric (MVP) | Token consumption per Trace | Core LLM cost concern; directly maps to existing `total_tokens` field |
| Rule management | API + frontend CRUD | Real-time rule changes without restart; aligns with existing UI patterns (Pricing, Dashboards) |
| Evaluation engine | Polling + state machine | Standard pattern (Prometheus, Grafana, Nightingale); enables flap prevention via `for` duration |
| Condition model | Multi-condition AND | Filters by model + threshold in one rule; OR achieved via separate rules |
| Notification (MVP) | Email via SMTP | Zero external API dependency; architecture extensible to DingTalk/Feishu/Slack |
| Storage | SQLite (embedded) | Zero-dependency, local-first, matches Labubu's philosophy |
| Scope | Single-instance, single-user | Rules apply globally; multi-tenant deferred |
| State machine states | ok → pending → firing → resolved | Industry standard; `pending` provides debounce window |

## Architecture

```
internal/
  alerting/
    rules/        — Rule CRUD + SQLite persistence
    engine/       — Evaluation engine (polling + state machine)
    notifier/     — Notification dispatch (SMTP + extension interface)
    api/          — HTTP handlers (rule management + alert status/history)
```

### Dependency Flow

```
api → {rules, engine}
engine → {rules, notifier}
```

- `api` calls `rules` for rule CRUD and `engine` for alert state / notification history queries
- `engine` reads rules from `rules` store and dispatches through `notifier`
- No circular references; each package exposes an interface consumed by the package above

### Component Diagram

```
┌──────────────────────────────────────────────┐
│  HTTP API (alerting/api)                     │
│  POST /api/alerts/rules      创建规则        │
│  GET  /api/alerts/rules      规则列表        │
│  PUT  /api/alerts/rules/:id  更新规则        │
│  DEL  /api/alerts/rules/:id  删除规则        │
│  GET  /api/alerts/states     告警状态        │
│  GET  /api/alerts/notifications 通知历史     │
└──────┬──────────────────┬───────────────────┘
       │  CRUD            │  queries
       ▼                  ▼
┌──────────────┐  ┌───────────────────────────┐
│ Rule Store   │  │ Engine (alerting/engine)  │
│ (SQLite)     │  │ Polling → eval → state    │
│              │  │ machine → events          │
└──────────────┘  └───────────┬───────────────┘
                              │
                   ┌──────────▼──────────┐
                   │ Notifier            │
                   │ SMTP → Email        │
                   │ (DingTalk/Feishu    │
                   │  deferred)          │
                   └─────────────────────┘
```

## Data Model

### alert_rules

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT (UUID) | Primary key |
| name | TEXT | Rule name, e.g. "Opus Token Spike" |
| enabled | INTEGER (bool) | 1 = enabled, 0 = paused |
| metric | TEXT | Metric name; MVP fixed to `total_tokens`; reserved for extension |
| conditions | JSON | AND-combined condition array. Example: `[{"field":"total_tokens","op":"gt","value":"100000"},{"field":"model","op":"eq","value":"claude-opus-4-8"}]` |
| for_duration | INTEGER | Debounce duration in seconds; default 300 |
| interval | INTEGER | Evaluation polling interval in seconds; default 60 |
| notifier | JSON | Notification config. MVP email: `{"type":"email","smtp_host":"...","smtp_port":587,"username":"...","password":"...","recipients":["a@b.com"]}` |
| created_at | TEXT (ISO8601) | |
| updated_at | TEXT (ISO8601) | |

### alert_states

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT (UUID) | Primary key |
| rule_id | TEXT | FK → alert_rules.id |
| trace_id | TEXT | The triggering trace's ID |
| status | TEXT | `ok` / `pending` / `firing` / `resolved` |
| triggered_at | TEXT | Timestamp when condition first met |
| last_fired_at | TEXT | Timestamp of last firing notification |
| resolved_at | TEXT | Timestamp when condition cleared |

### alert_notifications

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT (UUID) | Primary key |
| rule_id | TEXT | FK → alert_rules.id |
| trace_id | TEXT | The triggering trace's ID |
| action | TEXT | `firing` or `resolved` |
| channel | TEXT | Notification channel type, e.g. `email` |
| recipient | TEXT | Recipient address |
| sent_at | TEXT | Timestamp |
| success | INTEGER | 0 = failed, 1 = sent |
| error_msg | TEXT | Failure reason if success = 0 |

### Condition Operators

| Operator | JSON Value | Meaning |
|----------|------------|---------|
| `gt` | `> ` | Greater than |
| `gte` | `>=` | Greater than or equal |
| `lt` | `<` | Less than |
| `lte` | `<=` | Less than or equal |
| `eq` | `=` | Equal |
| `neq` | `!=` | Not equal |

### Condition Fields (MVP)

| Field | Data Type | Source |
|-------|-----------|--------|
| `total_tokens` | integer | Trace span aggregation |
| `input_tokens` | integer | Trace span aggregation |
| `output_tokens` | integer | Trace span aggregation |
| `model` | string | Trace / Span attribute |

## Evaluation Engine & State Machine

### Polling Loop

1. Load all enabled rules with `interval` and `for_duration` config
2. For each rule whose interval has elapsed since last evaluation:
   - Query traces created since last evaluation time
   - Filter traces matching all `conditions` (AND logic)
   - For each matching trace, drive the state machine
3. Sleep until next tick

### State Machine

```
       ┌──────────┐
       │   ok     │  ← Initial state
       └─────┬────┘
             │ Condition met
             ▼
       ┌──────────┐
       │ pending  │  ← Timer started, no notification
       └─────┬────┘
             │ Condition sustained ≥ for_duration
             ▼
       ┌──────────┐
  ┌───→│ firing   │  ← Send firing notification
  │    └─────┬────┘
  │         │ Condition no longer met
  │         ▼
  │    ┌──────────┐
  └────│ resolved │  ← Send resolved notification
       └──────────┘
```

### Key Behaviors

- **Debounce**: Transition from `ok` to `pending` does NOT trigger notification. Only `pending → firing` after sustained violation for `for_duration` seconds sends a firing notification.
- **Deduplication**: A `firing` state does not re-fire. Only state transitions produce notifications.
- **Persistence**: All state machine states are stored in SQLite `alert_states`. Service restart does not lose pending timers — the engine recalculates from `triggered_at`.
- **Garbage collection**: `resolved` states older than a configurable TTL (default 7 days) are periodically pruned.

## Notification

### Notifier Interface

```go
type Notifier interface {
    Type() string  // "email", "dingtalk", "feishu", "slack" ...
    Fire(ctx context.Context, rule Rule, state AlertState) error
    Resolve(ctx context.Context, rule Rule, state AlertState) error
}
```

### MVP: EmailNotifier (SMTP)

> **Note**: SMTP credentials are stored in plaintext within the SQLite `alert_rules.notifier` JSON field. For Labubu's local-first, single-user deployment model this is acceptable. If multi-tenant support is added in the future, credential encryption or external secret management should be addressed.

Configured per-rule via the `notifier` JSON field:

```json
{
  "type": "email",
  "smtp_host": "smtp.gmail.com",
  "smtp_port": 587,
  "username": "xxx@gmail.com",
  "password": "app-password",
  "recipients": ["me@example.com"]
}
```

### Notification Templates

**Firing notification:**

```
Subject: [LABUBU ALERT] Rule "Opus Token Spike" is firing

Rule: Opus Token Spike
Trace: abc123
Model: claude-opus-4-8
Total Tokens: 156,432
Threshold: 100,000
Triggered at: 2026-06-09 14:32:00

View details: http://localhost:8080/traces/abc123
```

**Resolved notification:**

```
Subject: [LABUBU RESOLVED] Rule "Opus Token Spike" recovered

Rule: Opus Token Spike
Trace: abc123
Resolved at: 2026-06-09 14:45:00
```

### Extension Design

Channel registry pattern:

```go
// Registration
notifier.Register("email", NewEmailNotifier)
notifier.Register("dingtalk", NewDingtalkNotifier)

// Dispatch
notifier, ok := registry.Get(rule.Notifier.Type)
notifier.Fire(ctx, rule, state)
```

Future channels (DingTalk, Feishu, Slack, Webhook) implement the `Notifier` interface and register themselves. No changes required in engine or API layers.

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/alerts/rules` | List all rules |
| `POST` | `/api/alerts/rules` | Create a rule |
| `GET` | `/api/alerts/rules/:id` | Get rule detail |
| `PUT` | `/api/alerts/rules/:id` | Update a rule |
| `DELETE` | `/api/alerts/rules/:id` | Delete a rule |
| `GET` | `/api/alerts/states` | List alert states (supports `?status=firing` filter) |
| `GET` | `/api/alerts/notifications` | List notification history (supports `?rule_id=` filter) |

## Frontend

### Routes

| Route | Component | Description |
|-------|-----------|-------------|
| `/alerts/rules` | RuleList | Rule table with create entry point |
| `/alerts/rules/new` | RuleForm | Create new rule |
| `/alerts/rules/:id/edit` | RuleForm | Edit existing rule |
| `/alerts/history` | AlertHistory | Alert states and notification history |

Added to the existing sidebar navigation, following the same patterns as Pricing and Dashboards.

### RuleList Page

Table columns: Name, Enabled (toggle), Metric, Last Triggered, Actions (edit / delete). Top toolbar with search input and "New Rule" button.

### RuleForm Page

Form fields:
- **Name** — text input
- **Enabled** — toggle switch
- **Conditions** — repeatable rows, each with:
  - Field dropdown: `total_tokens`, `input_tokens`, `output_tokens`, `model`
  - Operator dropdown: `>`, `>=`, `<`, `<=`, `=`, `!=`
  - Value text input
  - "+" button to add rows; "×" button to remove rows
  - Rows are visually joined with "AND" label
- **Debounce duration** — number input, seconds, default 300
- **Evaluation interval** — number input, seconds, default 60
- **SMTP Configuration** — Host, Port, Username, Password, Recipients (comma-separated or tag input)
- Bottom: Save / Cancel buttons

### AlertHistory Page

Table columns: Rule Name, Trace ID (clickable link → TraceDetail), Status (colored badge: firing=red, resolved=green), Triggered At, Resolved At, Channel. Supports filtering by rule and status.

### Tech Stack

Vue 3 + TypeScript + Vue Router + existing i18n system. Reuses existing UI patterns (table components, form layouts, API client conventions).

## Testing

- **Unit tests**: Rule condition evaluation, state machine transitions, notifier interface
- **Integration tests**: SQLite rule CRUD, engine polling loop with known trace data
- **No new end-to-end tests** required for MVP
- Follow existing Go test patterns (`go test -v ./internal/alerting/...`)

## Out of Scope (Deferred)

- Multi-metric rules (error rate, latency)
- Multi-channel notification (DingTalk, Feishu, Slack, Webhook)
- Multi-tenant / per-team rules
- Live Tail integration (real-time alert evaluation)
- Alert aggregation / grouping
- Silence / maintenance windows
- Rule templates or import/export
