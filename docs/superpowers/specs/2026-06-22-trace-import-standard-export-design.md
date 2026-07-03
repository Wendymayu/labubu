# Trace Import & Standardized OTLP Export Design

## Problem

1. **No trace import**: Labubu can download/export traces as OTLP JSON, but cannot import them. Users cannot restore traces from backups, share traces between instances, or import data from other observability tools.
2. **Non-standard export format**: The current OTLP export uses hand-rolled Go structs (`otlpTraceResponse`, etc.) with `encoding/json`. All attribute values are flattened to `stringValue`, losing type information (integers, booleans, doubles). Other tools (Jaeger, SigNoz, Tempo) use standard protojson with multi-type `AnyValue` (`intValue`, `doubleValue`, `boolValue`).
3. **OTLP HTTP JSON receiver bug**: The HTTP handler in `internal/receiver/otlp.go` uses `proto.Unmarshal` (binary) for JSON payloads, causing all JSON OTLP requests to fail with a parse error.

## Design Decisions

### 1. Export format standardization

Replace hand-rolled structs with `protojson.Marshal` on standard proto types (`tracepb.TracesData`).

- **traceId/spanId encoding**: protojson defaults to base64 for `bytes` fields. Post-process with regex to replace base64 values for `traceId`/`spanId` fields with hex strings — human-readable and compatible with Jaeger/SigNoz convention.
- **AnyValue types**: protojson naturally preserves `intValue`, `doubleValue`, `boolValue` etc. from internal data. Attribute values that were originally integers (e.g., `gen_ai.usage.input_tokens`) will export as `"intValue": "42"` instead of `"stringValue": "42"`.
- **Deleted code**: Remove entire `otlp_trace.go` file (hand-rolled structs: `otlpTraceResponse`, `otlpResourceSpans`, `otlpResource`, `otlpScopeSpans`, `otlpScope`, `otlpSpan`, `otlpEvent`, `otlpLink`, `otlpKeyValue`, `otlpAnyValue`, `otlpStatus`, and all helper functions like `convertToOTLP`, `spanToOTLP`, `mapToKeyValues`, `convertEvents`, `convertLinks`, `mapFromInterface`, `msToNanoString`, `kindToInt32`, `statusToOTLP`, `writeOTLPResponse`).

**New conversion flow**: `TraceDetail` → `tracepb.TracesData` proto struct → `protojson.Marshal` → regex hex replacement → JSON response.

### 2. Import endpoint

**`POST /api/v1/traces/import`**

- Accepts OTLP JSON body (standard `TracesData` or `ExportTraceServiceRequest` format)
- Uses `protojson.Unmarshal` to deserialize — naturally compatible with any standard OTLP JSON
- Before unmarshal, pre-process `traceId`/`spanId` hex values → base64 (reverse of export post-processing) so `protojson.Unmarshal` can parse them correctly
- Iterate `ResourceSpans`:
  - `translateResource(rs.Resource)` → `ResourceInfo`
  - For each `ScopeSpans`:
    - `translateScope(ss.Scope)` → `ScopeInfo`
    - `translateSpans(ss.Spans)` → `[]Span`
    - `store.InsertSpans(ctx, resource, scope, spans)` — reuses existing ingestion pipeline
- For each trace_id in the import data, check if it already exists in the store via `GetTrace`. If it exists, skip it entirely (do not merge/overwrite). Only call `InsertSpans` for new trace_ids.
- Return: `{ "imported": N, "skipped": M }` where N = new trace_ids written, M = trace_ids already in DB that were skipped
- Size limit: 10MB max request body (configurable)
- Rate limit: single import at a time (mutex on the store already prevents concurrent writes)

### 3. OTLP HTTP JSON receiver fix

In `internal/receiver/otlp.go`, the `handleTracesHTTP` function:

- `Content-Type: application/json` → `protojson.Unmarshal(body, &exportReq)`
- Default / `application/x-protobuf` → `proto.Unmarshal(body, &exportReq)`
- Same fix pattern for metrics (`handleMetricsHTTP`) if applicable
- Import: `go.opentelemetry.io/proto/otlp/collector/trace/v1` package (already in go.mod as `v0.20.0`)
- Import: `google.golang.org/protobuf/encoding/protojson` (already an indirect dependency via proto package)

### 4. Frontend: Import button on trace list page

- Add "Import" button in `TraceList.vue` toolbar, next to the existing "Download Selected" button
- Click triggers hidden `<input type="file" accept=".json">`
- Read file as text, POST to `/api/v1/traces/import` with JSON body
- Show result: toast-style notification `{imported: N, skipped: M}` or error message
- After successful import, refresh trace list

**API client** (`web/src/api/client.ts`):
- New type: `ImportResult { imported: number; skipped: number }`
- New function: `importTraces(jsonData: string): Promise<ImportResult>`

**i18n keys** (both `en.ts` and `zh.ts`):
- `traceList.import` / `traceList.import` = "Import" / "导入"
- `traceList.importResult` = "Imported {imported} traces, skipped {skipped}" / "已导入 {imported} 条 trace，跳过 {skipped} 条"
- `traceList.importFailed` = "Import failed: {error}" / "导入失败：{error}"

## Implementation Details

### Proto conversion: TraceDetail → TracesData

New function `convertToProto(t *storage.TraceDetail) *tracepb.TracesData`:

- Build `ResourceSpans` from `TraceDetail.ResourceAttrs`
- Build `ScopeSpans` from `TraceDetail.Scope` + `TraceDetail.Spans`
- Map each `SpanDetail` → `tracepb.Span`:
  - `traceId`: hex → 16-byte array
  - `spanId`: hex → 8-byte array
  - `parentSpanId`: hex → 8-byte array (empty string → zero bytes)
  - `kind`: string → int32 enum (reuse `kindToInt32` logic)
  - `startTimeUnixNano` / `endTimeUnixNano`: ms → nanos string
  - `attributes`: `map[string]string` → `[]*commonpb.KeyValue` with proper `AnyValue` types
  - `events`: parse JSON → `[]*tracepb.Span_Event`
  - `links`: parse JSON → `[]*tracepb.Span_Link`
  - `status`: string → `tracepb.Status` enum

**Attribute value type inference**: Since internal storage flattens all attributes to `string`, we need to infer the original type for standard output:
- Try `int64` first: if the string parses as an integer, output as `intValue`
- Try `float64`: if it parses as a float but not an integer, output as `doubleValue`
- Try `bool`: if the string is `"true"` or `"false"`, output as `boolValue`
- Default: `stringValue`

This type inference makes Labubu's export format compatible with standard tools while working with the internal flat-string storage.

### Proto conversion: TracesData → internal types (import)

Reuses existing `translateResource`, `translateScope`, `translateSpans` from `internal/receiver/otlp.go`. These functions already handle proto → internal type conversion.

New handler function `ImportTraces` in `internal/api/trace_handler.go`:

```go
func (h *TraceHandler) ImportTraces(w http.ResponseWriter, r *http.Request) {
    // 1. Read body (max 10MB)
    // 2. Pre-process: hex traceId/spanId → base64
    // 3. protojson.Unmarshal into TracesData
    // 4. Iterate ResourceSpans → translateResource/translateScope/translateSpans → InsertSpans
    // 5. Track imported vs skipped trace_ids
    // 6. Return {imported, skipped}
}
```

### hex ↔ base64 conversion for traceId/spanId

**Export (base64 → hex):** Regex post-processing on protojson output:
```
Replace "traceId":"<base64>" and "spanId":"<base64>" and "parentSpanId":"<base64>"
with hex equivalents.
```

Pattern: `"traceId":"([A-Za-z0-9+/=]+)"` → decode base64 → encode hex → `"traceId":"<hex>"`

**Import (hex → base64):** Pre-processing before protojson.Unmarshal:
- Detect hex-encoded traceId/spanId (32-char hex for traceId, 16-char hex for spanId)
- Convert to base64 for protojson compatibility
- If already base64, leave unchanged

### Router changes (`internal/api/router.go`)

Add:
```go
mux.HandleFunc("/api/v1/traces/import", traceHandler.ImportTraces)
```

### Export endpoint changes

`ExportTraces` handler stays at `POST /api/v1/traces/export` but now:
- Uses `convertToProto` instead of `convertToOTLP`
- Uses `protojson.Marshal` + hex post-processing instead of `json.Marshal` on hand-rolled structs

## File Map

| File | Action | Description |
|------|--------|-------------|
| `internal/api/otlp_trace.go` | **Delete** | Hand-rolled OTLP structs replaced by protojson |
| `internal/api/otlp_proto.go` | **Create** | Proto conversion: `convertToProto`, attribute type inference, hex↔base64 helpers |
| `internal/api/trace_handler.go` | **Modify** | Add `ImportTraces` handler, update `ExportTraces` to use protojson |
| `internal/api/router.go` | **Modify** | Add `/api/v1/traces/import` route |
| `internal/receiver/otlp.go` | **Modify** | Fix JSON handler: use `protojson.Unmarshal` for `application/json` |
| `web/src/api/client.ts` | **Modify** | Add `importTraces` function and `ImportResult` type |
| `web/src/views/TraceList.vue` | **Modify** | Add Import button, file picker, import logic, result toast |
| `web/src/i18n/locales/en.ts` | **Modify** | Add import-related i18n keys |
| `web/src/i18n/locales/zh.ts` | **Modify** | Add import-related i18n keys |

## Testing Strategy

### Backend tests (`internal/api/`)

- **TestExportFormat**: Export a trace, verify JSON is valid protojson with `intValue` for numeric attributes, hex-encoded traceId/spanId
- **TestImportRoundTrip**: Export a trace → import the exported JSON → verify imported trace matches original
- **TestImportSkipsExisting**: Import a trace that already exists → verify skipped count
- **TestImportStandardOTLP**: Import a standard OTLP JSON (from another tool) with base64 traceId → verify correct parsing
- **TestImportInvalidJSON**: Send malformed JSON → verify 400 error
- **TestImportTooLarge**: Send >10MB body → verify 400 error

### Frontend

- Manual test: Import button visible, file picker works, result notification appears, list refreshes after import
- TypeScript type check passes (`vue-tsc --noEmit`)
