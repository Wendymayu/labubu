package api

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/labubu/labubu/internal/storage"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// --- Proto-based OTLP conversion (standard protojson output) ---

// convertToProto builds a proto TracesData from an internal TraceDetail.
func convertToProto(t *storage.TraceDetail) *tracepb.TracesData {
	resource := &resourcepb.Resource{
		Attributes:             mapToProtoKeyValues(t.ResourceAttrs),
		DroppedAttributesCount: 0,
	}
	if t.ResourceSchemaURL != "" {
		// protojson omits empty strings, so only set when present
	}

	scopeSpans := &tracepb.ScopeSpans{
		Scope: &commonpb.InstrumentationScope{
			Name:       t.Scope.Name,
			Version:    t.Scope.Version,
			Attributes: mapToProtoKeyValues(t.Scope.Attributes),
		},
		Spans: make([]*tracepb.Span, 0, len(t.Spans)),
	}

	for _, s := range t.Spans {
		scopeSpans.Spans = append(scopeSpans.Spans, spanToProto(s, t.TraceIDHex))
	}

	rs := &tracepb.ResourceSpans{
		Resource:   resource,
		ScopeSpans: []*tracepb.ScopeSpans{scopeSpans},
	}
	if t.ResourceSchemaURL != "" {
		rs.SchemaUrl = t.ResourceSchemaURL
	}

	return &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{rs},
	}
}

// spanToProto converts a SpanDetail to a proto Span.
func spanToProto(s storage.SpanDetail, traceIDHex string) *tracepb.Span {
	traceIDBytes, _ := hex.DecodeString(traceIDHex)
	spanIDBytes, _ := hex.DecodeString(s.SpanID)
	parentSpanIDBytes, _ := hex.DecodeString(s.ParentSpanID)

	startTimeNano := s.StartTimeMS * 1_000_000
	endTimeNano := (s.StartTimeMS + s.DurationMS) * 1_000_000

	span := &tracepb.Span{
		TraceId:                traceIDBytes,
		SpanId:                 spanIDBytes,
		ParentSpanId:           parentSpanIDBytes,
		Name:                   s.Name,
		Kind:                   kindToProtoEnum(s.Kind),
		StartTimeUnixNano:      startTimeNano,
		EndTimeUnixNano:        endTimeNano,
		Attributes:             mapToProtoKeyValues(s.Attributes),
		DroppedAttributesCount: 0,
		Events:                 eventsToProto(s.Events),
		DroppedEventsCount:     0,
		Links:                  linksToProto(s.Links),
		DroppedLinksCount:      0,
		Status:                 statusToProto(s.Status, s.StatusMessage),
	}
	return span
}

// kindToProtoEnum converts a Kind string to the proto Span_SpanKind enum.
func kindToProtoEnum(kind string) tracepb.Span_SpanKind {
	switch kind {
	case "UNSPECIFIED":
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	case "INTERNAL":
		return tracepb.Span_SPAN_KIND_INTERNAL
	case "SERVER":
		return tracepb.Span_SPAN_KIND_SERVER
	case "CLIENT":
		return tracepb.Span_SPAN_KIND_CLIENT
	case "PRODUCER":
		return tracepb.Span_SPAN_KIND_PRODUCER
	case "CONSUMER":
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_INTERNAL
	}
}

// statusToProto converts a Status string + message to a proto Status.
func statusToProto(status string, message string) *tracepb.Status {
	var code tracepb.Status_StatusCode
	switch status {
	case "OK":
		code = tracepb.Status_STATUS_CODE_OK
	case "ERROR":
		code = tracepb.Status_STATUS_CODE_ERROR
	default:
		code = tracepb.Status_STATUS_CODE_UNSET
	}
	return &tracepb.Status{
		Code:    code,
		Message: message,
	}
}

// inferAnyValue does type inference on a string: try int64, then float64, then bool, then string.
func inferAnyValue(s string) *commonpb.AnyValue {
	// Try int64
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v}}
	}
	// Try float64
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v}}
	}
	// Try bool
	if v, err := strconv.ParseBool(s); err == nil {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: v}}
	}
	// Fallback: string
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

// mapToProtoKeyValues converts a map[string]string to proto KeyValue pairs with type inference.
func mapToProtoKeyValues(m map[string]string) []*commonpb.KeyValue {
	if len(m) == 0 {
		return []*commonpb.KeyValue{}
	}
	result := make([]*commonpb.KeyValue, 0, len(m))
	for k, v := range m {
		result = append(result, &commonpb.KeyValue{
			Key:   k,
			Value: inferAnyValue(v),
		})
	}
	return result
}

// eventsToProto converts []interface{} (parsed JSON events) to proto Span_Event.
func eventsToProto(events []interface{}) []*tracepb.Span_Event {
	if len(events) == 0 {
		return []*tracepb.Span_Event{}
	}
	result := make([]*tracepb.Span_Event, 0, len(events))
	for _, e := range events {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		timeMS, _ := m["time_ms"].(float64)
		attrs := mapFromInterface(m["attributes"])

		result = append(result, &tracepb.Span_Event{
			TimeUnixNano:           uint64(timeMS) * 1_000_000,
			Name:                   name,
			Attributes:             mapToProtoKeyValues(attrs),
			DroppedAttributesCount: 0,
		})
	}
	return result
}

// linksToProto converts []interface{} (parsed JSON links) to proto Span_Link.
func linksToProto(links []interface{}) []*tracepb.Span_Link {
	if len(links) == 0 {
		return []*tracepb.Span_Link{}
	}
	result := make([]*tracepb.Span_Link, 0, len(links))
	for _, l := range links {
		m, ok := l.(map[string]interface{})
		if !ok {
			continue
		}
		traceID, _ := m["trace_id"].(string)
		spanID, _ := m["span_id"].(string)
		attrs := mapFromInterface(m["attributes"])

		traceIDBytes, _ := hex.DecodeString(traceID)
		spanIDBytes, _ := hex.DecodeString(spanID)

		result = append(result, &tracepb.Span_Link{
			TraceId:                traceIDBytes,
			SpanId:                 spanIDBytes,
			Attributes:             mapToProtoKeyValues(attrs),
			DroppedAttributesCount: 0,
		})
	}
	return result
}

// mapFromInterface extracts a map[string]string from a raw JSON attribute map.
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

// marshalOTLPJSON marshals a TracesData proto to JSON using protojson,
// then post-processes base64-encoded traceId/spanId/parentSpanId fields to hex.
func marshalOTLPJSON(td *tracepb.TracesData) ([]byte, error) {
	marshaler := protojson.MarshalOptions{
		UseProtoNames: true,
	}
	jsonBytes, err := marshaler.Marshal(td)
	if err != nil {
		return nil, fmt.Errorf("protojson marshal: %w", err)
	}
	return replaceBase64WithHex(jsonBytes), nil
}

// base64HexRe matches "trace_id":"<base64>", "span_id":"<base64>", "parent_span_id":"<base64>"
// (protojson with UseProtoNames uses snake_case), OR the camelCase variants
// "traceId":"<base64>", "spanId":"<base64>", "parentSpanId":"<base64>".
// We use [^"]+ to match the value since base64 can contain + and / which are tricky in regex character classes.
var base64HexRe = regexp.MustCompile(`"(?:trace_id|traceId|span_id|spanId|parent_span_id|parentSpanId)"\s*:\s*"([^"]+)"`)

// replaceBase64WithHex converts base64-encoded ID fields to hex strings in protojson output.
// protojson encodes bytes fields as base64; OTLP JSON convention uses hex.
// Only replaces values that successfully decode as base64 and produce reasonable byte lengths
// (8 bytes for span/parent IDs, 16 bytes for trace IDs).
func replaceBase64WithHex(jsonBytes []byte) []byte {
	return base64HexRe.ReplaceAllFunc(jsonBytes, func(match []byte) []byte {
		submatch := base64HexRe.FindSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		b64Value := submatch[1]

		decoded, err := base64.StdEncoding.DecodeString(string(b64Value))
		if err != nil {
			// If standard base64 fails, try raw (no-padding) base64
			decoded, err = base64.RawStdEncoding.DecodeString(string(b64Value))
			if err != nil {
				return match // leave unchanged if decode fails
			}
		}

		// Only replace if the decoded length matches expected ID sizes (16 for trace, 8 for span)
		if len(decoded) != 16 && len(decoded) != 8 {
			return match // leave unchanged if size doesn't match expected ID lengths
		}

		hexStr := hex.EncodeToString(decoded)

		// Reconstruct the full match with the hex value, preserving the field name.
		fullMatch := string(match)
		colonIdx := strings.Index(fullMatch, ":")
		if colonIdx == -1 {
			return match
		}
		fieldPart := fullMatch[:colonIdx+1]
		return []byte(fieldPart + `"` + hexStr + `"`)
	})
}

// hexBase64Re matches "trace_id":"<hex>", "span_id":"<hex>", "parent_span_id":"<hex>"
// (snake_case) or camelCase variants, where hex is a valid hex string (at least 1 char).
var hexBase64Re = regexp.MustCompile(`"(?:trace_id|traceId|span_id|spanId|parent_span_id|parentSpanId)"\s*:\s*"([0-9a-fA-F]+)"`)

// replaceHexWithBase64 converts hex-encoded ID fields back to base64 (for import).
func replaceHexWithBase64(jsonBytes []byte) []byte {
	return hexBase64Re.ReplaceAllFunc(jsonBytes, func(match []byte) []byte {
		submatch := hexBase64Re.FindSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		hexValue := submatch[1]

		decoded, err := hex.DecodeString(string(hexValue))
		if err != nil {
			return match // leave unchanged if decode fails
		}
		b64Str := base64.StdEncoding.EncodeToString(decoded)

		// Reconstruct the full match preserving the field name part.
		fullMatch := string(match)
		colonIdx := strings.Index(fullMatch, ":")
		if colonIdx == -1 {
			return match
		}
		fieldPart := fullMatch[:colonIdx+1]
		return []byte(fieldPart + `"` + b64Str + `"`)
	})
}
