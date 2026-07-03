# Attribute Key Normalization Design

## Problem

Popular AI agents (Claude Code, Codex, OpenInference-based tools) use non-standard attribute key names that differ from the OpenTelemetry GenAI semantic conventions (`gen_ai.usage.*`, `gen_ai.request.*`). Labubu currently hardcodes the standard OTel keys when extracting typed fields (tokens, model name, session ID), causing these agents' data to appear as NULL in trace list columns, empty token summaries, and missing session associations.

**Current data from Claude Code:**

| What's sent | What Labubu looks for | Result |
|-------------|----------------------|--------|
| `input_tokens` | `gen_ai.usage.input_tokens` | NULL — no typed column, no token summary |
| `output_tokens` | `gen_ai.usage.output_tokens` | NULL |
| `session.id` | `jiuwenclaw.session.id` | Session list empty for this agent |
| `model` (varies) | `gen_ai.request.model` | NULL |

Raw attributes are always preserved in the `attributes` map, so data is not lost — but the typed extraction and UI features break.

## Design

### Core idea: data-driven attribute alias mapping

A single mapping table defines, for each standard key, a list of fallback key names to try. When normalizing, if the standard key is absent but a fallback key exists, the fallback's value is **copied** to the standard key in the attributes map. Original keys are preserved untouched.

This is a pure data structure + a generic loop — no agent-specific if-else logic. Adding support for a new agent only requires appending its key names to the relevant fallback lists.

### Mapping table

```go
// attrAliases maps a standard OTel attribute key to fallback keys
// that various agents use instead. If the standard key is absent but
// a fallback key exists, the fallback value is copied to the standard key.
var attrAliases = map[string][]string{
    // Token usage (GenAI semconv v1.27+ → Claude Code → OpenInference)
    "gen_ai.usage.input_tokens":  {"input_tokens",  "llm.usage.input_tokens"},
    "gen_ai.usage.output_tokens": {"output_tokens", "llm.usage.output_tokens"},
    "gen_ai.usage.total_tokens":  {"total_tokens",  "llm.usage.total_tokens"},
    // Request model
    "gen_ai.request.model":       {"model", "llm.request.model"},
    // Session ID (JiuwenClaw → Claude Code → Codex)
    "jiuwenclaw.session.id":      {"session.id", "codex.session.id"},
}
```

### Normalization function

```go
// normalizeAttributes copies values from fallback keys to standard keys
// when the standard key is absent. Original keys are preserved.
// This makes downstream extraction (typed columns, session, etc.)
// work regardless of which agent produced the data.
func normalizeAttributes(attrs map[string]string) {
    for stdKey, fallbacks := range attrAliases {
        if _, exists := attrs[stdKey]; exists {
            continue // standard key already present, no alias needed
        }
        for _, fb := range fallbacks {
            if v, exists := attrs[fb]; exists {
                attrs[stdKey] = v
                break // first matching fallback wins
            }
        }
    }
}
```

### Placement in the pipeline

**Where:** `internal/receiver/otlp.go`, inside `TranslateSpan`, immediately after `keyValueToMap(ps.Attributes)`.

```go
attrs := keyValueToMap(ps.Attributes)
normalizeAttributes(attrs) // ← add this line
```

This is the single entry point for all ingested spans (gRPC, HTTP, and import). All downstream consumers — typed column extraction (`getUint32Attr`, `getStringAttr`), session ID extraction (`jiuwenclaw.session.id`), and UI components — automatically benefit because the standard keys are now populated.

### Session ID extraction in storage

The `jiuwenclaw.session.id` key in `internal/storage/chdb_query.go:512` and `internal/storage/sqlite_store.go` (aggregateTraces) already works after normalization — Claude Code's `session.id` is copied to `jiuwenclaw.session.id` by the normalize step, so the existing extraction code works without modification.

### Context window pie chart (frontend)

The `gen_ai.context.*_tokens` keys are non-standard extensions. Claude Code sends `cache_creation_tokens` and `cache_read_tokens` instead. The pie chart currently hardcodes six specific keys.

**Fix:** In `web/src/views/TraceDetail.vue` and `web/src/components/TokenPieChart.vue`, add fallback patterns to the CTX_PATTERNS array. For each pattern, check both the standard key and known fallback keys:

```typescript
const CTX_PATTERNS: { patterns: string[]; label: string }[] = [
  { patterns: ['gen_ai.context.system_prompt',       'system_prompt_tokens'],  label: 'System' },
  { patterns: ['gen_ai.context.assistant_messages',  'assistant_messages_tokens'], label: 'Assistant History' },
  { patterns: ['gen_ai.context.user_messages',       'user_messages_tokens'],  label: 'User' },
  { patterns: ['gen_ai.context.tool_results',        'tool_results_tokens'],   label: 'Tool Results' },
  { patterns: ['gen_ai.context.tool_definitions',    'tool_definitions_tokens'], label: 'Tool Definitions' },
  { patterns: ['gen_ai.context.skill',               'skill_tokens'],          label: 'Skill' },
]
```

Each entry now has a `patterns` array (standard key first, then fallbacks). The lookup function tries each pattern in order and returns the first match.

### SpanDetail attribute grouping (frontend)

In `web/src/components/SpanDetail.vue`, the attribute grouping logic uses prefix matching. Add `llm.` prefix to the "Gen AI" group so OpenInference-style attributes get proper categorization.

## File Map

| File | Action | Description |
|------|--------|-------------|
| `internal/receiver/otlp.go` | Modify | Add `attrAliases` map, `normalizeAttributes` function, call it in `translateSpan` |
| `internal/storage/chdb_query.go` | No change | Session extraction already uses `jiuwenclaw.session.id` which is now populated by normalization |
| `web/src/views/TraceDetail.vue` | Modify | Change CTX_PATTERNS to use `patterns` array with fallbacks |
| `web/src/components/TokenPieChart.vue` | Modify | Change CTX_PATTERNS to use `patterns` array with fallbacks |
| `web/src/components/SpanDetail.vue` | Modify | Add `llm.` prefix to Gen AI attribute group |

## Testing Strategy

### Backend

- **TestNormalizeAttributes**: Table-driven test verifying each alias mapping (Claude Code keys, OpenInference keys, standard keys already present)
- **TestNormalizeNoOverwrite**: Standard key already present → fallback ignored
- **TestTokenExtractionAfterNormalize**: Send spans with `input_tokens` attribute → verify `getUint32Attr("gen_ai.usage.input_tokens")` returns the value

### Frontend

- Verify pie chart renders with Claude Code `cache_*` keys (manual test)
- Verify `llm.*` attributes appear in "Gen AI" group in SpanDetail (manual test)
- TypeScript type check passes

## Scope

This design covers the normalization layer only. It does NOT cover:
- Dynamic/YAML-configurable alias tables (future enhancement)
- Making the context window pie chart fully dynamic (pattern scanning across all `*_tokens` keys)
- Normalization on the export side (export should preserve original keys)
