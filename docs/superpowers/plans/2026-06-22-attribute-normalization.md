# Attribute Key Normalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a data-driven attribute key normalization layer so that non-standard keys from agents like Claude Code are automatically mapped to standard OTel keys, making token columns, session tracking, and pie charts work for all agents.

**Architecture:** A single `attrAliases` mapping table + `normalizeAttributes` function inserted in the receiver pipeline. Frontend CTX_PATTERNS arrays gain fallback key lists. SpanDetail grouping adds `llm.` prefix.

**Tech Stack:** Go (data-driven alias table), Vue 3 (TypeScript, pattern arrays)

---

### Task 1: Add normalizeAttributes to receiver pipeline

**Files:**
- Modify: `internal/receiver/otlp.go:438` (translateSpan function, after keyValueToMap)
- Modify: `internal/receiver/otlp_test.go` (add normalization tests)

- [ ] **Step 1: Write the failing test for normalizeAttributes**

Add to `internal/receiver/otlp_test.go` (new file created in Task 2 of the previous plan, already has memTestStore):

```go
func TestNormalizeAttributes(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]string
		expect map[string]string
	}{
		{
			name: "Claude Code keys → standard keys",
			input: map[string]string{
				"input_tokens":       "100",
				"output_tokens":      "50",
				"session.id":         "abc-123",
			},
			expect: map[string]string{
				"input_tokens":                    "100",
				"output_tokens":                   "50",
				"session.id":                      "abc-123",
				"gen_ai.usage.input_tokens":      "100",
				"gen_ai.usage.output_tokens":     "50",
				"jiuwenclaw.session.id":           "abc-123",
			},
		},
		{
			name: "standard keys already present → no alias",
			input: map[string]string{
				"gen_ai.usage.input_tokens": "200",
				"input_tokens":              "100",
			},
			expect: map[string]string{
				"gen_ai.usage.input_tokens": "200",
				"input_tokens":              "100",
			},
		},
		{
			name: "OpenInference llm.* keys",
			input: map[string]string{
				"llm.usage.input_tokens":  "300",
				"llm.usage.output_tokens": "150",
				"llm.request.model":       "gpt-4o",
			},
			expect: map[string]string{
				"llm.usage.input_tokens":       "300",
				"llm.usage.output_tokens":      "150",
				"llm.request.model":            "gpt-4o",
				"gen_ai.usage.input_tokens":    "300",
				"gen_ai.usage.output_tokens":   "150",
				"gen_ai.request.model":         "gpt-4o",
			},
		},
		{
			name: "empty attributes → no changes",
			input:  map[string]string{},
			expect: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := make(map[string]string, len(tt.input))
			for k, v := range tt.input {
				attrs[k] = v
			}
			normalizeAttributes(attrs)
			for k, v := range tt.expect {
				if attrs[k] != v {
					t.Errorf("key %q: got %q, want %q", k, attrs[k], v)
				}
			}
			// Verify no extra keys were added beyond what's expected
			for k := range attrs {
				if _, ok := tt.expect[k]; !ok {
					t.Errorf("unexpected key %q added", k)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/receiver/ -run TestNormalizeAttributes -tags '!local_engine'`
Expected: FAIL — `normalizeAttributes` function doesn't exist yet.

- [ ] **Step 3: Implement normalizeAttributes**

In `internal/receiver/otlp.go`, add the alias table and normalization function. Place them near the `keyValueToMap` function (around line 453):

```go
// attrAliases maps a standard OTel attribute key to fallback keys
// that various agents use instead. If the standard key is absent but
// a fallback key exists, the fallback value is copied to the standard key.
// Adding a new agent only requires appending its key names to the fallback lists.
var attrAliases = map[string][]string{
	// Token usage (GenAI semconv → Claude Code → OpenInference)
	"gen_ai.usage.input_tokens":  {"input_tokens", "llm.usage.input_tokens"},
	"gen_ai.usage.output_tokens": {"output_tokens", "llm.usage.output_tokens"},
	"gen_ai.usage.total_tokens":  {"total_tokens", "llm.usage.total_tokens"},
	// Request model
	"gen_ai.request.model":       {"model", "llm.request.model"},
	// Session ID (JiuwenClaw → Claude Code → Codex)
	"jiuwenclaw.session.id":      {"session.id", "codex.session.id"},
}

// normalizeAttributes copies values from fallback keys to standard keys
// when the standard key is absent. Original keys are preserved.
func normalizeAttributes(attrs map[string]string) {
	for stdKey, fallbacks := range attrAliases {
		if _, exists := attrs[stdKey]; exists {
			continue
		}
		for _, fb := range fallbacks {
			if v, exists := attrs[fb]; exists {
				attrs[stdKey] = v
				break
			}
		}
	}
}
```

- [ ] **Step 4: Insert normalizeAttributes call in translateSpan**

In `internal/receiver/otlp.go`, the `translateSpan` function (around line 438) creates the attributes map via `keyValueToMap(ps.Attributes)` and assigns it directly to `storage.Span{...Attributes: keyValueToMap(ps.Attributes)}`. 

Change line 438 from:
```go
Attributes:        keyValueToMap(ps.Attributes),
```
To:
```go
Attributes:        normalizeAttributes(keyValueToMap(ps.Attributes)),
```

This is a single-line change. The `normalizeAttributes` function mutates the map in-place and also returns it for convenience (chaining). Update the function to return the map:

```go
func normalizeAttributes(attrs map[string]string) map[string]string {
	for stdKey, fallbacks := range attrAliases {
		if _, exists := attrs[stdKey]; exists {
			continue
		}
		for _, fb := range fallbacks {
			if v, exists := attrs[fb]; exists {
				attrs[stdKey] = v
				break
			}
		}
	}
	return attrs
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -v ./internal/receiver/ -tags '!local_engine'`
Expected: All tests pass including `TestNormalizeAttributes`.

- [ ] **Step 6: Commit**

```bash
git add internal/receiver/otlp.go internal/receiver/otlp_test.go
git commit -m "feat: add attribute key normalization layer for multi-agent compatibility"
```

---

### Task 2: Update frontend context window patterns

**Files:**
- Modify: `web/src/views/TraceDetail.vue:217-241` (CTX_PATTERNS and lookup logic)
- Modify: `web/src/components/TokenPieChart.vue:86-92` (KEY_PATTERNS)

- [ ] **Step 1: Update TraceDetail.vue CTX_PATTERNS and lookup**

Change the `CTX_PATTERNS` type and data from `{ key: string; label: string }` to `{ patterns: string[]; label: string }`, and update the lookup loop.

Replace the CTX_PATTERNS definition (lines 218-225):

```typescript
/** Context-window token breakdown, with fallback patterns for multi-agent compatibility. */
const CTX_PATTERNS: { patterns: string[]; label: string }[] = [
  { patterns: ['gen_ai.context.system_prompt',       'system_prompt_tokens'],  label: 'System' },
  { patterns: ['gen_ai.context.assistant_messages',  'assistant_messages_tokens'], label: 'Assistant History' },
  { patterns: ['gen_ai.context.user_messages',       'user_messages_tokens'],  label: 'User' },
  { patterns: ['gen_ai.context.tool_results',        'tool_results_tokens'],   label: 'Tool Results' },
  { patterns: ['gen_ai.context.tool_definitions',    'tool_definitions_tokens'], label: 'Tool Definitions' },
  { patterns: ['gen_ai.context.skill',               'skill_tokens'],          label: 'Skill' },
]
```

Replace the lookup loop in `selectedSpanTokenSlices` (lines 232-238):

```typescript
  for (const { patterns, label } of CTX_PATTERNS) {
    let raw: string | undefined
    for (const p of patterns) {
      raw = attrs[p]
      if (raw) break
    }
    if (!raw) continue
    const n = parseInt(raw, 10)
    if (isNaN(n) || n <= 0) continue
    slices.push({ name: label, tokens: n })
  }
```

- [ ] **Step 2: Update TokenPieChart.vue KEY_PATTERNS**

Replace the `KEY_PATTERNS` definition (lines 86-93):

```typescript
const KEY_PATTERNS: { patterns: string[]; key: string; label: string }[] = [
  { patterns: ['gen_ai.context.system_prompt',       'system_prompt_tokens'],  key: 'system',           label: 'System' },
  { patterns: ['gen_ai.context.assistant_messages',  'assistant_messages_tokens'], key: 'assistant',        label: 'Assistant History' },
  { patterns: ['gen_ai.context.user_messages',       'user_messages_tokens'],  key: 'user',             label: 'User' },
  { patterns: ['gen_ai.context.tool_results',        'tool_results_tokens'],   key: 'tool',             label: 'Tool Results' },
  { patterns: ['gen_ai.context.tool_definitions',    'tool_definitions_tokens'], key: 'tool_definitions', label: 'Tool Definitions' },
  { patterns: ['gen_ai.context.skill',               'skill_tokens'],          key: 'skill',            label: 'Skill' },
]
```

The `TokenPieChart` component receives `items: PieSlice[]` as props, where each slice has `{ name: label, tokens: n }`. The `KEY_PATTERNS` is used in the `segments` computed to match `props.items` by `label`. Since the lookup in `TraceDetail.vue` already resolves the fallback patterns to a label, the pie chart doesn't need to do its own pattern lookup — it just matches by label. No further changes needed in TokenPieChart's `segments` computed.

- [ ] **Step 3: Run TypeScript type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS — the `PieSlice` interface has `{ name: string; tokens: number }`, so the label-based lookup still works.

- [ ] **Step 4: Commit**

```bash
git add web/src/views/TraceDetail.vue web/src/components/TokenPieChart.vue
git commit -m "feat: add fallback patterns to context window token pie chart for multi-agent compatibility"
```

---

### Task 3: Add llm. prefix to SpanDetail attribute grouping

**Files:**
- Modify: `web/src/components/SpanDetail.vue:138` (GROUP_RULES)

- [ ] **Step 1: Add llm. prefix to Gen AI group**

Change line 138:
```typescript
{ name: 'Gen AI', prefixes: ['gen_ai.'], defaultExpanded: true },
```
To:
```typescript
{ name: 'Gen AI', prefixes: ['gen_ai.', 'llm.'], defaultExpanded: true },
```

- [ ] **Step 2: Run TypeScript type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add web/src/components/SpanDetail.vue
git commit -m "feat: add llm. prefix to Gen AI attribute group in SpanDetail"
```

---

### Task 4: Build and final verification

**Files:** None — verification only

- [ ] **Step 1: Run all Go tests**

Run: `go test -v ./internal/... -tags '!local_engine'`
Expected: All tests pass.

- [ ] **Step 2: Run TypeScript type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS

- [ ] **Step 3: Build the binary**

Run: `go build -tags '!local_engine' ./cmd/labubu/`
Expected: Build succeeds.

- [ ] **Step 4: Push all commits**

```bash
git push origin develop
```

- [ ] **Step 5: Manual smoke test**

1. Start Labubu: `./labubu serve`
2. Open http://localhost:8080 → trace list
3. Verify token columns now show values for Claude Code traces (was previously NULL)
4. Click a Claude Code trace → verify span detail shows token summary
5. Verify Session list now shows Claude Code sessions (was previously empty)
6. Verify `llm.*` attributes appear in "Gen AI" group in SpanDetail
