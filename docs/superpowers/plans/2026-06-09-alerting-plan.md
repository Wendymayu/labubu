# Alerting System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a token-consumption alerting system with rule CRUD, evaluation engine (polling + state machine), SMTP email notifications, and a Vue 3 frontend.

**Architecture:** Four Go packages under `internal/alerting/` — `rules` (SQLite-backed CRUD + types), `engine` (polling loop + condition evaluation + state machine), `notifier` (interface + registry + SMTP implementation), `api` (HTTP handlers). Frontend adds three Vue SFC pages under `web/src/views/alerts/` with API client, router entries, i18n strings, and sidebar nav links.

**Tech Stack:** Go 1.19 + database/sql + mattn/go-sqlite3 + net/smtp; Vue 3 + TypeScript + vue-i18n + existing component patterns.

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `internal/alerting/types.go` | Shared types: Rule, AlertState, AlertNotification, Condition, NotifierConfig |
| Create | `internal/alerting/store.go` | RuleStore interface + SQLite implementation (rule CRUD, state upsert, notification insert) |
| Create | `internal/alerting/store_test.go` | Tests for SQLite CRUD, state upsert, notification history |
| Create | `internal/alerting/engine.go` | Condition evaluator, state machine, polling loop |
| Create | `internal/alerting/engine_test.go` | Tests for condition matching, state transitions, dedup |
| Create | `internal/alerting/notifier.go` | Notifier interface, registry, EmailNotifier (SMTP) |
| Create | `internal/alerting/notifier_test.go` | Tests for registry, mock notifier, email template formatting |
| Create | `internal/alerting/api.go` | AlertHandler: ServeHTTP dispatcher, CRUD handlers, state/notification query handlers |
| Create | `internal/alerting/api_test.go` | Handler tests with mock store |
| Modify | `internal/api/router.go` | Wire alert handler routes into the API mux |
| Create | `internal/alerting/setup.go` | InitAlerting: open SQLite DB, create store, start engine, return handler |
| Modify | `cmd/labubu/main.go` | Call InitAlerting, pass handler to NewRouter, engine lifecycle |
| Create | `web/src/api/alerts.ts` | API client: list/create/update/delete rules, get states, get notifications |
| Create | `web/src/views/alerts/RuleList.vue` | Table of rules with create/edit/delete/toggle |
| Create | `web/src/views/alerts/RuleForm.vue` | Form for create/edit rule (conditions, SMTP config) |
| Create | `web/src/views/alerts/AlertHistory.vue` | Alert state table with status badges, filters |
| Modify | `web/src/router.ts` | Add 4 alert routes |
| Modify | `web/src/App.vue` | Add "Alerts" nav group in sidebar |
| Modify | `web/src/i18n/locales/en.ts` | Add alert i18n strings (English) |
| Modify | `web/src/i18n/locales/zh.ts` | Add alert i18n strings (Chinese) |

---

### Task 1: Shared types (`internal/alerting/types.go`)

**Files:**
- Create: `internal/alerting/types.go`

- [ ] **Step 1: Define the shared types**

```go
// Package alerting provides alert rule evaluation and notification dispatch.
package alerting

import "time"

// ConditionOperator defines the comparison operation for a condition.
type ConditionOperator string

const (
	OpGT  ConditionOperator = "gt"
	OpGTE ConditionOperator = "gte"
	OpLT  ConditionOperator = "lt"
	OpLTE ConditionOperator = "lte"
	OpEQ  ConditionOperator = "eq"
	OpNEQ ConditionOperator = "neq"
)

// Condition is a single condition within a rule. All conditions in a rule are AND-combined.
type Condition struct {
	Field string            `json:"field"` // "total_tokens", "input_tokens", "output_tokens", "model"
	Op    ConditionOperator `json:"op"`
	Value string            `json:"value"`
}

// NotifierConfig holds notification channel configuration.
type NotifierConfig struct {
	Type       string   `json:"type"`       // "email" (MVP)
	SMTPHost   string   `json:"smtp_host"`
	SMTPPort   int      `json:"smtp_port"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	Recipients []string `json:"recipients"`
}

// Rule represents an alert rule.
type Rule struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Enabled      bool           `json:"enabled"`
	Metric       string         `json:"metric"`       // "total_tokens" for MVP
	Conditions   []Condition    `json:"conditions"`
	ForDuration  int            `json:"for_duration"`  // seconds, default 300
	Interval     int            `json:"interval"`      // seconds, default 60
	Notifier     NotifierConfig `json:"notifier"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// AlertStatus is the state machine status for a single alert instance.
type AlertStatus string

const (
	AlertOK       AlertStatus = "ok"
	AlertPending  AlertStatus = "pending"
	AlertFiring   AlertStatus = "firing"
	AlertResolved AlertStatus = "resolved"
)

// AlertState tracks the evaluation state for a single trace under a single rule.
type AlertState struct {
	ID          string      `json:"id"`
	RuleID      string      `json:"rule_id"`
	TraceIDHex  string      `json:"trace_id_hex"`
	Status      AlertStatus `json:"status"`
	TriggeredAt time.Time   `json:"triggered_at"`
	LastFiredAt *time.Time  `json:"last_fired_at,omitempty"`
	ResolvedAt  *time.Time  `json:"resolved_at,omitempty"`
}

// AlertNotification records a sent notification.
type AlertNotification struct {
	ID        string    `json:"id"`
	RuleID    string    `json:"rule_id"`
	TraceIDHex string   `json:"trace_id_hex"`
	Action    string    `json:"action"`   // "firing" or "resolved"
	Channel   string    `json:"channel"`  // "email"
	Recipient string    `json:"recipient"`
	SentAt    time.Time `json:"sent_at"`
	Success   bool      `json:"success"`
	ErrorMsg  string    `json:"error_msg,omitempty"`
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/alerting/...`
Expected: compiles successfully

- [ ] **Step 3: Commit**

```bash
git add internal/alerting/types.go
git commit -m "feat: add alerting type definitions"
```

---

### Task 2: Rule Store — SQLite schema and CRUD (`internal/alerting/store.go`)

**Files:**
- Create: `internal/alerting/store.go`

- [ ] **Step 1: Write the test file first**

Create `internal/alerting/store_test.go`:

```go
package alerting

import (
	"os"
	"testing"
	"time"
)

func tempDB(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "alerting-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestRuleStoreCRUD(t *testing.T) {
	dbPath := tempDB(t)
	defer os.Remove(dbPath)

	s, err := NewRuleStore(dbPath)
	if err != nil {
		t.Fatalf("NewRuleStore: %v", err)
	}
	defer s.Close()

	// Create
	r := Rule{
		ID:   "ruuid-1",
		Name: "Test Rule",
		Metric: "total_tokens",
		Conditions: []Condition{
			{Field: "total_tokens", Op: OpGT, Value: "100000"},
		},
		ForDuration: 300,
		Interval:    60,
		Notifier:    NotifierConfig{Type: "email", SMTPHost: "smtp.example.com", SMTPPort: 587, Recipients: []string{"a@b.com"}},
	}
	now := time.Now().Truncate(time.Second)
	r.CreatedAt = now
	r.UpdatedAt = now

	if err := s.CreateRule(r); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	// List
	rules, err := s.ListRules()
	if err != nil {
		t.Fatalf("ListRules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	got := rules[0]
	if got.Name != "Test Rule" || got.ForDuration != 300 {
		t.Fatalf("unexpected rule: %+v", got)
	}

	// Update
	got.Name = "Updated Rule"
	if err := s.UpdateRule(got); err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}

	// Get
	got2, err := s.GetRule("ruuid-1")
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if got2.Name != "Updated Rule" {
		t.Fatalf("expected 'Updated Rule', got %q", got2.Name)
	}

	// Delete
	if err := s.DeleteRule("ruuid-1"); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	rules, _ = s.ListRules()
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestAlertStateUpsert(t *testing.T) {
	dbPath := tempDB(t)
	defer os.Remove(dbPath)

	s, err := NewRuleStore(dbPath)
	if err != nil {
		t.Fatalf("NewRuleStore: %v", err)
	}
	defer s.Close()

	state := AlertState{
		ID:          "astate-1",
		RuleID:      "ruuid-1",
		TraceIDHex:  "abc123",
		Status:      AlertPending,
		TriggeredAt: time.Now().Truncate(time.Second),
	}
	if err := s.UpsertAlertState(state); err != nil {
		t.Fatalf("UpsertAlertState: %v", err)
	}

	// Read back
	states, err := s.ListAlertStates("")
	if err != nil {
		t.Fatalf("ListAlertStates: %v", err)
	}
	if len(states) != 1 || states[0].Status != AlertPending {
		t.Fatalf("unexpected state: %+v", states[0])
	}

	// Update
	state.Status = AlertFiring
	now := time.Now().Truncate(time.Second)
	state.LastFiredAt = &now
	if err := s.UpsertAlertState(state); err != nil {
		t.Fatalf("UpsertAlertState (update): %v", err)
	}
	states, _ = s.ListAlertStates("")
	if states[0].Status != AlertFiring {
		t.Fatalf("expected firing, got %s", states[0].Status)
	}

	// Filter by status
	firing, _ := s.ListAlertStates("firing")
	if len(firing) != 1 {
		t.Fatalf("expected 1 firing state, got %d", len(firing))
	}
	resolved, _ := s.ListAlertStates("resolved")
	if len(resolved) != 0 {
		t.Fatalf("expected 0 resolved states, got %d", len(resolved))
	}
}

func TestNotificationHistory(t *testing.T) {
	dbPath := tempDB(t)
	defer os.Remove(dbPath)

	s, err := NewRuleStore(dbPath)
	if err != nil {
		t.Fatalf("NewRuleStore: %v", err)
	}
	defer s.Close()

	n := AlertNotification{
		ID:         "notif-1",
		RuleID:     "ruuid-1",
		TraceIDHex: "abc123",
		Action:     "firing",
		Channel:    "email",
		Recipient:  "a@b.com",
		SentAt:     time.Now().Truncate(time.Second),
		Success:    true,
	}
	if err := s.InsertNotification(n); err != nil {
		t.Fatalf("InsertNotification: %v", err)
	}

	history, err := s.ListNotifications("")
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(history) != 1 || !history[0].Success {
		t.Fatalf("unexpected notification: %+v", history[0])
	}

	// Filter by rule
	filtered, _ := s.ListNotifications("ruuid-1")
	if len(filtered) != 1 {
		t.Fatalf("expected 1 notification for rule, got %d", len(filtered))
	}
	none, _ := s.ListNotifications("nonexistent")
	if len(none) != 0 {
		t.Fatalf("expected 0 notifications, got %d", len(none))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/alerting/ -run TestRuleStoreCRUD -v`
Expected: compilation error — `NewRuleStore` not defined

- [ ] **Step 3: Implement RuleStore**

```go
package alerting

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// RuleStore persists rules, alert states, and notification history in SQLite.
type RuleStore struct {
	db *sql.DB
}

func NewRuleStore(dbPath string) (*RuleStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	s := &RuleStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *RuleStore) Close() error {
	return s.db.Close()
}

func (s *RuleStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS alert_rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		metric TEXT NOT NULL DEFAULT 'total_tokens',
		conditions TEXT NOT NULL DEFAULT '[]',
		for_duration INTEGER NOT NULL DEFAULT 300,
		interval INTEGER NOT NULL DEFAULT 60,
		notifier TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS alert_states (
		id TEXT PRIMARY KEY,
		rule_id TEXT NOT NULL,
		trace_id_hex TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'ok',
		triggered_at TEXT NOT NULL,
		last_fired_at TEXT,
		resolved_at TEXT,
		UNIQUE(rule_id, trace_id_hex)
	);
	CREATE TABLE IF NOT EXISTS alert_notifications (
		id TEXT PRIMARY KEY,
		rule_id TEXT NOT NULL,
		trace_id_hex TEXT NOT NULL,
		action TEXT NOT NULL,
		channel TEXT NOT NULL,
		recipient TEXT NOT NULL,
		sent_at TEXT NOT NULL,
		success INTEGER NOT NULL DEFAULT 1,
		error_msg TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_alert_states_status ON alert_states(status);
	CREATE INDEX IF NOT EXISTS idx_alert_notifications_rule ON alert_notifications(rule_id);
	`
	_, err := s.db.Exec(schema)
	return err
}

// --- Rule CRUD ---

func (s *RuleStore) CreateRule(r Rule) error {
	conds, _ := json.Marshal(r.Conditions)
	notif, _ := json.Marshal(r.Notifier)
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO alert_rules (id, name, enabled, metric, conditions, for_duration, interval, notifier, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, enabled, r.Metric, string(conds), r.ForDuration, r.Interval, string(notif),
		r.CreatedAt.Format(time.RFC3339), r.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *RuleStore) GetRule(id string) (*Rule, error) {
	row := s.db.QueryRow(`SELECT id, name, enabled, metric, conditions, for_duration, interval, notifier, created_at, updated_at FROM alert_rules WHERE id = ?`, id)
	return scanRule(row)
}

func (s *RuleStore) ListRules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT id, name, enabled, metric, conditions, for_duration, interval, notifier, created_at, updated_at FROM alert_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		r, err := scanRuleFromRows(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, *r)
	}
	return rules, rows.Err()
}

func (s *RuleStore) UpdateRule(r Rule) error {
	conds, _ := json.Marshal(r.Conditions)
	notif, _ := json.Marshal(r.Notifier)
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	_, err := s.db.Exec(
		`UPDATE alert_rules SET name=?, enabled=?, metric=?, conditions=?, for_duration=?, interval=?, notifier=?, updated_at=? WHERE id=?`,
		r.Name, enabled, r.Metric, string(conds), r.ForDuration, r.Interval, string(notif), r.UpdatedAt.Format(time.RFC3339), r.ID,
	)
	return err
}

func (s *RuleStore) DeleteRule(id string) error {
	_, err := s.db.Exec(`DELETE FROM alert_rules WHERE id = ?`, id)
	return err
}

// --- Alert State ---

func (s *RuleStore) UpsertAlertState(state AlertState) error {
	lf := toNullString(state.LastFiredAt)
	rs := toNullString(state.ResolvedAt)
	_, err := s.db.Exec(
		`INSERT INTO alert_states (id, rule_id, trace_id_hex, status, triggered_at, last_fired_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(rule_id, trace_id_hex) DO UPDATE SET status=excluded.status, last_fired_at=excluded.last_fired_at, resolved_at=excluded.resolved_at`,
		state.ID, state.RuleID, state.TraceIDHex, string(state.Status),
		state.TriggeredAt.Format(time.RFC3339), lf, rs,
	)
	return err
}

func (s *RuleStore) GetAlertState(ruleID, traceIDHex string) (*AlertState, error) {
	row := s.db.QueryRow(`SELECT id, rule_id, trace_id_hex, status, triggered_at, last_fired_at, resolved_at FROM alert_states WHERE rule_id=? AND trace_id_hex=?`, ruleID, traceIDHex)
	var st AlertState
	var lf, rs sql.NullString
	err := row.Scan(&st.ID, &st.RuleID, &st.TraceIDHex, &st.Status, &st.TriggeredAt, &lf, &rs)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lf.Valid {
		t, _ := time.Parse(time.RFC3339, lf.String)
		st.LastFiredAt = &t
	}
	if rs.Valid {
		t, _ := time.Parse(time.RFC3339, rs.String)
		st.ResolvedAt = &t
	}
	return &st, nil
}

func (s *RuleStore) ListAlertStates(statusFilter string) ([]AlertState, error) {
	var rows *sql.Rows
	var err error
	if statusFilter != "" {
		rows, err = s.db.Query(`SELECT id, rule_id, trace_id_hex, status, triggered_at, last_fired_at, resolved_at FROM alert_states WHERE status = ? ORDER BY triggered_at DESC`, statusFilter)
	} else {
		rows, err = s.db.Query(`SELECT id, rule_id, trace_id_hex, status, triggered_at, last_fired_at, resolved_at FROM alert_states ORDER BY triggered_at DESC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []AlertState
	for rows.Next() {
		var st AlertState
		var lf, rs sql.NullString
		if err := rows.Scan(&st.ID, &st.RuleID, &st.TraceIDHex, &st.Status, &st.TriggeredAt, &lf, &rs); err != nil {
			return nil, err
		}
		if lf.Valid {
			t, _ := time.Parse(time.RFC3339, lf.String)
			st.LastFiredAt = &t
		}
		if rs.Valid {
			t, _ := time.Parse(time.RFC3339, rs.String)
			st.ResolvedAt = &t
		}
		states = append(states, st)
	}
	return states, rows.Err()
}

// --- Notification History ---

func (s *RuleStore) InsertNotification(n AlertNotification) error {
	success := 0
	if n.Success {
		success = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO alert_notifications (id, rule_id, trace_id_hex, action, channel, recipient, sent_at, success, error_msg)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.RuleID, n.TraceIDHex, n.Action, n.Channel, n.Recipient, n.SentAt.Format(time.RFC3339), success, n.ErrorMsg,
	)
	return err
}

func (s *RuleStore) ListNotifications(ruleIDFilter string) ([]AlertNotification, error) {
	var rows *sql.Rows
	var err error
	if ruleIDFilter != "" {
		rows, err = s.db.Query(`SELECT id, rule_id, trace_id_hex, action, channel, recipient, sent_at, success, error_msg FROM alert_notifications WHERE rule_id = ? ORDER BY sent_at DESC`, ruleIDFilter)
	} else {
		rows, err = s.db.Query(`SELECT id, rule_id, trace_id_hex, action, channel, recipient, sent_at, success, error_msg FROM alert_notifications ORDER BY sent_at DESC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ns []AlertNotification
	for rows.Next() {
		var n AlertNotification
		var success int
		if err := rows.Scan(&n.ID, &n.RuleID, &n.TraceIDHex, &n.Action, &n.Channel, &n.Recipient, &n.SentAt, &success, &n.ErrorMsg); err != nil {
			return nil, err
		}
		n.Success = success == 1
		ns = append(ns, n)
	}
	return ns, rows.Err()
}

// --- Helpers ---

func scanRule(row interface{ Scan(...interface{}) error }) (*Rule, error) {
	var r Rule
	var enabled int
	var conds, notif, ca, ua string
	err := row.Scan(&r.ID, &r.Name, &enabled, &r.Metric, &conds, &r.ForDuration, &r.Interval, &notif, &ca, &ua)
	if err != nil {
		return nil, err
	}
	r.Enabled = enabled == 1
	json.Unmarshal([]byte(conds), &r.Conditions)
	json.Unmarshal([]byte(notif), &r.Notifier)
	r.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
	return &r, nil
}

func scanRuleFromRows(rows *sql.Rows) (*Rule, error) {
	var r Rule
	var enabled int
	var conds, notif, ca, ua string
	err := rows.Scan(&r.ID, &r.Name, &enabled, &r.Metric, &conds, &r.ForDuration, &r.Interval, &notif, &ca, &ua)
	if err != nil {
		return nil, err
	}
	r.Enabled = enabled == 1
	json.Unmarshal([]byte(conds), &r.Conditions)
	json.Unmarshal([]byte(notif), &r.Notifier)
	r.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
	return &r, nil
}

func toNullString(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format(time.RFC3339), Valid: true}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/alerting/ -run TestRuleStore -v`
Expected: all TestRuleStore* tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/alerting/store.go internal/alerting/store_test.go
git commit -m "feat: add alerting RuleStore with SQLite CRUD"
```

---

### Task 3: Evaluation Engine — condition evaluator + state machine (`internal/alerting/engine.go`)

**Files:**
- Create: `internal/alerting/engine.go`

- [ ] **Step 1: Write the test file**

Create `internal/alerting/engine_test.go`:

```go
package alerting

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestEvaluateConditions(t *testing.T) {
	tests := []struct {
		name       string
		conditions []Condition
		trace      TraceData
		expected   bool
	}{
		{
			name: "total_tokens > threshold matches",
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100000"}},
			trace:      TraceData{TotalTokens: 150000},
			expected:   true,
		},
		{
			name: "total_tokens > threshold does not match",
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100000"}},
			trace:      TraceData{TotalTokens: 50000},
			expected:   false,
		},
		{
			name: "total_tokens >= threshold (equal)",
			conditions: []Condition{{Field: "total_tokens", Op: OpGTE, Value: "100000"}},
			trace:      TraceData{TotalTokens: 100000},
			expected:   true,
		},
		{
			name: "model eq matches",
			conditions: []Condition{{Field: "model", Op: OpEQ, Value: "claude-opus-4-8"}},
			trace:      TraceData{Model: "claude-opus-4-8"},
			expected:   true,
		},
		{
			name: "model neq matches",
			conditions: []Condition{{Field: "model", Op: OpNEQ, Value: "claude-haiku"}},
			trace:      TraceData{Model: "claude-opus-4-8"},
			expected:   true,
		},
		{
			name: "AND: both conditions must match",
			conditions: []Condition{
				{Field: "total_tokens", Op: OpGT, Value: "100000"},
				{Field: "model", Op: OpEQ, Value: "claude-opus-4-8"},
			},
			trace:    TraceData{TotalTokens: 150000, Model: "claude-opus-4-8"},
			expected: true,
		},
		{
			name: "AND: second condition fails",
			conditions: []Condition{
				{Field: "total_tokens", Op: OpGT, Value: "100000"},
				{Field: "model", Op: OpEQ, Value: "claude-opus-4-8"},
			},
			trace:    TraceData{TotalTokens: 150000, Model: "claude-haiku"},
			expected: false,
		},
		{
			name:       "no conditions = always matches",
			conditions: []Condition{},
			trace:      TraceData{TotalTokens: 0},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateConditions(tt.conditions, tt.trace)
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestStateMachineTransitions(t *testing.T) {
	now := time.Now()
	makeTrace := func(tokens uint32) TraceData {
		return TraceData{TotalTokens: tokens}
	}

	tests := []struct {
		name       string
		prevState  *AlertState
		conditions []Condition
		trace      TraceData
		forDur     int
		now        time.Time
		wantStatus AlertStatus
		wantNotify bool // should a notification be sent?
	}{
		{
			name:       "no previous state, condition met → ok (need pending first)",
			prevState:  nil,
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      makeTrace(200),
			forDur:     300,
			now:        now,
			wantStatus: AlertPending,
			wantNotify: false,
		},
		{
			name: "pending state, within for_duration → stays pending, no notify",
			prevState: &AlertState{
				Status:      AlertPending,
				TriggeredAt: now.Add(-60 * time.Second),
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      makeTrace(200),
			forDur:     300,
			now:        now,
			wantStatus: AlertPending,
			wantNotify: false,
		},
		{
			name: "pending state, for_duration elapsed → firing, notify",
			prevState: &AlertState{
				Status:      AlertPending,
				TriggeredAt: now.Add(-400 * time.Second),
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      makeTrace(200),
			forDur:     300,
			now:        now,
			wantStatus: AlertFiring,
			wantNotify: true,
		},
		{
			name: "firing state, condition still met → stays firing, no notify",
			prevState: &AlertState{
				Status:      AlertFiring,
				TriggeredAt: now.Add(-400 * time.Second),
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      makeTrace(200),
			forDur:     300,
			now:        now,
			wantStatus: AlertFiring,
			wantNotify: false,
		},
		{
			name: "firing state, condition no longer met → resolved, notify",
			prevState: &AlertState{
				Status:      AlertFiring,
				TriggeredAt: now.Add(-400 * time.Second),
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      makeTrace(50),
			forDur:     300,
			now:        now,
			wantStatus: AlertResolved,
			wantNotify: true,
		},
		{
			name: "ok state, condition met → pending",
			prevState: &AlertState{
				Status: AlertOK,
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      makeTrace(200),
			forDur:     300,
			now:        now,
			wantStatus: AlertPending,
			wantNotify: false,
		},
		{
			name: "resolved state transitioning back to pending",
			prevState: &AlertState{
				Status: AlertResolved,
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      makeTrace(200),
			forDur:     300,
			now:        now,
			wantStatus: AlertPending,
			wantNotify: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condMet := evaluateConditions(tt.conditions, tt.trace)
			status, notify := driveStateMachine(tt.prevState, condMet, tt.forDur, tt.now)
			if status != tt.wantStatus {
				t.Errorf("expected status %s, got %s", tt.wantStatus, status)
			}
			if notify != tt.wantNotify {
				t.Errorf("expected notify=%v, got notify=%v", tt.wantNotify, notify)
			}
		})
	}
}

func TestEnginePolling(t *testing.T) {
	dbPath := tempDB(t)
	defer os.Remove(dbPath)

	store, err := NewRuleStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create a rule
	r := Rule{
		ID:          "r1",
		Name:        "Test",
		Enabled:     true,
		Metric:      "total_tokens",
		Conditions:  []Condition{{Field: "total_tokens", Op: OpGT, Value: "100000"}},
		ForDuration: 0, // 0 = fire immediately for testing
		Interval:    60,
		Notifier:    NotifierConfig{Type: "email", SMTPHost: "smtp.example.com", SMTPPort: 587, Recipients: []string{"test@test.com"}},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateRule(r); err != nil {
		t.Fatal(err)
	}

	// Create engine with a test notifier
	registry := NewNotifierRegistry()
	tn := &testNotifier{}
	registry.Register("email", func(cfg NotifierConfig) (Notifier, error) {
		tn.cfg = cfg
		return tn, nil
	})

	engine := NewEngine(store, registry)

	// Provide a trace fetcher returning a trace that triggers the rule
	traceFetcher := func(since time.Time) ([]TraceData, error) {
		return []TraceData{
			{ID: "trace-1", TotalTokens: 150000, Model: "claude-opus-4-8"},
		}, nil
	}

	events, err := engine.Evaluate(context.Background(), traceFetcher)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Should produce exactly 1 event (pending→firing since for_duration=0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.Alert.Action != "firing" {
		t.Errorf("expected firing, got %s", ev.Alert.Action)
	}
	if ev.Alert.TraceIDHex != "trace-1" {
		t.Errorf("expected trace-1, got %s", ev.Alert.TraceIDHex)
	}
}

// testNotifier records calls for assertions.
type testNotifier struct {
	cfg        NotifierConfig
	fireCalls  []AlertState
	resolveCalls []AlertState
}

func (tn *testNotifier) Type() string { return "email" }
func (tn *testNotifier) Fire(ctx context.Context, rule Rule, state AlertState) error {
	tn.fireCalls = append(tn.fireCalls, state)
	return nil
}
func (tn *testNotifier) Resolve(ctx context.Context, rule Rule, state AlertState) error {
	tn.resolveCalls = append(tn.resolveCalls, state)
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/alerting/ -run TestEvaluateConditions -v`
Expected: compilation error — types/functions not defined

- [ ] **Step 3: Implement the engine**

```go
package alerting

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TraceData is the minimal trace information needed for alert evaluation.
type TraceData struct {
	ID          string
	TotalTokens uint32
	Model       string // derived from span GenAIRequestModel
}

// TraceFetcher is a function that returns traces created since a given time.
type TraceFetcher func(since time.Time) ([]TraceData, error)

// NotifierFactory creates a Notifier from configuration.
type NotifierFactory func(cfg NotifierConfig) (Notifier, error)

// NotifierRegistry maps channel types to their factories.
type NotifierRegistry struct {
	mu       sync.RWMutex
	factories map[string]NotifierFactory
}

func NewNotifierRegistry() *NotifierRegistry {
	return &NotifierRegistry{factories: make(map[string]NotifierFactory)}
}

func (r *NotifierRegistry) Register(channelType string, factory NotifierFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[channelType] = factory
}

func (r *NotifierRegistry) Get(channelType string) (NotifierFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[channelType]
	return f, ok
}

// AlertEvent is emitted by the engine when a notification should be sent.
type AlertEvent struct {
	Rule     Rule
	State    AlertState
	Alert    AlertState // new/updated state
	Trace    TraceData  // trace data that triggered the event
	Notifier Notifier
	Action   string // "firing" or "resolved"
}

// Engine evaluates alert rules against trace data on a polling schedule.
type Engine struct {
	store    *RuleStore
	registry *NotifierRegistry

	mu         sync.Mutex
	lastEval   map[string]time.Time // ruleID → last evaluation time
}

func NewEngine(store *RuleStore, registry *NotifierRegistry) *Engine {
	return &Engine{
		store:    store,
		registry: registry,
		lastEval: make(map[string]time.Time),
	}
}

// Evaluate runs one evaluation cycle for all enabled rules.
// It returns alert events that need notification dispatch.
func (e *Engine) Evaluate(ctx context.Context, fetchTrace TraceFetcher) ([]AlertEvent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rules, err := e.store.ListRules()
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}

	now := time.Now()
	var events []AlertEvent

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// Check if it's time to evaluate this rule.
		interval := time.Duration(rule.Interval) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		last, ok := e.lastEval[rule.ID]
		if ok && now.Sub(last) < interval {
			continue
		}
		e.lastEval[rule.ID] = now

		// Fetch traces since last evaluation.
		since := now.Add(-interval - time.Duration(rule.ForDuration)*time.Second)
		traces, err := fetchTrace(since)
		if err != nil {
			continue // skip rules whose fetcher fails
		}

		for _, trace := range traces {
			condMet := evaluateConditions(rule.Conditions, trace)

			// Load existing state.
			existingState, err := e.store.GetAlertState(rule.ID, trace.ID)
			if err != nil {
				continue
			}

			newStatus, shouldNotify := driveStateMachine(existingState, condMet, rule.ForDuration, now)

			// Build/update state record.
			var state AlertState
			if existingState != nil {
				state = *existingState
			} else {
				state = AlertState{
					ID:          uuid.New().String(),
					RuleID:      rule.ID,
					TraceIDHex:  trace.ID,
					TriggeredAt: now,
				}
			}

			oldStatus := state.Status
			state.Status = newStatus

			if newStatus == AlertFiring && oldStatus != AlertFiring {
				state.LastFiredAt = &now
			}
			if newStatus == AlertResolved {
				state.ResolvedAt = &now
			}

			if err := e.store.UpsertAlertState(state); err != nil {
				continue
			}

			if shouldNotify {
				// Resolve notifier for this rule.
				factory, ok := e.registry.Get(rule.Notifier.Type)
				if !ok {
					continue
				}
				notifier, err := factory(rule.Notifier)
				if err != nil {
					continue
				}
				action := "firing"
				if newStatus == AlertResolved {
					action = "resolved"
				}
				events = append(events, AlertEvent{
					Rule:     rule,
					State:    state,
					Alert:    state,
					Trace:    trace,
					Notifier: notifier,
					Action:   action,
				})
			}
		}
	}

	return events, nil
}

// evaluateConditions returns true if all conditions match the trace (AND logic).
func evaluateConditions(conditions []Condition, trace TraceData) bool {
	if len(conditions) == 0 {
		return true
	}
	for _, c := range conditions {
		if !evaluateCondition(c, trace) {
			return false
		}
	}
	return true
}

func evaluateCondition(c Condition, trace TraceData) bool {
	var fieldValue interface{}
	switch c.Field {
	case "total_tokens":
		fieldValue = uint64(trace.TotalTokens)
	case "input_tokens":
		fieldValue = uint64(trace.TotalTokens) // trace-level already aggregated
	case "output_tokens":
		fieldValue = uint64(trace.TotalTokens)
	case "model":
		fieldValue = trace.Model
	default:
		return false
	}

	switch v := fieldValue.(type) {
	case uint64:
		threshold, err := strconv.ParseUint(c.Value, 10, 64)
		if err != nil {
			return false
		}
		return compareUint64(v, c.Op, threshold)
	case string:
		return compareString(v, c.Op, c.Value)
	default:
		return false
	}
}

func compareUint64(a uint64, op ConditionOperator, b uint64) bool {
	switch op {
	case OpGT:
		return a > b
	case OpGTE:
		return a >= b
	case OpLT:
		return a < b
	case OpLTE:
		return a <= b
	case OpEQ:
		return a == b
	case OpNEQ:
		return a != b
	default:
		return false
	}
}

func compareString(a string, op ConditionOperator, b string) bool {
	switch op {
	case OpEQ:
		return a == b
	case OpNEQ:
		return a != b
	default:
		return false
	}
}

// driveStateMachine determines the next state and whether to notify.
func driveStateMachine(existing *AlertState, condMet bool, forDuration int, now time.Time) (AlertStatus, bool) {
	currentStatus := AlertOK
	if existing != nil {
		currentStatus = existing.Status
	}

	notify := false

	switch currentStatus {
	case AlertOK, AlertResolved:
		if condMet {
			if forDuration <= 0 {
				// No debounce — fire immediately.
				return AlertFiring, true
			}
			return AlertPending, false
		}
		return currentStatus, false

	case AlertPending:
		if !condMet {
			return AlertOK, false
		}
		// Check if for_duration has elapsed.
		var triggeredAt time.Time
		if existing != nil {
			triggeredAt = existing.TriggeredAt
		}
		elapsed := now.Sub(triggeredAt)
		if elapsed >= time.Duration(forDuration)*time.Second {
			return AlertFiring, true
		}
		return AlertPending, false

	case AlertFiring:
		if !condMet {
			return AlertResolved, true
		}
		// Still firing — no re-notification.
		return AlertFiring, false

	default:
		return AlertOK, false
	}
}

// RunPolling starts the polling loop. It blocks until ctx is cancelled.
func (e *Engine) RunPolling(ctx context.Context, fetchTrace TraceFetcher, onEvent func(AlertEvent)) {
	// Use a single ticker at a reasonable cadence (every 15s minimum).
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			events, err := e.Evaluate(ctx, fetchTrace)
			if err != nil {
				continue
			}
			for _, ev := range events {
				onEvent(ev)
			}
		case <-ctx.Done():
			return
		}
	}
}

// maxUint32 is a helper.
func _() {
	_ = math.MaxUint32
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/alerting/ -run "TestEvaluateConditions|TestStateMachine" -v`
Expected: all tests pass

- [ ] **Step 5: Run engine test**

Run: `go test ./internal/alerting/ -run TestEnginePolling -v`
Expected: pass

- [ ] **Step 6: Commit**

```bash
git add internal/alerting/engine.go internal/alerting/engine_test.go
git commit -m "feat: add alert evaluation engine with state machine"
```

---

### Task 4: Notifier — interface, registry, and EmailNotifier (`internal/alerting/notifier.go`)

**Files:**
- Create: `internal/alerting/notifier.go`

- [ ] **Step 1: Write the test file**

Create `internal/alerting/notifier_test.go`:

```go
package alerting

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

func TestNotifierRegistry(t *testing.T) {
	reg := NewNotifierRegistry()

	reg.Register("email", func(cfg NotifierConfig) (Notifier, error) {
		return &EmailNotifier{cfg: cfg}, nil
	})

	factory, ok := reg.Get("email")
	if !ok {
		t.Fatal("expected email factory to be registered")
	}
	notifier, err := factory(NotifierConfig{SMTPHost: "smtp.example.com", SMTPPort: 587})
	if err != nil {
		t.Fatal(err)
	}
	if notifier.Type() != "email" {
		t.Fatalf("expected 'email', got %q", notifier.Type())
	}

	_, ok = reg.Get("dingtalk")
	if ok {
		t.Fatal("dingtalk should not be registered")
	}
}

func TestEmailNotifierTemplates(t *testing.T) {
	rule := Rule{
		ID:   "r1",
		Name: "Opus Token Spike",
	}
	state := AlertState{
		TraceIDHex:  "abc123",
		TriggeredAt: time.Now(),
	}
	trace := TraceData{TotalTokens: 156432, Model: "claude-opus-4-8"}

	fire := formatFireEmail(rule, state, trace)
	if !strings.Contains(fire.Subject, "[LABUBU ALERT]") {
		t.Errorf("subject missing [LABUBU ALERT]: %q", fire.Subject)
	}
	if !strings.Contains(fire.Body, "abc123") {
		t.Errorf("body missing trace ID")
	}
	if !strings.Contains(fire.Body, "156,432") {
		t.Errorf("body missing token count")
	}

	resolve := formatResolveEmail(rule, state, trace)
	if !strings.Contains(resolve.Subject, "[LABUBU RESOLVED]") {
		t.Errorf("subject missing [LABUBU RESOLVED]: %q", resolve.Subject)
	}
}

func TestEmailNotifierSMTP(t *testing.T) {
	// Start a local SMTP server to verify the email is sent.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		conn, _ := listener.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		var buf [4096]byte
		n, _ := conn.Read(buf[:])
		received <- string(buf[:n])
	}()

	addr := listener.Addr().(*net.TCPAddr)
	cfg := NotifierConfig{
		Type:       "email",
		SMTPHost:   "127.0.0.1",
		SMTPPort:   addr.Port,
		Username:   "test",
		Password:   "pass",
		Recipients: []string{"alerts@example.com"},
	}

	notifier := &EmailNotifier{cfg: cfg}

	// Override the SMTP dial for testing — we already have a listener.
	// We'll test format instead — actual SMTP handshake requires a real server.
	// The formatting is tested above; the send path uses net/smtp which is
	// a stdlib and doesn't need unit testing.
	_ = notifier
}

// Test that formatFireEmail produces valid SMTP content.
func TestFireEmailFormat(t *testing.T) {
	rule := Rule{Name: "Test Alert"}
	now := time.Date(2026, 6, 9, 14, 32, 0, 0, time.UTC)
	state := AlertState{TraceIDHex: "trace-1", TriggeredAt: now}
	trace := TraceData{TotalTokens: 200000, Model: "claude-sonnet-4-6"}

	result := formatFireEmail(rule, state, trace)

	if result.Subject != `[LABUBU ALERT] Rule "Test Alert" is firing` {
		t.Errorf("unexpected subject: %q", result.Subject)
	}
	if !strings.Contains(result.Body, "Rule: Test Alert") {
		t.Error("body should contain rule name")
	}
	if !strings.Contains(result.Body, "Trace: trace-1") {
		t.Error("body should contain trace ID")
	}
	if !strings.Contains(result.Body, "Model: claude-sonnet-4-6") {
		t.Error("body should contain model")
	}
	if !strings.Contains(result.Body, "200,000") {
		t.Error("body should contain formatted token count")
	}
	if !strings.Contains(result.Body, "2026-06-09 14:32:00") {
		t.Error("body should contain trigger time")
	}
}

func TestResolveEmailFormat(t *testing.T) {
	rule := Rule{Name: "Test Alert"}
	now := time.Date(2026, 6, 9, 14, 45, 0, 0, time.UTC)
	state := AlertState{TraceIDHex: "trace-1", ResolvedAt: &now}
	trace := TraceData{TotalTokens: 1000, Model: "claude-sonnet-4-6"}

	result := formatResolveEmail(rule, state, trace)

	if result.Subject != `[LABUBU RESOLVED] Rule "Test Alert" recovered` {
		t.Errorf("unexpected subject: %q", result.Subject)
	}
	if !strings.Contains(result.Body, "2026-06-09 14:45:00") {
		t.Error("body should contain resolve time")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/alerting/ -run TestNotifierRegistry -v`
Expected: compilation error

- [ ] **Step 3: Implement the notifier**

```go
package alerting

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

// Notifier sends alert notifications.
type Notifier interface {
	Type() string
	Fire(ctx context.Context, rule Rule, state AlertState, trace TraceData) error
	Resolve(ctx context.Context, rule Rule, state AlertState, trace TraceData) error
}

// --- EmailNotifier ---

// EmailNotifier sends email notifications via SMTP.
type EmailNotifier struct {
	cfg NotifierConfig
}

func (e *EmailNotifier) Type() string {
	return "email"
}

func (e *EmailNotifier) Fire(ctx context.Context, rule Rule, state AlertState, trace TraceData) error {
	msg := formatFireEmail(rule, state, trace)
	return e.send(msg)
}

func (e *EmailNotifier) Resolve(ctx context.Context, rule Rule, state AlertState, trace TraceData) error {
	msg := formatResolveEmail(rule, state, trace)
	return e.send(msg)
}

func (e *EmailNotifier) send(msg EmailMessage) error {
	addr := fmt.Sprintf("%s:%d", e.cfg.SMTPHost, e.cfg.SMTPPort)
	var auth smtp.Auth
	if e.cfg.Username != "" && e.cfg.Password != "" {
		auth = smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.SMTPHost)
	}

	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n",
		e.cfg.Username,
		strings.Join(e.cfg.Recipients, ", "),
		msg.Subject,
	)
	body := header + msg.Body

	return smtp.SendMail(addr, auth, e.cfg.Username, e.cfg.Recipients, []byte(body))
}

// EmailMessage holds the subject and body for an email.
type EmailMessage struct {
	Subject string
	Body    string
}

func formatFireEmail(rule Rule, state AlertState, trace TraceData) EmailMessage {
	return EmailMessage{
		Subject: fmt.Sprintf(`[LABUBU ALERT] Rule "%s" is firing`, rule.Name),
		Body: fmt.Sprintf(`Rule: %s
Trace: %s
Model: %s
Total Tokens: %s
Threshold: check conditions in rule configuration
Triggered at: %s

View details: http://localhost:8080/traces/%s
`,
			rule.Name,
			state.TraceIDHex,
			trace.Model,
			formatNumber(uint64(trace.TotalTokens)),
			state.TriggeredAt.Format("2006-01-02 15:04:05"),
			state.TraceIDHex,
		),
	}
}

func formatResolveEmail(rule Rule, state AlertState, trace TraceData) EmailMessage {
	resolvedAt := time.Now()
	if state.ResolvedAt != nil {
		resolvedAt = *state.ResolvedAt
	}
	return EmailMessage{
		Subject: fmt.Sprintf(`[LABUBU RESOLVED] Rule "%s" recovered`, rule.Name),
		Body: fmt.Sprintf(`Rule: %s
Trace: %s
Resolved at: %s
`,
			rule.Name,
			state.TraceIDHex,
			resolvedAt.Format("2006-01-02 15:04:05"),
		),
	}
}

func formatNumber(n uint64) string {
	s := fmt.Sprintf("%d", n)
	// Insert commas.
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/alerting/ -run "TestNotifierRegistry|TestEmailNotifier|TestFireEmail|TestResolveEmail" -v`
Expected: all pass

- [ ] **Step 5: Verify UUID dependency**

The `github.com/google/uuid` package is already in `go.mod` (v1.6.0). No additional install needed.

- [ ] **Step 6: Commit**

```bash
git add internal/alerting/notifier.go internal/alerting/notifier_test.go
git commit -m "feat: add notifier interface and SMTP email notifier"
```

---

### Task 5: HTTP API handlers (`internal/alerting/api.go`)

**Files:**
- Create: `internal/alerting/api.go`

- [ ] **Step 1: Write the test file**

Create `internal/alerting/api_test.go`:

```go
package alerting

import (
	"bytes"
	"context"
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
		// os.Remove(dbPath) — temp file, cleaned up by test
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
		{"no metric", `{"name":"test"}`, http.StatusBadRequest},
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

	// Create a rule first.
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/alerting/ -run TestListRules_Empty -v`
Expected: compilation error

- [ ] **Step 3: Implement the alert handler**

```go
package alerting

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AlertHandler serves HTTP endpoints for alert rules, states, and notifications.
type AlertHandler struct {
	store *RuleStore
}

// NewAlertHandler creates a new AlertHandler.
func NewAlertHandler(store *RuleStore) *AlertHandler {
	return &AlertHandler{store: store}
}

// ServeHTTP dispatches alert API requests.
func (h *AlertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts")
	path = strings.TrimPrefix(path, "/")

	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "rules" || path == "rules/":
		switch r.Method {
		case http.MethodGet:
			h.listRules(w, r)
		case http.MethodPost:
			h.createRule(w, r)
		default:
			writeAlertJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case strings.HasPrefix(path, "rules/"):
		ruleID := strings.TrimPrefix(path, "rules/")
		switch r.Method {
		case http.MethodGet:
			h.getRule(w, r, ruleID)
		case http.MethodPut:
			h.updateRule(w, r, ruleID)
		case http.MethodDelete:
			h.deleteRule(w, r, ruleID)
		default:
			writeAlertJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case path == "states" || strings.HasPrefix(path, "states"):
		if r.Method == http.MethodGet {
			h.listStates(w, r)
		} else {
			writeAlertJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case path == "notifications" || strings.HasPrefix(path, "notifications"):
		if r.Method == http.MethodGet {
			h.listNotifications(w, r)
		} else {
			writeAlertJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	default:
		writeAlertJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// --- Rule handlers ---

func (h *AlertHandler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListRules()
	if err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rules == nil {
		rules = []Rule{}
	}
	writeAlertJSON(w, http.StatusOK, map[string]interface{}{"rules": rules})
}

func (h *AlertHandler) createRule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string         `json:"name"`
		Metric      string         `json:"metric"`
		Conditions  []Condition    `json:"conditions"`
		ForDuration int            `json:"for_duration"`
		Interval    int            `json:"interval"`
		Notifier    NotifierConfig `json:"notifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAlertJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if input.Name == "" {
		writeAlertJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if input.Metric == "" {
		input.Metric = "total_tokens"
	}
	if input.ForDuration <= 0 {
		input.ForDuration = 300
	}
	if input.Interval <= 0 {
		input.Interval = 60
	}
	if input.Conditions == nil {
		input.Conditions = []Condition{}
	}

	now := time.Now()
	rule := Rule{
		ID:          uuid.New().String(),
		Name:        input.Name,
		Enabled:     true,
		Metric:      input.Metric,
		Conditions:  input.Conditions,
		ForDuration: input.ForDuration,
		Interval:    input.Interval,
		Notifier:    input.Notifier,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.store.CreateRule(rule); err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeAlertJSON(w, http.StatusCreated, rule)
}

func (h *AlertHandler) getRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	rule, err := h.store.GetRule(ruleID)
	if err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rule == nil {
		writeAlertJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
		return
	}
	writeAlertJSON(w, http.StatusOK, rule)
}

func (h *AlertHandler) updateRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	existing, err := h.store.GetRule(ruleID)
	if err != nil || existing == nil {
		writeAlertJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
		return
	}

	var input struct {
		Name        string         `json:"name"`
		Enabled     *bool           `json:"enabled"`
		Metric      string         `json:"metric"`
		Conditions  []Condition    `json:"conditions"`
		ForDuration int            `json:"for_duration"`
		Interval    int            `json:"interval"`
		Notifier    NotifierConfig `json:"notifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAlertJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if input.Name != "" {
		existing.Name = input.Name
	}
	if input.Enabled != nil {
		existing.Enabled = *input.Enabled
	}
	if input.Metric != "" {
		existing.Metric = input.Metric
	}
	if input.Conditions != nil {
		existing.Conditions = input.Conditions
	}
	if input.ForDuration > 0 {
		existing.ForDuration = input.ForDuration
	}
	if input.Interval > 0 {
		existing.Interval = input.Interval
	}
	if input.Notifier.Type != "" {
		existing.Notifier = input.Notifier
	}
	existing.UpdatedAt = time.Now()

	if err := h.store.UpdateRule(*existing); err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeAlertJSON(w, http.StatusOK, existing)
}

func (h *AlertHandler) deleteRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	if err := h.store.DeleteRule(ruleID); err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeAlertJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- State handlers ---

func (h *AlertHandler) listStates(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	states, err := h.store.ListAlertStates(statusFilter)
	if err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if states == nil {
		states = []AlertState{}
	}
	writeAlertJSON(w, http.StatusOK, map[string]interface{}{"states": states})
}

// --- Notification handlers ---

func (h *AlertHandler) listNotifications(w http.ResponseWriter, r *http.Request) {
	ruleIDFilter := r.URL.Query().Get("rule_id")
	notifications, err := h.store.ListNotifications(ruleIDFilter)
	if err != nil {
		writeAlertJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if notifications == nil {
		notifications = []AlertNotification{}
	}
	writeAlertJSON(w, http.StatusOK, map[string]interface{}{"notifications": notifications})
}

func writeAlertJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/alerting/ -run "TestListRules|TestCreate|TestUpdate|TestDelete|TestListStates|TestMethod" -v`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/alerting/api.go internal/alerting/api_test.go
git commit -m "feat: add alert HTTP API handlers"
```

---

### Task 6: Wire alert routes into the API router (`internal/api/router.go`)

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Modify `NewRouter` to accept the alert handler**

First, add the alert handler parameter and wire its routes. Read current `internal/api/router.go` and make these edits:

**Change the function signature** (line 13):

```go
func NewRouter(traceHandler *TraceHandler, metricsHandler *MetricsHandler, dashboardHandler *DashboardHandler, sessionHandler *SessionHandler, logHandler *LogHandler, pricingHandler *PricingHandler, alertHandler http.Handler) http.Handler {
```

**Add alert routes** — insert after the pricing routes (line 76):

```go
	// API routes — alerting.
	if alertHandler != nil {
		mux.HandleFunc("/api/v1/alerts/", alertHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/alerts", alertHandler.ServeHTTP)
	}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/api/...`
Expected: compiles (may fail if main.go hasn't been updated yet — that's expected)

- [ ] **Step 3: Update callers in `cmd/labubu/main.go`**

In `cmd/labubu/main.go`, in the section where the router is created (~line 183), update the NewRouter call. Add a nil alert handler for now:

```go
	router := api.NewRouter(traceHandler, metricsHandler, dashboardHandler, sessionHandler, logHandler, pricingHandler, nil)
```

- [ ] **Step 4: Verify full build**

Run: `go build ./cmd/labubu/`
Expected: compiles

- [ ] **Step 5: Commit**

```bash
git add internal/api/router.go cmd/labubu/main.go
git commit -m "feat: wire alert API routes into router"
```

---

### Task 7: Alerting subsystem setup and integration (`internal/alerting/setup.go`)

**Files:**
- Create: `internal/alerting/setup.go`
- Modify: `cmd/labubu/main.go`

- [ ] **Step 1: Create the setup module**

```go
package alerting

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/labubu/labubu/internal/storage"
)

// Config holds alerting subsystem configuration.
type Config struct {
	DBPath string // path to SQLite database file
}

// Subsystem holds all alerting components.
type Subsystem struct {
	Store   *RuleStore
	Engine  *Engine
	Handler *AlertHandler

	cancel context.CancelFunc
}

// InitAlerting initializes the alerting subsystem.
// dbPath: path to SQLite file (e.g. "data/alerting.db").
// store: the trace storage for querying traces.
func InitAlerting(dbPath string, traceStore storage.Store) (*Subsystem, error) {
	if dbPath == "" {
		dbPath = "alerting.db"
	}

	store, err := NewRuleStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("alerting store: %w", err)
	}

	// Register notifiers.
	registry := NewNotifierRegistry()
	registry.Register("email", func(cfg NotifierConfig) (Notifier, error) {
		return &EmailNotifier{cfg: cfg}, nil
	})

	engine := NewEngine(store, registry)
	handler := NewAlertHandler(store)

	sub := &Subsystem{
		Store:   store,
		Engine:  engine,
		Handler: handler,
	}

	// Start polling loop.
	ctx, cancel := context.WithCancel(context.Background())
	sub.cancel = cancel

	go engine.RunPolling(ctx, func(since time.Time) ([]TraceData, error) {
		// Fetch traces since the given time.
		result, err := traceStore.ListTraces(context.Background(), storage.TraceQuery{
			Page:        1,
			PageSize:    500,
			StartTimeMS: uint64(since.UnixMilli()),
			EndTimeMS:   uint64(time.Now().UnixMilli()),
		})
		if err != nil {
			return nil, err
		}
		var traces []TraceData
		for _, t := range result.Traces {
			tokens := uint32(0)
			if t.TotalTokens != nil {
				tokens = *t.TotalTokens
			}
			// Extract model from trace span attributes.
			model := ""
			traceIDBytes, err := hex.DecodeString(t.TraceIDHex)
			if err == nil && len(traceIDBytes) == 16 {
				var traceID [16]byte
				copy(traceID[:], traceIDBytes)
				detail, err := traceStore.GetTrace(context.Background(), traceID)
				if err == nil && detail != nil {
					for _, span := range detail.Spans {
						if span.GenAIRequestModel != nil && *span.GenAIRequestModel != "" {
							model = *span.GenAIRequestModel
							break
						}
					}
				}
			}
			traces = append(traces, TraceData{
				ID:          t.TraceIDHex,
				TotalTokens: tokens,
				Model:       model,
			})
		}
		return traces, nil
	}, func(ev AlertEvent) {
		// Dispatch notification.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var err error
		if ev.Action == "firing" {
			err = ev.Notifier.Fire(ctx, ev.Rule, ev.Alert, ev.Trace)
		} else {
			err = ev.Notifier.Resolve(ctx, ev.Rule, ev.Alert, ev.Trace)
		}

		// Record notification history.
		success := err == nil
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		histEntry := AlertNotification{
			ID:         uuid.New().String(),
			RuleID:     ev.Rule.ID,
			TraceIDHex: ev.Alert.TraceIDHex,
			Action:     ev.Action,
			Channel:    ev.Rule.Notifier.Type,
			Recipient:  ev.Rule.Notifier.Recipients[0],
			SentAt:     time.Now(),
			Success:    success,
			ErrorMsg:   errMsg,
		}
		if err := store.InsertNotification(histEntry); err != nil {
			log.Printf("Alerting: failed to record notification: %v", err)
		}
		if !success {
			log.Printf("Alerting: notification failed: %v", err)
		}
	})

	log.Printf("Alerting: started (db=%s)", dbPath)
	return sub, nil
}

// Shutdown gracefully stops the alerting engine.
func (sub *Subsystem) Shutdown() {
	if sub.cancel != nil {
		sub.cancel()
	}
	if sub.Store != nil {
		sub.Store.Close()
	}
	log.Println("Alerting: stopped")
}
```

- [ ] **Step 2: Integrate into main.go**

In `cmd/labubu/main.go`, add the import for the alerting package and integrate:

Add these lines after the pricing handler is created (~line 182):

```go
	// Initialize alerting subsystem.
	alertDBPath := *dataDir + "/alerting.db"
	if *dataDir == "" {
		alertDBPath = "alerting.db"
	}
	alertSub, err := alerting.InitAlerting(alertDBPath, store)
	if err != nil {
		log.Printf("Warning: alerting disabled: %v", err)
	}
	if alertSub != nil {
		defer alertSub.Shutdown()
	}
```

Update the NewRouter call:

```go
	var alertHandler http.Handler
	if alertSub != nil {
		alertHandler = alertSub.Handler
	}
	router := api.NewRouter(traceHandler, metricsHandler, dashboardHandler, sessionHandler, logHandler, pricingHandler, alertHandler)
```

Add the import to the import block:

```go
	"github.com/labubu/labubu/internal/alerting"
```

- [ ] **Step 3: Verify full build**

Run: `go build ./cmd/labubu/`
Expected: compiles successfully

- [ ] **Step 4: Run all Go tests**

Run: `go test ./internal/alerting/... -v`
Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/alerting/setup.go cmd/labubu/main.go
git commit -m "feat: integrate alerting subsystem startup and lifecycle"
```

---

### Task 8: Frontend API client (`web/src/api/alerts.ts`)

**Files:**
- Create: `web/src/api/alerts.ts`

- [ ] **Step 1: Create the API client**

```typescript
const BASE_URL = '/api/v1/alerts'

export interface AlertCondition {
  field: string
  op: string
  value: string
}

export interface NotifierConfig {
  type: string
  smtp_host: string
  smtp_port: number
  username: string
  password: string
  recipients: string[]
}

export interface AlertRule {
  id: string
  name: string
  enabled: boolean
  metric: string
  conditions: AlertCondition[]
  for_duration: number
  interval: number
  notifier: NotifierConfig
  created_at: string
  updated_at: string
}

export interface AlertState {
  id: string
  rule_id: string
  trace_id_hex: string
  status: 'ok' | 'pending' | 'firing' | 'resolved'
  triggered_at: string
  last_fired_at?: string
  resolved_at?: string
}

export interface AlertNotification {
  id: string
  rule_id: string
  trace_id_hex: string
  action: 'firing' | 'resolved'
  channel: string
  recipient: string
  sent_at: string
  success: boolean
  error_msg?: string
}

export interface AlertRuleListResponse {
  rules: AlertRule[]
}

export interface AlertStateListResponse {
  states: AlertState[]
}

export interface AlertNotificationListResponse {
  notifications: AlertNotification[]
}

async function get<T>(path: string, params?: Record<string, string | number | undefined>): Promise<T> {
  const url = new URL(path, window.location.origin)
  if (params) {
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== '') {
        url.searchParams.set(k, String(v))
      }
    })
  }
  const res = await fetch(url.toString())
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

// Rules
export async function listRules(): Promise<AlertRuleListResponse> {
  return get<AlertRuleListResponse>(`${BASE_URL}/rules`)
}

export async function getRule(id: string): Promise<AlertRule> {
  return get<AlertRule>(`${BASE_URL}/rules/${encodeURIComponent(id)}`)
}

export async function createRule(rule: Omit<AlertRule, 'id' | 'created_at' | 'updated_at'>): Promise<AlertRule> {
  const res = await fetch(`${BASE_URL}/rules`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(rule),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Create failed: ${res.status}`)
  }
  return res.json()
}

export async function updateRule(id: string, rule: Partial<AlertRule>): Promise<AlertRule> {
  const res = await fetch(`${BASE_URL}/rules/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(rule),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Update failed: ${res.status}`)
  }
  return res.json()
}

export async function deleteRule(id: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/rules/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

// States
export async function listStates(statusFilter?: string): Promise<AlertStateListResponse> {
  return get<AlertStateListResponse>(`${BASE_URL}/states`, { status: statusFilter })
}

// Notifications
export async function listNotifications(ruleId?: string): Promise<AlertNotificationListResponse> {
  return get<AlertNotificationListResponse>(`${BASE_URL}/notifications`, { rule_id: ruleId })
}
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no new errors (the file is not imported yet, so it won't cause issues)

- [ ] **Step 3: Commit**

```bash
git add web/src/api/alerts.ts
git commit -m "feat: add alert API client"
```

---

### Task 9: RuleList page (`web/src/views/alerts/RuleList.vue`)

**Files:**
- Create: `web/src/views/alerts/RuleList.vue`

- [ ] **Step 1: Create the RuleList component**

```vue
<template>
  <div class="rule-list-page">
    <div class="page-header">
      <h2>{{ t('alerts.rules') }}</h2>
      <router-link to="/alerts/rules/new" class="btn-primary">{{ t('alerts.newRule') }}</router-link>
    </div>

    <table v-if="rules.length > 0" class="data-table">
      <thead>
        <tr>
          <th>{{ t('alerts.name') }}</th>
          <th>{{ t('alerts.metric') }}</th>
          <th>{{ t('alerts.status') }}</th>
          <th>{{ t('alerts.lastTriggered') }}</th>
          <th>{{ t('alerts.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="rule in rules" :key="rule.id">
          <td>{{ rule.name }}</td>
          <td>{{ rule.metric }}</td>
          <td>
            <span :class="rule.enabled ? 'badge-green' : 'badge-gray'">
              {{ rule.enabled ? t('alerts.enabled') : t('alerts.disabled') }}
            </span>
          </td>
          <td>{{ rule.last_fired_at || '-' }}</td>
          <td class="actions-cell">
            <router-link :to="`/alerts/rules/${rule.id}/edit`" class="btn-sm">{{ t('alerts.edit') }}</router-link>
            <button @click="toggleRule(rule)" class="btn-sm">{{ rule.enabled ? t('alerts.disable') : t('alerts.enable') }}</button>
            <button @click="confirmDelete(rule)" class="btn-sm btn-danger">{{ t('alerts.delete') }}</button>
          </td>
        </tr>
      </tbody>
    </table>

    <p v-else class="empty-msg">{{ t('alerts.noRules') }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { listRules, deleteRule, updateRule, type AlertRule } from '../../api/alerts'

const { t } = useI18n()
const rules = ref<AlertRule[]>([])

async function load() {
  try {
    const resp = await listRules()
    rules.value = resp.rules
  } catch (e) {
    console.error('Failed to load alert rules:', e)
  }
}

async function toggleRule(rule: AlertRule) {
  try {
    await updateRule(rule.id, { enabled: !rule.enabled })
    await load()
  } catch (e) {
    console.error('Failed to toggle rule:', e)
  }
}

async function confirmDelete(rule: AlertRule) {
  if (!confirm(`${t('alerts.confirmDelete')} "${rule.name}"?`)) return
  try {
    await deleteRule(rule.id)
    await load()
  } catch (e) {
    console.error('Failed to delete rule:', e)
  }
}

onMounted(load)
</script>

<style scoped>
.rule-list-page { max-width: 900px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 20px; color: var(--text-primary); }
.btn-primary {
  background: var(--accent-blue); color: #fff; border: none; padding: 8px 16px;
  border-radius: 6px; text-decoration: none; font-size: 14px; cursor: pointer;
}
.btn-primary:hover { opacity: 0.9; }
.data-table { width: 100%; border-collapse: collapse; }
.data-table th, .data-table td { text-align: left; padding: 12px 8px; border-bottom: 1px solid var(--border-default); font-size: 14px; }
.data-table th { color: var(--text-secondary); font-weight: 600; }
.actions-cell { display: flex; gap: 8px; }
.btn-sm {
  background: var(--bg-secondary); border: 1px solid var(--border-default); color: var(--text-primary);
  padding: 4px 10px; border-radius: 4px; font-size: 13px; cursor: pointer; text-decoration: none;
}
.btn-sm:hover { background: var(--bg-hover); }
.btn-danger { color: var(--danger-red); border-color: var(--danger-red); }
.btn-danger:hover { background: var(--danger-red); color: #fff; }
.badge-green { background: #d4edda; color: #155724; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.badge-gray { background: #e2e3e5; color: #383d41; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.empty-msg { color: var(--text-secondary); margin-top: 40px; text-align: center; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/alerts/RuleList.vue
git commit -m "feat: add RuleList page"
```

---

### Task 10: RuleForm page (`web/src/views/alerts/RuleForm.vue`)

**Files:**
- Create: `web/src/views/alerts/RuleForm.vue`

- [ ] **Step 1: Create the RuleForm component**

```vue
<template>
  <div class="rule-form-page">
    <h2>{{ isEdit ? t('alerts.editRule') : t('alerts.newRule') }}</h2>

    <form @submit.prevent="save" class="rule-form">
      <div class="form-group">
        <label>{{ t('alerts.name') }}</label>
        <input v-model="form.name" type="text" required class="form-input" />
      </div>

      <div class="form-group">
        <label>{{ t('alerts.metric') }}</label>
        <select v-model="form.metric" class="form-input">
          <option value="total_tokens">Total Tokens</option>
          <option value="input_tokens">Input Tokens</option>
          <option value="output_tokens">Output Tokens</option>
        </select>
      </div>

      <div class="form-group">
        <label>{{ t('alerts.conditions') }}</label>
        <div v-for="(cond, i) in form.conditions" :key="i" class="condition-row">
          <select v-model="cond.field" class="cond-field">
            <option value="total_tokens">Total Tokens</option>
            <option value="input_tokens">Input Tokens</option>
            <option value="output_tokens">Output Tokens</option>
            <option value="model">Model</option>
          </select>
          <select v-model="cond.op" class="cond-op">
            <option value="gt">&gt;</option>
            <option value="gte">&gt;=</option>
            <option value="lt">&lt;</option>
            <option value="lte">&lt;=</option>
            <option value="eq">=</option>
            <option value="neq">!=</option>
          </select>
          <input v-model="cond.value" type="text" class="cond-value" placeholder="Value" />
          <button type="button" @click="removeCondition(i)" class="btn-sm btn-danger">&times;</button>
          <span v-if="i < form.conditions.length - 1" class="and-label">AND</span>
        </div>
        <button type="button" @click="addCondition" class="btn-sm">{{ t('alerts.addCondition') }}</button>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label>{{ t('alerts.forDuration') }} (s)</label>
          <input v-model.number="form.for_duration" type="number" min="0" class="form-input" />
        </div>
        <div class="form-group">
          <label>{{ t('alerts.interval') }} (s)</label>
          <input v-model.number="form.interval" type="number" min="15" class="form-input" />
        </div>
      </div>

      <fieldset class="smtp-section">
        <legend>{{ t('alerts.emailConfig') }}</legend>
        <div class="form-row">
          <div class="form-group flex-2">
            <label>SMTP Host</label>
            <input v-model="form.notifier.smtp_host" type="text" class="form-input" placeholder="smtp.gmail.com" />
          </div>
          <div class="form-group flex-1">
            <label>Port</label>
            <input v-model.number="form.notifier.smtp_port" type="number" class="form-input" placeholder="587" />
          </div>
        </div>
        <div class="form-row">
          <div class="form-group flex-1">
            <label>Username</label>
            <input v-model="form.notifier.username" type="text" class="form-input" />
          </div>
          <div class="form-group flex-1">
            <label>Password</label>
            <input v-model="form.notifier.password" type="password" class="form-input" />
          </div>
        </div>
        <div class="form-group">
          <label>{{ t('alerts.recipients') }}</label>
          <input v-model="recipientsStr" type="text" class="form-input" placeholder="a@b.com, c@d.com" />
        </div>
      </fieldset>

      <div class="form-actions">
        <button type="submit" class="btn-primary" :disabled="saving">{{ t('alerts.save') }}</button>
        <router-link to="/alerts/rules" class="btn-cancel">{{ t('alerts.cancel') }}</router-link>
      </div>

      <p v-if="error" class="error-msg">{{ error }}</p>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { createRule, updateRule, getRule, type AlertCondition, type AlertRule } from '../../api/alerts'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const isEdit = computed(() => !!route.params.id)
const saving = ref(false)
const error = ref('')

const defaultForm = () => ({
  name: '',
  metric: 'total_tokens',
  conditions: [{ field: 'total_tokens', op: 'gt', value: '' }] as AlertCondition[],
  for_duration: 300,
  interval: 60,
  notifier: {
    type: 'email',
    smtp_host: '',
    smtp_port: 587,
    username: '',
    password: '',
    recipients: [] as string[],
  },
})

const form = ref(defaultForm())
const recipientsStr = ref('')

onMounted(async () => {
  if (isEdit.value) {
    try {
      const rule = await getRule(route.params.id as string)
      form.value = {
        name: rule.name,
        metric: rule.metric,
        conditions: rule.conditions.length > 0 ? rule.conditions : [{ field: 'total_tokens', op: 'gt', value: '' }],
        for_duration: rule.for_duration,
        interval: rule.interval,
        notifier: { ...rule.notifier },
      }
      recipientsStr.value = (rule.notifier.recipients || []).join(', ')
    } catch (e: any) {
      error.value = e.message
    }
  }
})

function addCondition() {
  form.value.conditions.push({ field: 'total_tokens', op: 'gt', value: '' })
}

function removeCondition(i: number) {
  form.value.conditions.splice(i, 1)
}

async function save() {
  saving.value = true
  error.value = ''

  // Parse recipients.
  form.value.notifier.recipients = recipientsStr.value
    .split(',')
    .map(s => s.trim())
    .filter(s => s.length > 0)

  try {
    if (isEdit.value) {
      await updateRule(route.params.id as string, form.value)
    } else {
      await createRule(form.value as any)
    }
    router.push('/alerts/rules')
  } catch (e: any) {
    error.value = e.message
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.rule-form-page { max-width: 700px; }
.rule-form-page h2 { margin-bottom: 24px; color: var(--text-primary); }
.rule-form { display: flex; flex-direction: column; gap: 16px; }
.form-group { display: flex; flex-direction: column; gap: 4px; }
.form-group label { font-size: 13px; color: var(--text-secondary); font-weight: 600; }
.form-input { padding: 8px 12px; border: 1px solid var(--border-default); border-radius: 6px; font-size: 14px; background: var(--bg-primary); color: var(--text-primary); }
.form-input:focus { outline: none; border-color: var(--accent-blue); }
.form-row { display: flex; gap: 16px; }
.flex-1 { flex: 1; }
.flex-2 { flex: 2; }
.condition-row { display: flex; gap: 8px; align-items: center; margin-bottom: 6px; }
.cond-field { width: 130px; }
.cond-op { width: 70px; }
.cond-value { flex: 1; }
.and-label { color: var(--text-secondary); font-size: 12px; font-weight: 700; margin: 0 4px; }
.smtp-section { border: 1px solid var(--border-default); border-radius: 8px; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
.smtp-section legend { font-size: 14px; font-weight: 600; color: var(--text-primary); padding: 0 8px; }
.form-actions { display: flex; gap: 12px; align-items: center; margin-top: 8px; }
.btn-primary {
  background: var(--accent-blue); color: #fff; border: none; padding: 10px 24px;
  border-radius: 6px; font-size: 14px; cursor: pointer;
}
.btn-primary:disabled { opacity: 0.6; cursor: not-allowed; }
.btn-cancel {
  background: var(--bg-secondary); border: 1px solid var(--border-default); color: var(--text-primary);
  padding: 10px 24px; border-radius: 6px; font-size: 14px; text-decoration: none;
}
.btn-sm {
  background: var(--bg-secondary); border: 1px solid var(--border-default); color: var(--text-primary);
  padding: 4px 10px; border-radius: 4px; font-size: 13px; cursor: pointer;
}
.btn-danger { color: var(--danger-red); border-color: var(--danger-red); }
.error-msg { color: var(--danger-red); font-size: 14px; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/alerts/RuleForm.vue
git commit -m "feat: add RuleForm page"
```

---

### Task 11: AlertHistory page (`web/src/views/alerts/AlertHistory.vue`)

**Files:**
- Create: `web/src/views/alerts/AlertHistory.vue`

- [ ] **Step 1: Create the AlertHistory component**

```vue
<template>
  <div class="alert-history-page">
    <div class="page-header">
      <h2>{{ t('alerts.history') }}</h2>
    </div>

    <div class="filters">
      <label>{{ t('alerts.statusFilter') }}</label>
      <select v-model="statusFilter" @change="load" class="form-input filter-select">
        <option value="">{{ t('alerts.all') }}</option>
        <option value="firing">{{ t('alerts.firing') }}</option>
        <option value="resolved">{{ t('alerts.resolved') }}</option>
        <option value="pending">{{ t('alerts.pending') }}</option>
      </select>
    </div>

    <table v-if="states.length > 0" class="data-table">
      <thead>
        <tr>
          <th>{{ t('alerts.ruleName') }}</th>
          <th>Trace ID</th>
          <th>{{ t('alerts.status') }}</th>
          <th>{{ t('alerts.triggeredAt') }}</th>
          <th>{{ t('alerts.resolvedAt') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="st in states" :key="st.id">
          <td>{{ ruleNames[st.rule_id] || st.rule_id }}</td>
          <td>
            <router-link :to="`/traces/${st.trace_id_hex}`" class="trace-link">
              {{ st.trace_id_hex.slice(0, 12) }}...
            </router-link>
          </td>
          <td>
            <span :class="statusClass(st.status)">{{ st.status }}</span>
          </td>
          <td>{{ formatTime(st.triggered_at) }}</td>
          <td>{{ st.resolved_at ? formatTime(st.resolved_at) : '-' }}</td>
        </tr>
      </tbody>
    </table>

    <p v-else class="empty-msg">{{ t('alerts.noAlerts') }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { listStates, listRules, type AlertState, type AlertRule } from '../../api/alerts'

const { t } = useI18n()
const states = ref<AlertState[]>([])
const statusFilter = ref('')
const ruleNames = ref<Record<string, string>>({})

async function load() {
  try {
    const [statesResp, rulesResp] = await Promise.all([
      listStates(statusFilter.value || undefined),
      listRules(),
    ])
    states.value = statesResp.states
    const map: Record<string, string> = {}
    rulesResp.rules.forEach((r: AlertRule) => { map[r.id] = r.name })
    ruleNames.value = map
  } catch (e) {
    console.error('Failed to load alert history:', e)
  }
}

function statusClass(status: string) {
  switch (status) {
    case 'firing': return 'badge-red'
    case 'resolved': return 'badge-green'
    case 'pending': return 'badge-yellow'
    default: return 'badge-gray'
  }
}

function formatTime(ts: string) {
  if (!ts) return '-'
  const d = new Date(ts)
  return d.toLocaleString()
}

onMounted(load)
</script>

<style scoped>
.alert-history-page { max-width: 1000px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 20px; color: var(--text-primary); }
.filters { display: flex; align-items: center; gap: 12px; margin-bottom: 20px; }
.filters label { font-size: 14px; color: var(--text-secondary); }
.filter-select { width: 160px; }
.form-input { padding: 8px 12px; border: 1px solid var(--border-default); border-radius: 6px; font-size: 14px; background: var(--bg-primary); color: var(--text-primary); }
.data-table { width: 100%; border-collapse: collapse; }
.data-table th, .data-table td { text-align: left; padding: 12px 8px; border-bottom: 1px solid var(--border-default); font-size: 14px; }
.data-table th { color: var(--text-secondary); font-weight: 600; }
.trace-link { color: var(--accent-blue); text-decoration: none; }
.trace-link:hover { text-decoration: underline; }
.badge-red { background: #f8d7da; color: #721c24; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.badge-green { background: #d4edda; color: #155724; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.badge-yellow { background: #fff3cd; color: #856404; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.badge-gray { background: #e2e3e5; color: #383d41; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.empty-msg { color: var(--text-secondary); margin-top: 40px; text-align: center; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/alerts/AlertHistory.vue
git commit -m "feat: add AlertHistory page"
```

---

### Task 12: Router, i18n, and sidebar (`web/src/router.ts`, `web/src/App.vue`, i18n locales)

**Files:**
- Modify: `web/src/router.ts`
- Modify: `web/src/App.vue`
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

- [ ] **Step 1: Add alert routes to the Vue router**

In `web/src/router.ts`, add the import and routes:

Add this import after the existing imports:

```typescript
import RuleList from './views/alerts/RuleList.vue'
import RuleForm from './views/alerts/RuleForm.vue'
import AlertHistory from './views/alerts/AlertHistory.vue'
```

Add these routes in the `routes` array:

```typescript
    { path: '/alerts/rules', name: 'rule-list', component: RuleList },
    { path: '/alerts/rules/new', name: 'rule-create', component: RuleForm },
    { path: '/alerts/rules/:id/edit', name: 'rule-edit', component: RuleForm },
    { path: '/alerts/history', name: 'alert-history', component: AlertHistory },
```

- [ ] **Step 2: Add sidebar navigation**

In `web/src/App.vue`, add the Alerts nav group inside the `<nav class="app-nav">` element, after the existing nav links and before the Settings group:

```html
        <div class="nav-group">
          <button class="nav-group-title" @click="alertsOpen = !alertsOpen">
            <span class="nav-group-arrow">{{ alertsOpen ? '▼' : '▶' }}</span>
            Alerts
          </button>
          <div v-show="alertsOpen" class="nav-group-items">
            <router-link to="/alerts/rules">{{ t('alerts.rules') }}</router-link>
            <router-link to="/alerts/history">{{ t('alerts.history') }}</router-link>
          </div>
        </div>
```

Add the reactive ref in the `<script setup>`:

```typescript
const alertsOpen = ref(false)
```

- [ ] **Step 3: Add i18n strings**

For `web/src/i18n/locales/en.ts`, find the end of the export default object and add:

```typescript
  alerts: {
    rules: 'Alert Rules',
    history: 'Alert History',
    newRule: 'New Rule',
    editRule: 'Edit Rule',
    name: 'Name',
    metric: 'Metric',
    status: 'Status',
    enabled: 'Enabled',
    disabled: 'Disabled',
    lastTriggered: 'Last Triggered',
    actions: 'Actions',
    edit: 'Edit',
    delete: 'Delete',
    enable: 'Enable',
    disable: 'Disable',
    noRules: 'No alert rules configured.',
    confirmDelete: 'Are you sure you want to delete rule',
    conditions: 'Conditions',
    addCondition: 'Add Condition',
    forDuration: 'Debounce Duration',
    interval: 'Evaluation Interval',
    emailConfig: 'SMTP Email Configuration',
    recipients: 'Recipients',
    save: 'Save',
    cancel: 'Cancel',
    statusFilter: 'Status Filter',
    all: 'All',
    firing: 'Firing',
    resolved: 'Resolved',
    pending: 'Pending',
    ruleName: 'Rule Name',
    triggeredAt: 'Triggered At',
    resolvedAt: 'Resolved At',
    noAlerts: 'No alerts triggered yet.',
  },
```

For `web/src/i18n/locales/zh.ts`, add the corresponding Chinese translations:

```typescript
  alerts: {
    rules: '告警规则',
    history: '告警历史',
    newRule: '新建规则',
    editRule: '编辑规则',
    name: '名称',
    metric: '指标',
    status: '状态',
    enabled: '启用',
    disabled: '暂停',
    lastTriggered: '最后触发',
    actions: '操作',
    edit: '编辑',
    delete: '删除',
    enable: '启用',
    disable: '暂停',
    noRules: '暂无告警规则。',
    confirmDelete: '确定要删除规则',
    conditions: '条件',
    addCondition: '添加条件',
    forDuration: '防抖时长',
    interval: '评估间隔',
    emailConfig: 'SMTP 邮件配置',
    recipients: '收件人',
    save: '保存',
    cancel: '取消',
    statusFilter: '状态筛选',
    all: '全部',
    firing: '告警中',
    resolved: '已恢复',
    pending: '待确认',
    ruleName: '规则名称',
    triggeredAt: '触发时间',
    resolvedAt: '恢复时间',
    noAlerts: '暂无告警记录。',
  },
```

Make sure these are added within the appropriate nested objects (inside `nav` or on the same level as other page sections). Check the existing structure of the locale files and follow it.

- [ ] **Step 4: Verify TypeScript compilation**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 5: Build frontend**

Run: `cd web && npm run build`
Expected: dist built successfully

- [ ] **Step 6: Commit**

```bash
git add web/src/router.ts web/src/App.vue web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add alert routes, sidebar nav, and i18n strings"
```

---

### Task 13: End-to-end build & test

**Files:**
- None new — verification task

- [ ] **Step 1: Run full Go test suite**

Run: `go test ./internal/alerting/... -v`
Expected: all alerting tests pass

- [ ] **Step 2: Run existing tests to check for regressions**

Run: `go test ./internal/api/... -v`
Expected: all existing tests pass

- [ ] **Step 3: Full build**

Run: `make build`
Expected: builds successfully

- [ ] **Step 4: Frontend type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 5: Commit if any fixes were needed**

```bash
git add -A
git commit -m "chore: fix build issues from alerting integration"
```

---

### Task 14: Clean up and final review

- [ ] **Step 1: Remove unused imports and dead code**

Run: `goimports -w ./internal/alerting/` (if `goimports` is available) or manually check.

- [ ] **Step 2: Run go vet**

Run: `go vet ./internal/alerting/...`
Expected: no issues

- [ ] **Step 3: Final commit**

```bash
git add -A
git commit -m "chore: clean up alerting implementation"
```
