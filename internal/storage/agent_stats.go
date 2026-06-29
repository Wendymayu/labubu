package storage

import (
	"fmt"
	"sort"
)

// computeAgentStats calculates agent behavior statistics from traces and spans.
//
// This is a pure function shared by all Store implementations (SQLite, chDB,
// memstore) so that GetSessionAgentStats produces identical results regardless
// of the active storage backend. It only reads Trace.StatusCode and
// Span.{StartTimeMS, StatusCode, Attributes} — callers need not populate
// other fields.
func computeAgentStats(sessionTraces []Trace, allSpans []Span) *AgentStats {
	// Trace success rate: ok traces / total traces.
	okCount := 0
	for _, t := range sessionTraces {
		if StatusCodeToString(t.StatusCode) == "OK" {
			okCount++
		}
	}
	traceSuccessRate := float64(okCount) / float64(len(sessionTraces))

	// Span-per-trace.
	totalSpans := len(allSpans)
	spanPerTrace := float64(0)
	if len(sessionTraces) > 0 {
		spanPerTrace = float64(totalSpans) / float64(len(sessionTraces))
	}

	// Identify tool spans and LLM spans.
	var toolSpans []Span
	for _, s := range allSpans {
		if s.Attributes["gen_ai.tool.name"] != "" {
			toolSpans = append(toolSpans, s)
		}
	}

	// Sort tool spans by StartTimeMS.
	sort.Slice(toolSpans, func(i, j int) bool {
		return toolSpans[i].StartTimeMS < toolSpans[j].StartTimeMS
	})

	// Total and successful tool calls.
	totalToolCalls := len(toolSpans)
	successfulToolCalls := 0
	for _, s := range toolSpans {
		if StatusCodeToString(s.StatusCode) == "OK" {
			successfulToolCalls++
		}
	}

	avgToolSuccessRate := float64(0)
	if totalToolCalls > 0 {
		avgToolSuccessRate = float64(successfulToolCalls) / float64(totalToolCalls)
	}

	// Per-tool statistics.
	type toolAgg struct {
		callCount    int
		successCount int
		spans        []Span
	}
	toolAggs := make(map[string]*toolAgg)
	for _, s := range toolSpans {
		name := s.Attributes["gen_ai.tool.name"]
		agg, ok := toolAggs[name]
		if !ok {
			agg = &toolAgg{}
			toolAggs[name] = agg
		}
		agg.callCount++
		if StatusCodeToString(s.StatusCode) == "OK" {
			agg.successCount++
		}
		agg.spans = append(agg.spans, s)
	}

	// Retry detection per tool: consecutive error spans followed by 1 ok span = 1 retry group.
	// Retry count = number of error spans in the group.
	// Also track max loop: consecutive same-tool-name spans in the overall time-ordered tool spans.
	totalRetries := 0
	for _, agg := range toolAggs {
		consecutiveErrors := 0
		for _, s := range agg.spans {
			if StatusCodeToString(s.StatusCode) == "ERROR" {
				consecutiveErrors++
			} else if StatusCodeToString(s.StatusCode) == "OK" && consecutiveErrors > 0 {
				totalRetries += consecutiveErrors
				consecutiveErrors = 0
			} else {
				consecutiveErrors = 0
			}
		}
	}

	avgRetries := float64(0)
	if len(toolAggs) > 0 {
		avgRetries = float64(totalRetries) / float64(len(toolAggs))
	}

	// Loop detection: max consecutive same tool_name spans in time-ordered tool spans.
	globalMaxLoop := 0
	if len(toolSpans) > 0 {
		currentLoop := 1
		for i := 1; i < len(toolSpans); i++ {
			if toolSpans[i].Attributes["gen_ai.tool.name"] == toolSpans[i-1].Attributes["gen_ai.tool.name"] {
				currentLoop++
			} else {
				if currentLoop > globalMaxLoop {
					globalMaxLoop = currentLoop
				}
				currentLoop = 1
			}
		}
		if currentLoop > globalMaxLoop {
			globalMaxLoop = currentLoop
		}
	}

	// Per-tool max loop.
	toolMaxLoops := make(map[string]int)
	if len(toolSpans) > 0 {
		currentName := toolSpans[0].Attributes["gen_ai.tool.name"]
		currentCount := 1
		for i := 1; i < len(toolSpans); i++ {
			name := toolSpans[i].Attributes["gen_ai.tool.name"]
			if name == currentName {
				currentCount++
			} else {
				if currentCount > toolMaxLoops[currentName] {
					toolMaxLoops[currentName] = currentCount
				}
				currentName = name
				currentCount = 1
			}
		}
		if currentCount > toolMaxLoops[currentName] {
			toolMaxLoops[currentName] = currentCount
		}
	}

	// Avg loop depth across all tool types.
	totalLoopDepth := 0
	for _, depth := range toolMaxLoops {
		totalLoopDepth += depth
	}
	avgLoopDepth := float64(0)
	if len(toolMaxLoops) > 0 {
		avgLoopDepth = float64(totalLoopDepth) / float64(len(toolMaxLoops))
	}

	// Per-tool retry count for avg_retries per tool.
	toolRetryCounts := make(map[string]int)
	for name, agg := range toolAggs {
		consecutiveErrors := 0
		for _, s := range agg.spans {
			if StatusCodeToString(s.StatusCode) == "ERROR" {
				consecutiveErrors++
			} else if StatusCodeToString(s.StatusCode) == "OK" && consecutiveErrors > 0 {
				toolRetryCounts[name] += consecutiveErrors
				consecutiveErrors = 0
			} else {
				consecutiveErrors = 0
			}
		}
	}

	// Avg retries across all tool types.
	sumRetries := 0
	for _, rc := range toolRetryCounts {
		sumRetries += rc
	}
	if len(toolRetryCounts) > 0 {
		avgRetries = float64(sumRetries) / float64(len(toolRetryCounts))
	}

	// Build tool usage items, sorted by call count descending.
	toolUsage := make([]ToolUsageItem, 0, len(toolAggs))
	for name, agg := range toolAggs {
		successRate := float64(0)
		if agg.callCount > 0 {
			successRate = float64(agg.successCount) / float64(agg.callCount)
		}
		toolUsage = append(toolUsage, ToolUsageItem{
			ToolName:    name,
			CallCount:   agg.callCount,
			SuccessRate: successRate,
			AvgRetries:  float64(toolRetryCounts[name]),
			MaxLoop:     toolMaxLoops[name],
		})
	}
	sort.Slice(toolUsage, func(i, j int) bool {
		return toolUsage[i].CallCount > toolUsage[j].CallCount
	})

	stats := &AgentStats{
		TraceSuccessRate:    traceSuccessRate,
		AvgToolSuccessRate:  avgToolSuccessRate,
		AvgRetries:          avgRetries,
		AvgLoopDepth:        avgLoopDepth,
		MaxLoopDepth:        globalMaxLoop,
		SpanPerTrace:        spanPerTrace,
		TotalToolCalls:      totalToolCalls,
		SuccessfulToolCalls: successfulToolCalls,
		ToolUsage:           toolUsage,
	}

	stats.Insights = generateInsights(stats)

	return stats
}

// generateInsights produces actionable insights from agent stats.
func generateInsights(stats *AgentStats) []string {
	var insights []string
	for _, item := range stats.ToolUsage {
		if item.MaxLoop >= 3 {
			insights = append(insights, fmt.Sprintf("%s has max loop depth %d — agent may be stuck in a retry loop", item.ToolName, item.MaxLoop))
		}
	}
	for _, item := range stats.ToolUsage {
		if item.SuccessRate < 0.8 && item.CallCount >= 3 {
			insights = append(insights, fmt.Sprintf("%s has low success rate (%d%%) — consider adding fallback logic", item.ToolName, int(item.SuccessRate*100)))
		}
	}
	if stats.TraceSuccessRate < 0.7 && stats.TraceSuccessRate > 0 {
		insights = append(insights, fmt.Sprintf("Over %d%% of traces failed — agent configuration may need adjustment", int((1-stats.TraceSuccessRate)*100)))
	}
	if stats.AvgRetries > 1.0 {
		insights = append(insights, fmt.Sprintf("High average retry count (%.1f) — tool calls frequently fail on first attempt", stats.AvgRetries))
	}
	if len(insights) > 4 {
		insights = insights[:4]
	}
	return insights
}
