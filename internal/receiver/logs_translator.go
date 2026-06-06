package receiver

import (
	"fmt"

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

	timestamp := lr.TimeUnixNano / 1_000_000

	severity := severityNumberToString(lr.SeverityNumber)

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
