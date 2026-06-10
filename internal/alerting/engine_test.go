package alerting

import (
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
			name:       "total_tokens > threshold matches",
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100000"}},
			trace:      TraceData{TotalTokens: 150000},
			expected:   true,
		},
		{
			name:       "total_tokens > threshold does not match",
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100000"}},
			trace:      TraceData{TotalTokens: 50000},
			expected:   false,
		},
		{
			name:       "total_tokens >= threshold (equal)",
			conditions: []Condition{{Field: "total_tokens", Op: OpGTE, Value: "100000"}},
			trace:      TraceData{TotalTokens: 100000},
			expected:   true,
		},
		{
			name:       "model eq matches",
			conditions: []Condition{{Field: "model", Op: OpEQ, Value: "claude-opus-4-8"}},
			trace:      TraceData{Model: "claude-opus-4-8"},
			expected:   true,
		},
		{
			name:       "model neq matches",
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

	tests := []struct {
		name       string
		prevState  *AlertState
		conditions []Condition
		trace      TraceData
		forDur     int
		now        time.Time
		wantStatus AlertStatus
		wantNotify bool
	}{
		{
			name:       "no previous state, condition met -> pending",
			prevState:  nil,
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      TraceData{TotalTokens: 200},
			forDur:     300,
			now:        now,
			wantStatus: AlertPending,
			wantNotify: false,
		},
		{
			name: "pending state, within for_duration -> stays pending, no notify",
			prevState: &AlertState{
				Status:      AlertPending,
				TriggeredAt: now.Add(-60 * time.Second),
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      TraceData{TotalTokens: 200},
			forDur:     300,
			now:        now,
			wantStatus: AlertPending,
			wantNotify: false,
		},
		{
			name: "pending state, for_duration elapsed -> firing, notify",
			prevState: &AlertState{
				Status:      AlertPending,
				TriggeredAt: now.Add(-400 * time.Second),
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      TraceData{TotalTokens: 200},
			forDur:     300,
			now:        now,
			wantStatus: AlertFiring,
			wantNotify: true,
		},
		{
			name: "firing state, condition still met -> stays firing, no notify",
			prevState: &AlertState{
				Status:      AlertFiring,
				TriggeredAt: now.Add(-400 * time.Second),
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      TraceData{TotalTokens: 200},
			forDur:     300,
			now:        now,
			wantStatus: AlertFiring,
			wantNotify: false,
		},
		{
			name: "firing state, condition no longer met -> resolved, notify",
			prevState: &AlertState{
				Status:      AlertFiring,
				TriggeredAt: now.Add(-400 * time.Second),
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      TraceData{TotalTokens: 50},
			forDur:     300,
			now:        now,
			wantStatus: AlertResolved,
			wantNotify: true,
		},
		{
			name: "ok state, condition met -> pending",
			prevState: &AlertState{
				Status: AlertOK,
			},
			conditions: []Condition{{Field: "total_tokens", Op: OpGT, Value: "100"}},
			trace:      TraceData{TotalTokens: 200},
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
			trace:      TraceData{TotalTokens: 200},
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

	r := Rule{
		ID:          "r1",
		Name:        "Test",
		Enabled:     true,
		Metric:      "total_tokens",
		Conditions:  []Condition{{Field: "total_tokens", Op: OpGT, Value: "100000"}},
		ForDuration: 0,
		Interval:    60,
		Notifier:    NotifierConfig{Type: "email", SMTPHost: "smtp.example.com", SMTPPort: 587, Recipients: []string{"test@test.com"}},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateRule(r); err != nil {
		t.Fatal(err)
	}

	registry := NewNotifierRegistry()
	tn := &testNotifier{}
	registry.Register("email", func(cfg NotifierConfig) (Notifier, error) {
		tn.cfg = cfg
		return tn, nil
	})

	engine := NewEngine(store, registry)

	traceFetcher := func(since time.Time) ([]TraceData, error) {
		return []TraceData{
			{ID: "trace-1", TotalTokens: 150000, Model: "claude-opus-4-8"},
		}, nil
	}

	events, err := engine.Evaluate(traceFetcher)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.Action != "firing" {
		t.Errorf("expected firing, got %s", ev.Action)
	}
	if ev.Alert.TraceIDHex != "trace-1" {
		t.Errorf("expected trace-1, got %s", ev.Alert.TraceIDHex)
	}
}

type testNotifier struct {
	cfg          NotifierConfig
	fireCalls    []AlertState
	resolveCalls []AlertState
}

func (tn *testNotifier) Type() string { return "email" }
func (tn *testNotifier) Fire(rule Rule, state AlertState, trace TraceData) error {
	tn.fireCalls = append(tn.fireCalls, state)
	return nil
}
func (tn *testNotifier) Resolve(rule Rule, state AlertState, trace TraceData) error {
	tn.resolveCalls = append(tn.resolveCalls, state)
	return nil
}
