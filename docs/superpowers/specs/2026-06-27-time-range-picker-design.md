# Time-Range Picker for Traces / Sessions / Logs Pages

**Date:** 2026-06-27
**Status:** Approved (design)
**Scope:** Frontend-only. No Go/backend changes.

## Goal

Add custom time-range filtering to the Traces (链路), Sessions (会话), and Logs (日志) list pages, styled after the cost page's existing period selector. Extract a shared `TimeRangePicker` component and refactor the cost page to use it too.

## Background — the backend is already complete

Investigation confirms the entire backend chain already supports `start`/`end` (epoch ms) time filtering for all three list endpoints. No Go changes are required.

| Layer | traces | sessions | logs |
|---|---|---|---|
| API client (`web/src/api/client.ts`) | `TraceQuery.start/end` → `listTraces` | `SessionQuery.start/end` → `listSessions` | `LogQuery.start/end` → `listLogs` |
| HTTP handler | `trace_handler.go:51` parses `start`/`end` → `StartTimeMS/EndTimeMS` | `session_handler.go:34` | `log_handler.go:157` → `StartTime/EndTime` |
| SQLite (default non-CGO) | `buildSqliteTraceWhereClause` (`sqlite_store.go:1601`) | `buildSqliteSessionWhereClause` (`:1637`) | `buildSqliteLogWhereClause` (`:1677`) |
| chDB (CGO) | `chdb_query.go:155` | `chdb_query.go:242` | `chdb_query.go:388` |
| memstore (fallback) | `memstore.go:344` | `memstore.go:522` | `memstore.go:205` |

The only missing piece is the **UI**: the three list pages have no time-range selector. The cost page has one, but it is inline markup inside `CostDashboard.vue`, not a reusable component.

## Decisions (user-confirmed)

1. **Default state = "Today".** The three list pages currently default to all-time data. After this change they default to "Today"; an "All" preset switches back to all-time. This intentionally changes the out-of-box behavior of the three pages (user accepted this).
2. **Shared component, cost page refactored.** Build one `TimeRangePicker` component; refactor `CostDashboard.vue` to use it. Single source of truth, consistent styling across all four pages.
3. **Time picker placement:** on its own line **above** the existing filter bar (matches the cost page's `.period-bar`; avoids crowding the already-busy filter bars).
4. **Empty-state hint:** when a list is empty under an active (non-"All") time filter, show a hint pointing the user to the "All" preset.

## Design

### Component: `web/src/components/TimeRangePicker.vue`

Extracts the cost page's inline period-bar logic (`toLocalInputValue`, the custom-range seeding inside `setPeriod`, `onCustomChange`) into a self-contained component.

**Props**
- `showAll?: boolean` (default `false`) — whether to render the "All" preset button. The cost page passes `false` (keeps `today / 7d / 30d / custom`); the three list pages pass `true` (`all / today / 7d / 30d / custom`).

**Emit**
- `change` → payload `{ period: string; start?: number; end?: number }`:
  - `all` → `{ period: 'all' }` (start/end omitted → caller sends no time params → unfiltered)
  - `today` → `{ period: 'today', start: <local midnight today>, end: <now> }`
  - `7d` → `{ period: '7d', start: <now - 7d>, end: <now> }`
  - `30d` → `{ period: '30d', start: <now - 30d>, end: <now> }`
  - `custom` → `{ period: 'custom', start: <customStart ms>, end: <customEnd ms> }`

**Behavior**
- Default selected preset: `today`.
- **Emits `change` on initial mount** (with the default `today` selection, including computed `start`/`end`). This is the trigger parents use for their first fetch, so the default "Today" range is applied immediately. Vue mounts children before parents, so the mount emit fires before the parent's `onMounted` — parents therefore **remove** their now-redundant onMounted list-fetch (see per-page changes) and rely on this emit. The component is always rendered (no `v-if`), so the mount emit is guaranteed.
- Switching to `custom` seeds `customStart`/`customEnd` from the current preset (same logic as the current cost page `setPeriod('custom')`).
- `datetime-local` inputs with a localized "to" label between them; both `@change` trigger an emit.
- Validates `start < end` for custom; on violation, emits nothing and surfaces an `invalidRange` message inline (mirrors cost page).
- Scoped styles carry `.period-bar`, `.btn-preset`, `.custom-range` (moved from `CostDashboard.vue`).

### Key interface detail — period vs. explicit range

The cost endpoint and the list endpoints consume time differently:
- **cost endpoint** receives a `period` string and computes the window **server-side**; `start`/`end` only override when custom.
- **list endpoints** receive only `start`/`end` epoch ms and do **not** accept a `period` param.

The component computes `start`/`end` for every selection and emits them all. Each caller uses what its API needs:
- **Cost page:** `getCostSummary(period, groupBy, period === 'custom' ? { start, end } : undefined)` — presets still send `period` only (server computes window), custom sends the range. **Cost page behavior is unchanged.**
- **List pages:** `listTraces({ ..., start, end })` (and the sessions/logs equivalents) — for `all`, both are `undefined` and omitted by the `get()` helper → no time filter.

### Per-page changes

**TraceList.vue / SessionList.vue / LogList.vue**
- Add `<TimeRangePicker showAll @change="onTimeChange" />` on its own line above the `.filters` / `.log-toolbar`.
- Store `timeRange` (`{ period, start?, end? }`) in component state; `onTimeChange` updates it, resets to page 1, and refetches.
- Pass `start`/`end` into the existing `listTraces` / `listSessions` / `listLogs` calls.
- `reset()` resets the time range back to "Today" (default) in addition to clearing text/service/status filters.
- **Remove the redundant onMounted list-fetch** (`fetchTraces()` / `fetchSessions()` / `fetchLogs()`); the component's mount emit now triggers the first fetch with the default "Today" range. Keep ancillary onMounted calls (`fetchServices`, `fetchEventNames`).
- Empty-state: when the list is empty **and** `period !== 'all'`, append a hint (e.g. `timeRange.emptyHint`) suggesting switching to "All".

**CostDashboard.vue**
- Replace the inline `.period-bar` template block with `<TimeRangePicker @change="onTimeChange" />`.
- Remove the now-redundant local state (`activePeriod`, `customStart`, `customEnd`) and helpers (`toLocalInputValue`, `onCustomChange`, `setPeriod`) — their logic moves into the component.
- `onTimeChange({ period, start, end })` → `fetchData` calls `getCostSummary(period, groupBy, period === 'custom' ? { start, end } : undefined)`.
- **Remove the `fetchData()` call from `onMounted`** (the component's mount emit triggers it); keep `checkPricing()`.
- **Keep** a `.btn-preset` style rule in `CostDashboard.vue` scoped styles for the breakdown toggle (byModel / byService buttons still use `btn-preset`). The component has its own scoped `.btn-preset`; scoped styles do not collide.

### i18n

Add a new `timeRange` section to **both** `web/src/i18n/locales/en.ts` and `zh.ts`:
- `all`, `today`, `7d`, `30d`, `custom`, `to`, `invalidRange`, `emptyHint`

Migrate `CostDashboard.vue` template references from `costDashboard.today / 7d / 30d / custom / to / invalidRange` to `timeRange.*`, and remove those six duplicate keys from the `costDashboard` section. Cost-specific keys (`totalCost`, etc.) remain in `costDashboard`.

## Testing / Verification

- `cd web && npx vue-tsc --noEmit` — strict TypeScript check (CLAUDE.md mandates no `any`).
- No Go changes; Go tests unaffected and skipped.
- Manual: `go run -tags dev ./cmd/labubu serve`, verify on the four pages:
  - Default selection is "Today"; list shows only today's data.
  - Switch All / 7d / 30d / Custom → results update, pagination resets to page 1.
  - Custom datetime inputs seed from the previous preset; invalid range (start > end) shows the error and does not fetch.
  - Empty list under a non-All filter shows the hint.
  - Cost page: behavior identical to before (presets send `period`, custom sends range, breakdown toggle still styled).

## Out of scope

- No new backend endpoints, no Go changes.
- No additional preset granularities (1h / 6h etc.) — preset set matches the cost page plus "All".
- No persistence of the selected time range across page navigation or reloads (state is per-page, like the existing filter text).
