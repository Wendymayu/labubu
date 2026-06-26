package receiver

import (
	"fmt"
	"strings"

	"github.com/labubu/labubu/internal/storage"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// translateLogs converts OTLP ResourceLogs → []storage.LogRecord.
func translateLogs(resourceLogs []*logspb.ResourceLogs) []storage.LogRecord {
	var records []storage.LogRecord

	for _, rl := range resourceLogs {
		if rl == nil {
			continue
		}
		resourceAttrs := keyValueToMap(rl.Resource.GetAttributes())

		for _, scopeLog := range rl.ScopeLogs {
			if scopeLog == nil {
				continue
			}
			scopeAttrs := keyValueToMap(scopeLog.Scope.GetAttributes())

			for _, lr := range scopeLog.LogRecords {
				if lr == nil {
					continue
				}
				record := translateLogRecord(lr, resourceAttrs, scopeAttrs)
				records = append(records, record)
			}
		}
	}

	return records
}

// translateLogRecord converts a single OTLP LogRecord to storage.LogRecord.
func translateLogRecord(lr *logspb.LogRecord, resourceAttrs, scopeAttrs map[string]string) storage.LogRecord {
	var traceID [16]byte
	copy(traceID[:], lr.TraceId)

	var spanID [8]byte
	copy(spanID[:], lr.SpanId)

	// OTLP spec: TimeUnixNano is optional; when absent, fall back to ObservedTimeUnixNano.
	ts := lr.TimeUnixNano
	if ts == 0 {
		ts = lr.ObservedTimeUnixNano
	}
	timestamp := ts / 1_000_000

	// OTLP spec: when SeverityNumber is UNSPECIFIED, derive severity from SeverityText.
	severity := severityNumberToString(lr.SeverityNumber)
	if lr.SeverityNumber == logspb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED && lr.SeverityText != "" {
		severity = severityTextToLabel(lr.SeverityText)
	}

	// Merge resource + scope + log record attributes.
	attrs := make(map[string]string)
	for k, v := range resourceAttrs {
		attrs[k] = v
	}
	for k, v := range scopeAttrs {
		attrs[k] = v
	}
	for k, v := range keyValueToMap(lr.Attributes) {
		attrs[k] = v
	}

	// Extract event.name from attributes for the dedicated column.
	eventName := ""
	if v, ok := attrs["event.name"]; ok {
		eventName = v
	}

	// Body: use the string value.
	body := anyValueToString(lr.Body)

	return storage.LogRecord{
		TraceID:    traceID,
		SpanID:     spanID,
		Timestamp:  timestamp,
		Severity:   severity,
		EventName:  eventName,
		Body:       body,
		Attributes: attrs,
	}
}

// severityNumberToString converts OTLP SeverityNumber to a string label.
// Reference: https://opentelemetry.io/docs/specs/otel/logs/data-model/#severity-fields
func severityNumberToString(sn logspb.SeverityNumber) string {
	switch sn {
	case logspb.SeverityNumber_SEVERITY_NUMBER_TRACE, logspb.SeverityNumber_SEVERITY_NUMBER_TRACE2, logspb.SeverityNumber_SEVERITY_NUMBER_TRACE3, logspb.SeverityNumber_SEVERITY_NUMBER_TRACE4:
		return "TRACE"
	case logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG, logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG2, logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG3, logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG4:
		return "DEBUG"
	case logspb.SeverityNumber_SEVERITY_NUMBER_INFO, logspb.SeverityNumber_SEVERITY_NUMBER_INFO2, logspb.SeverityNumber_SEVERITY_NUMBER_INFO3, logspb.SeverityNumber_SEVERITY_NUMBER_INFO4:
		return "INFO"
	case logspb.SeverityNumber_SEVERITY_NUMBER_WARN, logspb.SeverityNumber_SEVERITY_NUMBER_WARN2, logspb.SeverityNumber_SEVERITY_NUMBER_WARN3, logspb.SeverityNumber_SEVERITY_NUMBER_WARN4:
		return "WARN"
	case logspb.SeverityNumber_SEVERITY_NUMBER_ERROR, logspb.SeverityNumber_SEVERITY_NUMBER_ERROR2, logspb.SeverityNumber_SEVERITY_NUMBER_ERROR3, logspb.SeverityNumber_SEVERITY_NUMBER_ERROR4:
		return "ERROR"
	case logspb.SeverityNumber_SEVERITY_NUMBER_FATAL, logspb.SeverityNumber_SEVERITY_NUMBER_FATAL2, logspb.SeverityNumber_SEVERITY_NUMBER_FATAL3, logspb.SeverityNumber_SEVERITY_NUMBER_FATAL4:
		return "FATAL"
	default:
		return fmt.Sprintf("SEVERITY_NUMBER_%d", int(sn))
	}
}

// severityTextToLabel derives a canonical severity label
// (TRACE/DEBUG/INFO/WARN/ERROR/FATAL) from SeverityText when SeverityNumber
// is UNSPECIFIED. This implements the OTLP spec's requirement to derive
// severity from text when the number is absent. Unrecognized text is returned
// uppercased so the record gets a meaningful severity instead of the
// "SEVERITY_NUMBER_0" placeholder. The match is case-insensitive and
// prefix-based so common variants like "WARNING"→WARN, "ERROR"→ERROR work.
func severityTextToLabel(text string) string {
	upper := strings.ToUpper(text)
	switch {
	case strings.HasPrefix(upper, "TRACE"):
		return "TRACE"
	case strings.HasPrefix(upper, "DEBUG"):
		return "DEBUG"
	case strings.HasPrefix(upper, "INFO"):
		return "INFO"
	case strings.HasPrefix(upper, "WARN"):
		return "WARN"
	case strings.HasPrefix(upper, "ERR"):
		return "ERROR"
	case strings.HasPrefix(upper, "FATAL"), strings.HasPrefix(upper, "CRIT"):
		return "FATAL"
	}
	return upper
}
