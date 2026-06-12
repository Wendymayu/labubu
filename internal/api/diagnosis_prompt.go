package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/labubu/labubu/internal/storage"
)

const diagnosisSystemPrompt = `You are an LLM observability expert analyzing OTLP trace data from AI agent applications.
Evaluate the trace across four dimensions on a 0-100 scale:

1. **Latency (延迟):** Is the overall duration acceptable? Are there unnecessary delays or slow spans?
2. **Cost (成本):** Is token usage efficient? Is context window utilization reasonable? Are there redundant LLM calls?
3. **Error (错误):** Are there ERROR spans, exceptions, or failures? How severe are they?
4. **Efficiency (效率):** Are tool calls well-structured? Could parallelization reduce time? Are there redundant or circular tool calls?

Scoring guidelines:
- 90-100: Excellent, no issues
- 80-89: Good, minor optimizations possible
- 60-79: Fair, notable issues to address
- 40-59: Poor, significant problems
- 0-39: Critical, major failures

For each dimension scoring below 80, provide specific findings with titles, descriptions, and actionable suggestions.
Return ONLY valid JSON matching this schema — no markdown, no preamble:

{
  "overall_score": 72,
  "scores": {
    "latency": 85,
    "cost": 62,
    "error": 45,
    "efficiency": 88
  },
  "summary": "one-sentence summary",
  "findings": [
    {
      "severity": "error",
      "dimension": "error",
      "title": "short title",
      "description": "detailed description referencing specific spans and data",
      "suggestion": "actionable improvement suggestion"
    }
  ]
}`

// buildDiagnosisUserPrompt creates the user prompt for trace diagnosis.
func buildDiagnosisUserPrompt(trace *storage.TraceDetail, logs []storage.LogListItem) string {
	var b strings.Builder

	// Trace summary
	b.WriteString("Trace Summary:\n")
	service := ""
	if v, ok := trace.ResourceAttrs["service.name"]; ok {
		service = v
	}
	b.WriteString(fmt.Sprintf("- Service: %s\n", service))
	b.WriteString(fmt.Sprintf("- Total spans: %d\n", trace.SpanCount))
	b.WriteString(fmt.Sprintf("- Total duration: %.1fs\n", float64(trace.DurationMS)/1000.0))

	totalTokens := uint32(0)
	llmCount := 0
	for _, s := range trace.Spans {
		if s.TotalTokens != nil {
			totalTokens += *s.TotalTokens
		}
		if s.GenAIRequestModel != nil {
			llmCount++
		}
	}
	b.WriteString(fmt.Sprintf("- Total tokens: %d\n", totalTokens))
	b.WriteString(fmt.Sprintf("- LLM spans: %d\n", llmCount))
	if trace.Cost != nil {
		b.WriteString(fmt.Sprintf("- Total cost: %.4f %s\n", *trace.Cost, trace.CostCurrency))
	}
	b.WriteString("\n")

	// Span list
	b.WriteString("Spans:\n")
	for i, s := range trace.Spans {
		kindStr := spanKindLabel(s.Kind)
		if s.GenAIRequestModel != nil {
			kindStr = "LLM"
		}
		b.WriteString(fmt.Sprintf("[%d] %s | %s | %.2fs", i, s.Name, kindStr, float64(s.DurationMS)/1000.0))
		if s.InputTokens != nil {
			b.WriteString(fmt.Sprintf(" | input=%d output=%d total=%d", *s.InputTokens, *s.OutputTokens, *s.TotalTokens))
		}
		if s.GenAIRequestModel != nil {
			b.WriteString(fmt.Sprintf(" | model=%s", *s.GenAIRequestModel))
		}
		b.WriteString(fmt.Sprintf(" | status=%s", s.Status))
		if s.StatusMessage != "" {
			b.WriteString(fmt.Sprintf(" | message=%q", s.StatusMessage))
		}
		b.WriteString("\n")

		// Events
		if len(s.Events) > 0 {
			eventsJSON, _ := jsonMarshalCompact(s.Events)
			b.WriteString(fmt.Sprintf("  Events: %s\n", eventsJSON))
		}
	}
	b.WriteString("\n")

	// Logs
	if len(logs) > 0 {
		b.WriteString("Logs:\n")
		for _, l := range logs {
			b.WriteString(fmt.Sprintf("[%s] %s", l.Severity, l.EventName))
			if l.Body != "" {
				b.WriteString(fmt.Sprintf(" | body=%s", l.Body))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

// spanKindLabel returns a human-readable label for a span kind code.
func spanKindLabel(kind string) string {
	switch kind {
	case "INTERNAL":
		return "INTERNAL"
	case "SERVER":
		return "SERVER"
	case "CLIENT":
		return "CLIENT"
	case "PRODUCER":
		return "PRODUCER"
	case "CONSUMER":
		return "CONSUMER"
	default:
		return "UNSPECIFIED"
	}
}

// jsonMarshalCompact marshals v as compact JSON (no extra whitespace).
func jsonMarshalCompact(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}