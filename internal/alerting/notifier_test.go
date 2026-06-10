package alerting

import (
	"strings"
	"testing"
	"time"
)

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
	if !strings.Contains(resolve.Body, "abc123") {
		t.Errorf("body missing trace ID in resolve")
	}
}

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

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0"},
		{100, "100"},
		{1000, "1,000"},
		{1000000, "1,000,000"},
		{156432, "156,432"},
		{42, "42"},
	}

	for _, tt := range tests {
		got := formatNumber(tt.n)
		if got != tt.want {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
