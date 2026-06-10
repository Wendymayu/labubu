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
		ID:     "ruuid-1",
		Name:   "Test Rule",
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
