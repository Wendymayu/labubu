# Trace OTLP JSON Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OTLP JSON format export endpoint and update the frontend download button to support both JSON and OTLP formats.

**Architecture:** Backend: new `otlp_trace.go` with OTLP JSON struct types and a conversion function that maps the internal `TraceDetail` to OTLP `ResourceSpans` format. The existing `GetTrace` handler checks for `?format=otlp`. Frontend: TraceDetail.vue download button changed to a small dropdown with two options (JSON / OTLP JSON).

**Tech Stack:** Go (encoding/json), Vue 3, vanilla fetch

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/api/otlp_trace.go` (create) | OTLP JSON struct types + conversion function |
| `internal/api/trace_handler.go` (modify) | Add `?format=otlp` parameter handling |
| `web/src/views/TraceDetail.vue` (modify) | Download button → format dropdown + OTLP fetch |

---

### Task 1: Backend OTLP Conversion + Endpoint

**Files:**
- Create: `internal/api/otlp_trace.go`
- Modify: `internal/api/trace_handler.go`

- [ ] **Step 1: Create `internal/api/otlp_trace.go`**

```go
package api

import (
	"encoding/json"
	"fmt"
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

	ospan := otlpSpan{
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

	if ospan.ParentSpanID == "" {
		ospan.ParentSpanID = "" // keep empty string, OTLP allows empty for root
	}

	return ospan
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
		return 1 // default to INTERNAL
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
		attrs := mapFromEventAttrs(m["attributes"])

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
		attrs := mapFromEventAttrs(m["attributes"])

		result = append(result, otlpLink{
			TraceID:                traceID,
			SpanID:                 spanID,
			Attributes:             mapToKeyValues(attrs),
			DroppedAttributesCount: 0,
		})
	}
	return result
}

func mapFromEventAttrs(attrsRaw interface{}) map[string]string {
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
```

Note: The `writeOTLPResponse` function needs `net/http`. Add the import at the top of the file or use the existing handler conventions.

Actually, update the file to include the http import:

The file needs: `"encoding/json"`, `"fmt"`, `"net/http"`, `"strconv"`, and `"github.com/labubu/labubu/internal/storage"`.

- [ ] **Step 2: Modify `internal/api/trace_handler.go` — add format parameter**

In `GetTrace`, after successfully fetching the trace detail and before writing the response, add a format check. Replace:

```go
writeJSON(w, http.StatusOK, map[string]interface{}{"trace": detail})
```

With:

```go
if r.URL.Query().Get("format") == "otlp" {
    writeOTLPResponse(w, detail)
    return
}
writeJSON(w, http.StatusOK, map[string]interface{}{"trace": detail})
```

- [ ] **Step 3: Build and verify**

```bash
cd /d/opensource/github/labubu && go build -o labubu.exe ./cmd/labubu
```

Expected: build succeeds with no errors.

- [ ] **Step 4: Commit**

```bash
cd /d/opensource/github/labubu
git add internal/api/otlp_trace.go internal/api/trace_handler.go
git commit -m "feat: add OTLP JSON format export endpoint for traces"
```

---

### Task 2: Frontend Format Selector

**Files:**
- Modify: `web/src/views/TraceDetail.vue`

- [ ] **Step 1: Replace the download button with a format dropdown**

Replace the single download button:

```html
<button class="btn-download" @click="downloadTrace" title="Download trace as JSON">Download</button>
```

With a button group:

```html
<div class="download-group">
  <button class="btn-download" @click="downloadTraceJSON" title="Download as internal JSON">JSON</button>
  <button class="btn-download" @click="downloadTraceOTLP" title="Download as OTLP JSON (importable to Jaeger/Grafana)">OTLP</button>
</div>
```

- [ ] **Step 2: Update the download functions**

Replace the single `downloadTrace` function with two functions:

```typescript
function downloadTraceJSON() {
  if (!trace.value) return
  downloadBlob(JSON.stringify(trace.value, null, 2), `trace-${traceIdHex}.json`)
}

async function downloadTraceOTLP() {
  try {
    const res = await fetch(`/api/v1/traces/${traceIdHex}?format=otlp`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const json = await res.json()
    downloadBlob(JSON.stringify(json, null, 2), `trace-${traceIdHex}-otlp.json`)
  } catch (e: any) {
    alert(`OTLP download failed: ${e.message}`)
  }
}

function downloadBlob(content: string, filename: string) {
  const blob = new Blob([content], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
```

- [ ] **Step 3: Update CSS**

Replace `.btn-download` with styles for the button group:

```css
.download-group {
  display: flex;
  gap: 4px;
  align-self: center;
}
.btn-download {
  padding: 6px 12px;
  border: 1px solid #333;
  background: #111;
  color: #94a3b8;
  border-radius: 6px;
  cursor: pointer;
  font-size: 12px;
}
.btn-download:first-child {
  border-radius: 6px 0 0 6px;
}
.btn-download:last-child {
  border-radius: 0 6px 6px 0;
}
.btn-download:hover {
  border-color: #38bdf8;
  color: #38bdf8;
}
```

- [ ] **Step 4: Build and verify**

```bash
cd /d/opensource/github/labubu/web && npm run build
```

Expected: build succeeds.

- [ ] **Step 5: Commit**

```bash
cd /d/opensource/github/labubu
git add web/src/views/TraceDetail.vue web/dist/
git commit -m "feat: add OTLP format option to trace download button"
```

---

## Verification Checklist

1. Open trace detail page → "JSON" and "OTLP" buttons visible
2. Click "JSON" → downloads `trace-{id}.json` (internal format, as before)
3. Click "OTLP" → downloads `trace-{id}-otlp.json` in OTLP ResourceSpans format
4. OTLP file has correct structure: resourceSpans[].scopeSpans[].spans[]
5. Timestamps in nanoseconds, span kind as integer, attributes as key-value objects
6. Zero-length arrays for attributes/events/links (not null)
