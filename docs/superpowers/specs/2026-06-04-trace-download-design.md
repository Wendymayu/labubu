# Trace Download Feature

**Date**: 2026-06-04
**Status**: designed, not yet planned

## Motivation

Users want to download trace data for local analysis using tools like jq, Python, VS Code, or other observability platforms. The current trace detail page displays data but has no export capability.

## Design

### Phase 1: JSON Download (Now)

Add a download button to the trace detail page that saves the current trace as a formatted JSON file.

**UI placement:**
- A "Download" button in the trace summary area (alongside Trace ID, Service, Duration, Spans, Total Tokens).
- Button text: icon + "Download" or just an icon.

**Behavior:**
- Click → serialize `trace` object to pretty-printed JSON (`JSON.stringify(trace, null, 2)`).
- Create a `Blob` with MIME type `application/json`.
- Trigger browser download via `URL.createObjectURL` + `<a>` click.
- File name: `trace-{traceIdHex}.json`.

**Implementation:**
- TraceDetail.vue: add a download button in the template, add a `downloadTrace()` function.
- No backend changes. Trace data is already loaded in memory when viewing the detail page.

### Phase 2: OTLP JSON Export (Future)

Add an optional `?format=otlp` query parameter to the trace detail API endpoint that converts the internal trace representation to OTLP JSON format, enabling import into Jaeger, Grafana Tempo, and other OTLP-compatible platforms.

**Backend:**
- New endpoint or parameter: `GET /api/v1/traces/{traceIdHex}?format=otlp`
- Format conversion logic in `trace_handler.go` or a new file.
- Convert internal `TraceDetail` → OTLP `ResourceSpans` → `ScopeSpans` → `Spans` JSON structure.

**Frontend:**
- Update download button to offer format choice (simple dropdown or two buttons).
- OTLP format fetches from the new endpoint; JSON format uses in-memory data.

## Files to Change (Phase 1)

| File | Changes |
|------|---------|
| `web/src/views/TraceDetail.vue` | Add download button in summary area + `downloadTrace()` function |

## Edge Cases

- **Trace fails to load**: download button hidden (already gated by `v-else-if="trace"`).
- **Very large trace**: JSON serialization may take a moment for traces with thousands of spans; acceptable — browser handles it asynchronously via Blob.

## What Stays the Same

- All existing trace detail functionality (waterfall, drawer, span detail).
- Backend (zero changes for Phase 1).
- API client types.
