//go:build !cgo && nosqlite

package storage

import (
	"math"
	"testing"
)

func TestComputeAgentStatsEmpty(t *testing.T) {
	result := computeAgentStats(nil, nil)
	if result == nil {
		t.Fatalf("expected non-nil result for empty input, got nil")
	}
	// With zero traces, TraceSuccessRate = 0/0 = NaN.
	if !math.IsNaN(result.TraceSuccessRate) {
		t.Errorf("trace_success_rate: got %v, want NaN", result.TraceSuccessRate)
	}
	if result.TotalToolCalls != 0 {
		t.Errorf("total_tool_calls: got %v, want 0", result.TotalToolCalls)
	}
	if result.MaxLoopDepth != 0 {
		t.Errorf("max_loop_depth: got %v, want 0", result.MaxLoopDepth)
	}
	if len(result.ToolUsage) != 0 {
		t.Errorf("tool_usage: got %d items, want 0", len(result.ToolUsage))
	}
}

func TestComputeAgentStatsTraceSuccessRate(t *testing.T) {
	traces := []Trace{
		{StatusCode: 1}, // ok
		{StatusCode: 1}, // ok
		{StatusCode: 2}, // error
		{StatusCode: 1}, // ok
	}
	spans := []Span{}
	result := computeAgentStats(traces, spans)
	if result.TraceSuccessRate != 0.75 {
		t.Errorf("trace_success_rate: got %v, want 0.75", result.TraceSuccessRate)
	}
}

func TestComputeAgentStatsToolSuccessRate(t *testing.T) {
	traces := []Trace{{StatusCode: 1}}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 200, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 300, StatusCode: 2, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
	}
	result := computeAgentStats(traces, spans)
	if result.TotalToolCalls != 3 {
		t.Errorf("total_tool_calls: got %v, want 3", result.TotalToolCalls)
	}
	if result.SuccessfulToolCalls != 2 {
		t.Errorf("successful_tool_calls: got %v, want 2", result.SuccessfulToolCalls)
	}
	if result.AvgToolSuccessRate != 2.0/3.0 {
		t.Errorf("avg_tool_success_rate: got %v, want 0.667", result.AvgToolSuccessRate)
	}
}

func TestComputeAgentStatsLoopDetection(t *testing.T) {
	traces := []Trace{{StatusCode: 1}}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 200, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 300, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 400, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
	}
	result := computeAgentStats(traces, spans)
	if result.MaxLoopDepth != 3 {
		t.Errorf("max_loop_depth: got %v, want 3", result.MaxLoopDepth)
	}
	for _, item := range result.ToolUsage {
		if item.ToolName == "file_read" && item.MaxLoop != 3 {
			t.Errorf("file_read max_loop: got %v, want 3", item.MaxLoop)
		}
	}
}

func TestComputeAgentStatsRetryDetection(t *testing.T) {
	traces := []Trace{{StatusCode: 1}}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 2, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
		{StartTimeMS: 200, StatusCode: 2, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
		{StartTimeMS: 300, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
	}
	result := computeAgentStats(traces, spans)
	for _, item := range result.ToolUsage {
		if item.ToolName == "web_search" {
			if item.AvgRetries != 2.0 {
				t.Errorf("web_search avg_retries: got %v, want 2.0", item.AvgRetries)
			}
			if item.CallCount != 3 {
				t.Errorf("web_search call_count: got %v, want 3", item.CallCount)
			}
		}
	}
}

func TestComputeAgentStatsInsights(t *testing.T) {
	traces := []Trace{
		{StatusCode: 2},
		{StatusCode: 2},
		{StatusCode: 2},
	}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 200, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 300, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
	}
	result := computeAgentStats(traces, spans)
	if len(result.Insights) < 1 {
		t.Fatalf("expected at least 1 insight, got %d", len(result.Insights))
	}
	hasLoopInsight := false
	for _, ins := range result.Insights {
		if ins == "file_read has max loop depth 3 — agent may be stuck in a retry loop" {
			hasLoopInsight = true
		}
	}
	if !hasLoopInsight {
		t.Errorf("expected loop insight for file_read, got: %v", result.Insights)
	}
}

func TestComputeAgentStatsNoGenAISpans(t *testing.T) {
	traces := []Trace{{StatusCode: 1}}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 1, Attributes: map[string]string{"service.name": "my-svc"}},
	}
	result := computeAgentStats(traces, spans)
	if result.TotalToolCalls != 0 {
		t.Errorf("total_tool_calls: got %v, want 0", result.TotalToolCalls)
	}
	if result.ToolUsage != nil && len(result.ToolUsage) != 0 {
		t.Errorf("tool_usage should be empty, got %d items", len(result.ToolUsage))
	}
}
