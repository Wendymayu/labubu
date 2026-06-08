# LLM Model Configuration Design Spec

> **Goal:** Allow users to configure multiple LLM models with API keys for future trace analysis, with one model set as the active default.

> **Status:** ✅ Approved (2026-06-08)

---

## Architecture

New independent module (`llm-configs`) alongside existing `model-pricing`. Follows the exact same patterns as Pricing: Go handler with `ServeHTTP` dispatch, `Store` interface methods, memstore map + chDB `ReplacingMergeTree` table, Vue 3 page under Settings. API Key is stored in plaintext but masked on read.

**Key principle:** LLM configs and Model Pricing are completely independent. Pricing will later become read-only (YAML-driven). LLM configs are user-managed via UI.

---

## Data Model

### Go (internal/storage)

```go
// LlmConfig holds configuration for a single LLM model used for trace analysis.
type LlmConfig struct {
    Id          string  `json:"id"`
    ModelName   string  `json:"model_name"`
    ProviderURL string  `json:"provider_url"`
    ApiKey      string  `json:"api_key"`    // plaintext at rest, masked on GET
    IsDefault   bool    `json:"is_default"`
    Temperature float64 `json:"temperature"` // default 0.7
    MaxTokens   int     `json:"max_tokens"`  // default 4096
}
```

### TypeScript (vue)

```ts
interface LlmConfig {
  id: string
  model_name: string
  provider_url: string
  api_key: string        // masked by backend: "sk-a***3x" or "***"
  is_default: boolean
  temperature: number    // default 0.7
  max_tokens: number     // default 4096
}
```

### Default values

| Field       | Default |
|-------------|---------|
| temperature | 0.7     |
| max_tokens  | 4096    |

### API Key masking rules

- Length ≤ 8: return `"***"`
- Length > 8: return `first_3_chars***last_2_chars` (e.g. `"sk-a***3x"`)

---

## API Design

Base path: `/api/v1/llm-configs`

| Method | Path                        | Action   | Notes                                      |
|--------|-----------------------------|----------|--------------------------------------------|
| GET    | `/api/v1/llm-configs`       | List all | `api_key` is masked in response            |
| POST   | `/api/v1/llm-configs`       | Create   | `model_name`, `provider_url`, `api_key` required |
| PUT    | `/api/v1/llm-configs/{id}`  | Update   | `api_key` `"***"` means retain existing    |
| DELETE | `/api/v1/llm-configs/{id}`  | Delete   |                                            |

### Business rules

- **Default uniqueness:** Setting `is_default: true` on any model automatically sets all others to `false`. Only one default can exist.
- **API Key retention:** If the update payload has `api_key: "***"`, the backend keeps the stored key unchanged. Otherwise it updates.
- **Delete default:** No special restriction. If the default is deleted, no model is default until the user explicitly sets one.

---

## Storage Layer

### Store interface (additions)

```go
GetLlmConfigs(ctx context.Context) ([]LlmConfig, error)
CreateLlmConfig(ctx context.Context, c *LlmConfig) error
UpdateLlmConfig(ctx context.Context, c *LlmConfig) error
DeleteLlmConfig(ctx context.Context, id string) error
```

### memstore

```go
type memStore struct {
    // ... existing fields ...
    llmConfigs map[string]LlmConfig  // keyed by id
}
```

### chDB

```sql
CREATE TABLE IF NOT EXISTS llm_configs (
    id           String,
    model_name   String,
    provider_url String,
    api_key      String,
    is_default   UInt8,
    temperature  Float64,
    max_tokens   Int32,
    updated_at   DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY id
```

Uses `ReplacingMergeTree` for upsert semantics — same pattern as model_pricing table.

---

## Frontend

### Route

- `/settings/llm-configs` → `LlmConfig.vue`

### Navigation (App.vue sidebar)

Under the existing Settings collapsible group:

```
Settings ▸
  Model Pricing
  LLM Configs       ← new
```

### Page layout (LlmConfig.vue)

Table with columns:

| Model Name | Provider URL | API Key | Default | Temp | Max Tokens | Actions |

- **Default column:** Shows ★ for active default, "Set Default" button for others
- **"+ Add Model"** button above the table
- **Form:** modal overlay with all fields
- **Delete confirmation:** "This is the active default model. Delete anyway?" if deleting the default

### API client (client.ts)

New functions:
```ts
listLlmConfigs(): Promise<{ configs: LlmConfig[] }>
createLlmConfig(config: Omit<LlmConfig, 'id'>): Promise<LlmConfig>
updateLlmConfig(id: string, config: LlmConfig): Promise<LlmConfig>
deleteLlmConfig(id: string): Promise<void>
```

---

## Files to create/modify

### Create
- `internal/api/llm_config_handler.go` — HTTP handler
- `web/src/views/LlmConfig.vue` — Settings page

### Modify
- `internal/api/router.go` — register `/api/v1/llm-configs` routes
- `internal/storage/storage.go` — add `LlmConfig` struct + Store interface methods
- `internal/storage/memstore.go` — implement memstore methods
- `internal/storage/chdb.go` — implement chDB methods
- `internal/storage/chdb_query.go` — add SQL builders
- `web/src/api/client.ts` — add LlmConfig types + API functions
- `web/src/App.vue` — add nav link + route registration
- `web/src/router/index.ts` — add route (if separate file)

### Tests
- `internal/api/llm_config_handler_test.go` — handler tests

---

## Out of scope

- Encryption of API keys at rest
- Environment variable references for API keys
- Actual LLM trace analysis (this is config only, the prerequisite)
- Integration with Pricing model list (they remain independent)
