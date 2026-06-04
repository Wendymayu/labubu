package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labubu/labubu/internal/storage"
)

// --- OTLP JSON types (hand-rolled for clean JSON output) ---

type otlpTraceResponse struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes             []otlpKeyValue `json:"attributes"`
	DroppedAttributesCount int            `json:"droppedAttributesCount"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name       string         `json:"name"`
	Version    string         `json:"version"`
	Attributes []otlpKeyValue `json:"attributes"`
}

type otlpSpan struct {
	TraceID                string         `json:"traceId"`
	SpanID                 string         `json:"spanId"`
	ParentSpanID           string         `json:"parentSpanId"`
	Name                   string         `json:"name"`
	Kind                   int32          `json:"kind"`
	StartTimeUnixNano      string         `json:"startTimeUnixNano"`
	EndTimeUnixNano        string         `json:"endTimeUnixNano"`
	Attributes             []otlpKeyValue `json:"attributes"`
	DroppedAttributesCount int            `json:"droppedAttributesCount"`
	Events                 []otlpEvent    `json:"events"`
	DroppedEventsCount     int            `json:"droppedEventsCount"`
	Links                  []otlpLink     `json:"links"`
	DroppedLinksCount      int            `json:"droppedLinksCount"`
	Status                 otlpStatus     `json:"status"`
}

type otlpEvent struct {
	TimeUnixNano           string         `json:"timeUnixNano"`
	Name                   string         `json:"name"`
	Attributes             []otlpKeyValue `json:"attributes"`
	DroppedAttributesCount int            `json:"droppedAttributesCount"`
}

type otlpLink struct {
	TraceID                string         `json:"traceId"`
	SpanID                 string         `json:"spanId"`
	Attributes             []otlpKeyValue `json:"attributes"`
	DroppedAttributesCount int            `json:"droppedAttributesCount"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue string `json:"stringValue"`
}

type otlpStatus struct {
	Code    int32  `json:"code"`
	Message string `json:"message,omitempty"`
}

// convertToOTLP converts an internal TraceDetail to OTLP JSON format.
func convertToOTLP(t *storage.TraceDetail) otlpTraceResponse {
	resource := otlpResource{
		Attributes:             mapToKeyValues(t.ResourceAttrs),
		DroppedAttributesCount: 0,
	}

	scopeSpans := otlpScopeSpans{
		Scope: otlpScope{
			Name:       t.Scope.Name,
			Version:    t.Scope.Version,
			Attributes: mapToKeyValues(t.Scope.Attributes),
		},
		Spans: make([]otlpSpan, 0, len(t.Spans)),
	}

	for _, s := range t.Spans {
		scopeSpans.Spans = append(scopeSpans.Spans, spanToOTLP(s, t.TraceIDHex))
	}

	return otlpTraceResponse{
		ResourceSpans: []otlpResourceSpans{{
			Resource:   resource,
			ScopeSpans: []otlpScopeSpans{scopeSpans},
		}},
	}
}

func spanToOTLP(s storage.SpanDetail, traceIDHex string) otlpSpan {
	endTimeMS := s.StartTimeMS + s.DurationMS

	return otlpSpan{
		TraceID:                traceIDHex,
		SpanID:                 s.SpanID,
		ParentSpanID:           s.ParentSpanID,
		Name:                   s.Name,
		Kind:                   kindToInt32(s.Kind),
		StartTimeUnixNano:      msToNanoString(s.StartTimeMS),
		EndTimeUnixNano:        msToNanoString(endTimeMS),
		Attributes:             mapToKeyValues(s.Attributes),
		DroppedAttributesCount: 0,
		Events:                 convertEvents(s.Events),
		DroppedEventsCount:     0,
		Links:                  convertLinks(s.Links),
		DroppedLinksCount:      0,
		Status:                 statusToOTLP(s.Status, s.StatusMessage),
	}
}

func msToNanoString(ms uint64) string {
	ns := ms * 1_000_000
	return strconv.FormatUint(ns, 10)
}

func kindToInt32(kind string) int32 {
	switch kind {
	case "UNSPECIFIED":
		return 0
	case "INTERNAL":
		return 1
	case "SERVER":
		return 2
	case "CLIENT":
		return 3
	case "PRODUCER":
		return 4
	case "CONSUMER":
		return 5
	default:
		return 1
	}
}

func statusToOTLP(status string, message string) otlpStatus {
	switch status {
	case "OK":
		return otlpStatus{Code: 1, Message: message}
	case "ERROR":
		return otlpStatus{Code: 2, Message: message}
	default:
		return otlpStatus{Code: 0, Message: message}
	}
}

func mapToKeyValues(m map[string]string) []otlpKeyValue {
	if len(m) == 0 {
		return []otlpKeyValue{}
	}
	result := make([]otlpKeyValue, 0, len(m))
	for k, v := range m {
		result = append(result, otlpKeyValue{
			Key:   k,
			Value: otlpAnyValue{StringValue: v},
		})
	}
	return result
}

func convertEvents(events []interface{}) []otlpEvent {
	if len(events) == 0 {
		return []otlpEvent{}
	}
	result := make([]otlpEvent, 0, len(events))
	for _, e := range events {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		timeMS, _ := m["time_ms"].(float64)
		attrs := mapFromInterface(m["attributes"])

		result = append(result, otlpEvent{
			TimeUnixNano:           msToNanoString(uint64(timeMS)),
			Name:                   name,
			Attributes:             mapToKeyValues(attrs),
			DroppedAttributesCount: 0,
		})
	}
	return result
}

func convertLinks(links []interface{}) []otlpLink {
	if len(links) == 0 {
		return []otlpLink{}
	}
	result := make([]otlpLink, 0, len(links))
	for _, l := range links {
		m, ok := l.(map[string]interface{})
		if !ok {
			continue
		}
		traceID, _ := m["trace_id"].(string)
		spanID, _ := m["span_id"].(string)
		attrs := mapFromInterface(m["attributes"])

		result = append(result, otlpLink{
			TraceID:                traceID,
			SpanID:                 spanID,
			Attributes:             mapToKeyValues(attrs),
			DroppedAttributesCount: 0,
		})
	}
	return result
}

func mapFromInterface(attrsRaw interface{}) map[string]string {
	if attrsRaw == nil {
		return nil
	}
	m, ok := attrsRaw.(map[string]interface{})
	if !ok {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

// writeOTLPResponse converts a TraceDetail to OTLP JSON and writes it.
func writeOTLPResponse(w http.ResponseWriter, t *storage.TraceDetail) {
	otlp := convertToOTLP(t)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(otlp)
}
