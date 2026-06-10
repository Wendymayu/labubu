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
	Type       string   `json:"type"` // "email" (MVP)
	SMTPHost   string   `json:"smtp_host"`
	SMTPPort   int      `json:"smtp_port"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	Recipients []string `json:"recipients"`
}

// Rule represents an alert rule.
type Rule struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Enabled     bool           `json:"enabled"`
	Metric      string         `json:"metric"`       // "total_tokens" for MVP
	Conditions  []Condition    `json:"conditions"`
	ForDuration int            `json:"for_duration"` // seconds, default 300
	Interval    int            `json:"interval"`     // seconds, default 60
	Notifier    NotifierConfig `json:"notifier"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
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
	ID         string    `json:"id"`
	RuleID     string    `json:"rule_id"`
	TraceIDHex string    `json:"trace_id_hex"`
	Action     string    `json:"action"`  // "firing" or "resolved"
	Channel    string    `json:"channel"` // "email"
	Recipient  string    `json:"recipient"`
	SentAt     time.Time `json:"sent_at"`
	Success    bool      `json:"success"`
	ErrorMsg   string    `json:"error_msg,omitempty"`
}
