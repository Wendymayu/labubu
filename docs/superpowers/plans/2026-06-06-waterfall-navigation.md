# WaterfallChart 导航优化 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 优化 WaterfallChart 在大 Trace（数百 span）下的浏览体验：支持折叠子树、搜索 span name、快速过滤 LLM/Error/Tool 类型。

**Architecture:** 仅修改 WaterfallChart.vue 一个组件。在现有 computed `displaySpans` 基础上增加折叠状态管理（`collapsedParents` Set）、搜索过滤状态（`searchQuery` + `activeFilters`），通过改写 walk 函数实现子树折叠。Props/emits 不变，TraceDetail 等消费方无感知。

**Tech Stack:** Vue 3 Composition API（`<script setup lang="ts">`），CSS 变量主题系统

---

### Task 1: 子树折叠 — computed 数据与 displaySpans 重写

**Files:**
- Modify: `web/src/components/WaterfallChart.vue`（`<script setup>` 部分）

**背景说明：** 当前 displaySpans 通过 `childrenMap` + `walk` 函数将所有 span 平铺渲染。需要改造 walk 以支持折叠：当父节点在 `collapsedParents` Set 中时，跳过其子节点但标记 `_hasCollapsedChildren` 和 `_childCount`。

- [ ] **Step 1: 新增类型定义和状态变量**

在 `<script setup>` 的 `DisplaySpan` 接口和 computed 之间插入：

```typescript
// --- Collapse state ---
const collapsedParents = ref<Set<string>>(new Set())
const DEFAULT_EXPAND_DEPTH = 1
```

在 `DisplaySpan` 接口中增加两个字段：

```typescript
interface DisplaySpan extends SpanDetail {
  _depth: number
  _hasChildren: boolean
  _expanded: boolean
  _childCount: number           // 新增：直接子节点数量
  _hasCollapsedChildren: boolean // 新增：true = 有被折叠的子节点
}
```

- [ ] **Step 2: 初始化 collapsedParents**

在 `displaySpans` computed 开头，基于 `props.spans` 初始化折叠状态。每次 spans 变化时重建 collapsedParents，但保留用户手动展开/折叠的状态（用 ref 持久化）。简单起见：首次加载时将所有 depth > 1 且有子节点的 span 加入 collapsedParents。

```typescript
// 在 displaySpans computed 内部，build childrenMap 之后：

// 首次初始化折叠状态（基于 spans 引用判断是否是新 trace）
const spansChanged = !initDone.value
const childrenMap = new Map<string, SpanDetail[]>()
// ...现有构建 childrenMap 的逻辑...

// 计算每个 span 的直接子节点数
const childCountMap = new Map<string, number>()
for (const span of props.spans) {
  const parentKey = span.parent_span_id || '__root__'
  childCountMap.set(parentKey, (childCountMap.get(parentKey) || 0) + 1)
}
```

这里需要新增一个 ref 来检测 spans 是否变化（新 trace 加载）：

```typescript
const lastSpansRef = ref<SpanDetail[] | null>(null)
```

在 computed 开头判断：

```typescript
if (lastSpansRef.value !== props.spans) {
  // 新 trace，重置折叠状态
  collapsedParents.value = new Set()
  lastSpansRef.value = props.spans
}
```

- [ ] **Step 3: 重写 walk 函数支持折叠**

当前 walk 函数：
```typescript
function walk(parentId: string, depth: number) {
  const children = childrenMap.get(parentId) || []
  for (const span of children) {
    const hasChildren = childrenMap.has(span.span_id) && (childrenMap.get(span.span_id)?.length ?? 0) > 0
    result.push({ ...span, _depth: depth, _hasChildren: hasChildren, _expanded: true })
    if (hasChildren) { walk(span.span_id, depth + 1) }
  }
}
```

替换为：

```typescript
function walk(parentId: string, depth: number) {
  const children = childrenMap.get(parentId) || []
  for (const span of children) {
    const hasChildren = childrenMap.has(span.span_id) && (childrenMap.get(span.span_id)?.length ?? 0) > 0
    const childCount = childCountMap.get(span.span_id) || 0
    const isCollapsed = collapsedParents.value.has(span.span_id)

    // 首次加载：depth > DEFAULT_EXPAND_DEPTH 且有子节点 → 自动折叠
    if (hasChildren && depth >= DEFAULT_EXPAND_DEPTH) {
      collapsedParents.value.add(span.span_id)
    }

    result.push({
      ...span,
      _depth: depth,
      _hasChildren: hasChildren,
      _expanded: !isCollapsed,
      _childCount: childCount,
      _hasCollapsedChildren: hasChildren && isCollapsed,
    })

    if (hasChildren && !isCollapsed) {
      walk(span.span_id, depth + 1)
    }
  }
}
```

注意：首次加载后 collapsedParents 会被 walk 修改，这没问题因为 computed 会重新计算。但为了避免无限循环，需要在 walk 中先检查 collapsedParents 是否已有该节点，如果有则不覆盖（保留用户手动操作）。

修正：首次初始化逻辑应该放在 computed 开头而非 walk 内部。把初始化移到 computed 开头，walk 只读取 collapsedParents：

```typescript
const displaySpans = computed(() => {
  // --- 首次加载：初始化折叠状态 ---
  if (lastSpansRef.value !== props.spans) {
    collapsedParents.value = new Set()
    // 先构建 childrenMap 确定哪些有子节点
    const initChildrenMap = new Map<string, SpanDetail[]>()
    for (const span of props.spans) {
      const pk = span.parent_span_id || '__root__'
      if (!initChildrenMap.has(pk)) initChildrenMap.set(pk, [])
      initChildrenMap.get(pk)!.push(span)
    }
    // 折叠 depth >= DEFAULT_EXPAND_DEPTH 且有子节点的 span
    function markCollapsed(parentId: string, depth: number) {
      const children = initChildrenMap.get(parentId) || []
      for (const span of children) {
        const hasKids = initChildrenMap.has(span.span_id) && (initChildrenMap.get(span.span_id)?.length ?? 0) > 0
        if (hasKids && depth >= DEFAULT_EXPAND_DEPTH) {
          collapsedParents.value.add(span.span_id)
        }
        markCollapsed(span.span_id, depth + 1)
      }
    }
    markCollapsed('__root__', 0)
    lastSpansRef.value = props.spans
  }

  // --- 构建 childrenMap ---
  const childrenMap = new Map<string, SpanDetail[]>()
  const childCountMap = new Map<string, number>()
  for (const span of props.spans) {
    const parentKey = span.parent_span_id || '__root__'
    if (!childrenMap.has(parentKey)) childrenMap.set(parentKey, [])
    childrenMap.get(parentKey)!.push(span)
    childCountMap.set(parentKey, (childCountMap.get(parentKey) || 0) + 1)
  }

  // --- walk ---
  const result: DisplaySpan[] = []
  function walk(parentId: string, depth: number) {
    const children = childrenMap.get(parentId) || []
    for (const span of children) {
      const hasChildren = childrenMap.has(span.span_id) && (childrenMap.get(span.span_id)?.length ?? 0) > 0
      const childCount = childCountMap.get(span.span_id) || 0
      const isCollapsed = collapsedParents.value.has(span.span_id)
      result.push({
        ...span,
        _depth: depth,
        _hasChildren: hasChildren,
        _expanded: !isCollapsed,
        _childCount: childCount,
        _hasCollapsedChildren: hasChildren && isCollapsed,
      })
      if (hasChildren && !isCollapsed) {
        walk(span.span_id, depth + 1)
      }
    }
  }
  walk('__root__', 0)
  return result
})
```

- [ ] **Step 4: 实现 toggleExpand 函数**

替换当前的空函数：

```typescript
function toggleExpand(spanId: string) {
  if (collapsedParents.value.has(spanId)) {
    collapsedParents.value.delete(spanId)
  } else {
    collapsedParents.value.add(spanId)
  }
  // 触发 computed 重新计算
  collapsedParents.value = new Set(collapsedParents.value)
}
```

- [ ] **Step 5: 更新 template 中的折叠图标和子节点计数**

当前 template 中 `col-name` 部分：
```html
<span class="col-name" :style="{ paddingLeft: (span._depth * 20 + 8) + 'px' }">
  <span v-if="span._hasChildren" class="toggle-icon" @click.stop="toggleExpand(span.span_id)">{{ span._expanded ? '▼' : '▶' }}</span>
  <span :class="['kind-dot', kindDotClass(span.kind)]"></span>
  {{ span.name }}
  <span v-if="selectedSpanId === span.span_id" class="selected-marker">◀</span>
</span>
```

替换为（增加 `_hasCollapsedChildren` 计数显示，折叠/展开图标区分）：

```html
<span class="col-name" :style="{ paddingLeft: (span._depth * 20 + 8) + 'px' }">
  <span
    v-if="span._hasChildren"
    class="toggle-icon"
    @click.stop="toggleExpand(span.span_id)"
  >{{ span._expanded ? '▼' : '▶' }}</span>
  <span v-else class="toggle-icon toggle-placeholder"></span>
  <span :class="['kind-dot', kindDotClass(span.kind)]"></span>
  {{ span.name }}
  <span v-if="selectedSpanId === span.span_id" class="selected-marker">◀</span>
</span>
```

在 `col-duration` 后新增子节点计数：

```html
<span class="col-duration">{{ formatDuration(span.duration_ms) }}</span>
<span class="col-children">{{ span._hasCollapsedChildren ? `[${span._childCount}]` : '' }}</span>
<span class="col-tokens">
```

注意：需要新增 `col-children` 列的样式，在 `col-duration` 和 `col-tokens` 之间。

更新 waterall-header：

```html
<div class="waterfall-header">
  <span class="col-name">Name</span>
  <span class="col-timeline">Timeline</span>
  <span class="col-duration">Duration</span>
  <span class="col-children"></span>
  <span class="col-tokens">Tokens</span>
</div>
```

- [ ] **Step 6: 运行 TypeScript 检查**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: clean output, no errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/WaterfallChart.vue
git commit -m "feat: add subtree collapse/expand to WaterfallChart"
```

---

### Task 2: 工具栏 — 搜索框 + 快速过滤标签 + Expand/Collapse 按钮

**Files:**
- Modify: `web/src/components/WaterfallChart.vue`

- [ ] **Step 1: 新增工具栏状态变量**

在 `<script setup>` 中，Task 1 的状态变量之后插入：

```typescript
// --- Search & Filter ---
const searchQuery = ref('')
const activeFilters = ref<Set<string>>(new Set())  // 'llm' | 'error' | 'tool'
const FILTER_OPTIONS = [
  { key: 'llm', label: 'LLM' },
  { key: 'error', label: 'Error' },
  { key: 'tool', label: 'Tool' },
] as const
```

- [ ] **Step 2: 新增搜索过滤逻辑**

```typescript
const matchCount = computed(() => {
  let count = 0
  for (const span of props.spans) {
    if (matchesSearch(span) && matchesFilters(span)) count++
  }
  return count
})

function matchesSearch(span: SpanDetail): boolean {
  if (!searchQuery.value) return true
  return span.name.toLowerCase().includes(searchQuery.value.toLowerCase())
}

function matchesFilters(span: SpanDetail): boolean {
  if (activeFilters.value.size === 0) return true
  let match = false
  for (const f of activeFilters.value) {
    switch (f) {
      case 'llm':
        if (span.total_tokens && span.total_tokens > 0) match = true
        break
      case 'error':
        if (span.status === 'ERROR') match = true
        break
      case 'tool':
        if (span.kind === 'CLIENT' && span.name.toLowerCase().includes('tool')) match = true
        break
    }
  }
  return match
}

function toggleFilter(key: string) {
  const newFilters = new Set(activeFilters.value)
  if (newFilters.has(key)) {
    newFilters.delete(key)
  } else {
    newFilters.add(key)
  }
  activeFilters.value = newFilters
}

function clearFilters() {
  activeFilters.value = new Set()
}

// 搜索 debounce 在 template 中用 v-model + watchDebounce 实现
// 或直接用 v-model.lazy + 手动 debounce
let debounceTimer: ReturnType<typeof setTimeout> | null = null
function onSearchInput(value: string) {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    searchQuery.value = value
  }, 500)
}
```

- [ ] **Step 3: 在 displaySpans 中集成搜索匹配标记**

在 walk 中 push span 之前，增加搜索/过滤匹配标记：

```typescript
// 在 DisplaySpan 接口中增加：
interface DisplaySpan extends SpanDetail {
  _depth: number
  _hasChildren: boolean
  _expanded: boolean
  _childCount: number
  _hasCollapsedChildren: boolean
  _searchMatch: boolean     // 新增
  _filterMatch: boolean     // 新增
}
```

在 result.push 时：

```typescript
result.push({
  ...span,
  _depth: depth,
  _hasChildren: hasChildren,
  _expanded: !isCollapsed,
  _childCount: childCount,
  _hasCollapsedChildren: hasChildren && isCollapsed,
  _searchMatch: searchQuery.value ? matchesSearch(span) : true,
  _filterMatch: activeFilters.value.size > 0 ? matchesFilters(span) : true,
})
```

- [ ] **Step 4: 实现 Expand All / Collapse All**

```typescript
function expandAll() {
  collapsedParents.value = new Set()
}

function collapseAll() {
  const newSet = new Set<string>()
  // 收集所有有子节点的 span (depth >= 1)
  for (const span of displaySpans.value) {
    if (span._hasChildren && span._depth >= 1) {
      newSet.add(span.span_id)
    }
  }
  collapsedParents.value = newSet
}
```

- [ ] **Step 5: 在 template 中添加工具栏 HTML**

在 `<div class="waterfall">` 内部，waterfall-header 之前插入：

```html
<div class="waterfall-toolbar">
  <div class="toolbar-left">
    <input
      class="search-input"
      type="text"
      placeholder="Search span name..."
      :value="searchQuery"
      @input="onSearchInput(($event.target as HTMLInputElement).value)"
    />
    <span v-if="searchQuery" class="search-count">{{ matchCount }}/{{ spans.length }}</span>
  </div>
  <div class="toolbar-filters">
    <button
      :class="['filter-btn', { active: activeFilters.size === 0 }]"
      @click="clearFilters"
    >All</button>
    <button
      v-for="opt in FILTER_OPTIONS"
      :key="opt.key"
      :class="['filter-btn', { active: activeFilters.has(opt.key) }]"
      @click="toggleFilter(opt.key)"
    >{{ opt.label }}</button>
  </div>
  <div class="toolbar-actions">
    <button class="action-btn" @click="expandAll" title="Expand All">⇤</button>
    <button class="action-btn" @click="collapseAll" title="Collapse All">⇥</button>
  </div>
</div>
```

- [ ] **Step 6: 运行 TypeScript 检查**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/WaterfallChart.vue
git commit -m "feat: add search bar and quick-filter tags to WaterfallChart toolbar"
```

---

### Task 3: 统计摘要栏 + 搜索高亮 + 自动展开祖先

**Files:**
- Modify: `web/src/components/WaterfallChart.vue`

- [ ] **Step 1: 新增统计 computed**

```typescript
const statsCounts = computed(() => {
  let llm = 0, error = 0, tool = 0
  let maxDurationSpan: SpanDetail | null = null
  for (const span of props.spans) {
    if (span.total_tokens && span.total_tokens > 0) llm++
    if (span.status === 'ERROR') error++
    if (span.kind === 'CLIENT' && span.name.toLowerCase().includes('tool')) tool++
    if (!maxDurationSpan || span.duration_ms > maxDurationSpan.duration_ms) {
      maxDurationSpan = span
    }
  }
  return {
    llm, error, tool, total: props.spans.length,
    maxDurationName: maxDurationSpan?.name || '-',
    maxDurationMs: maxDurationSpan?.duration_ms || 0,
  }
})
```

- [ ] **Step 2: 统计摘要栏 template**

在 toolbar 和 waterfall-header 之间插入：

```html
<div class="waterfall-stats">
  <span>共 {{ statsCounts.total }} spans</span>
  <span class="stats-sep">·</span>
  <span :class="['stats-link', { active: activeFilters.has('llm') }]" @click="toggleFilter('llm')">LLM {{ statsCounts.llm }}</span>
  <span class="stats-sep">·</span>
  <span :class="['stats-link', { active: activeFilters.has('error') }]" @click="toggleFilter('error')">Error {{ statsCounts.error }}</span>
  <span class="stats-sep">·</span>
  <span :class="['stats-link', { active: activeFilters.has('tool') }]" @click="toggleFilter('tool')">Tool {{ statsCounts.tool }}</span>
  <span class="stats-sep">·</span>
  <span>最长 span: {{ statsCounts.maxDurationName }} ({{ formatDuration(statsCounts.maxDurationMs) }})</span>
</div>
```

- [ ] **Step 3: 搜索匹配自动展开祖先链**

新增函数：给定一个 matched span ID，展开它所有的祖先。

```typescript
function expandAncestors(spanId: string) {
  // 构建 parent map
  const parentMap = new Map<string, string>()
  for (const span of props.spans) {
    if (span.parent_span_id) {
      parentMap.set(span.span_id, span.parent_span_id)
    }
  }
  // 从该 span 往上追溯到根
  let current: string | undefined = spanId
  while (current && parentMap.has(current)) {
    const parentId = parentMap.get(current)!
    collapsedParents.value.delete(parentId)
    current = parentId
  }
  // 触发响应式更新
  collapsedParents.value = new Set(collapsedParents.value)
}
```

在搜索输入时，为每个匹配的 span 展开其祖先链。用 `watch` 监听 searchQuery 变化：

```typescript
import { watch } from 'vue'

watch(searchQuery, (newVal) => {
  if (!newVal) return
  // 找到第一个匹配的 span
  const firstMatch = props.spans.find(s => matchesSearch(s))
  if (firstMatch) {
    expandAncestors(firstMatch.span_id)
  }
  // 为所有匹配项展开祖先
  for (const span of props.spans) {
    if (matchesSearch(span)) {
      expandAncestors(span.span_id)
    }
  }
})
```

- [ ] **Step 4: 高亮匹配文字 — 行样式**

在 `waterfall-row` 的 class 绑定中增加搜索匹配和过滤匹配：

```html
<div
  v-for="span in displaySpans"
  :key="span.span_id"
  :class="[
    'waterfall-row',
    {
      selected: selectedSpanId === span.span_id,
      'search-match': span._searchMatch && searchQuery,
      'filter-dimmed': activeFilters.size > 0 && !span._filterMatch,
    }
  ]"
  @click="$emit('select-span', span)"
>
```

新增对应的 CSS 类：

```css
.waterfall-row.search-match {
  background: rgba(251, 191, 36, 0.1);
}
.waterfall-row.filter-dimmed {
  opacity: 0.4;
}
```

- [ ] **Step 5: 高亮匹配文字 — span name 中关键字高亮**

在 col-name 中用 computed 拆分匹配文字和高亮显示。由于直接在 template 中难以实现部分文字高亮，采用简单方案：匹配 span 的 name 字体加粗。

```html
<span :class="['kind-dot', kindDotClass(span.kind)]"></span>
<span :class="{ 'match-text': span._searchMatch && searchQuery }">{{ span.name }}</span>
```

CSS:

```css
.match-text { font-weight: 700; }
```

- [ ] **Step 6: 运行 TypeScript 检查**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/WaterfallChart.vue
git commit -m "feat: add stats summary bar, search highlighting, and auto-expand ancestors"
```

---

### Task 4: 视觉打磨 — CSS 完善

**Files:**
- Modify: `web/src/components/WaterfallChart.vue`（`<style scoped>` 部分）

- [ ] **Step 1: 工具栏样式**

在 `<style scoped>` 中新增：

```css
/* === Toolbar === */
.waterfall-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border-default);
  flex-wrap: wrap;
}
.toolbar-left {
  display: flex;
  align-items: center;
  gap: 6px;
}
.search-input {
  padding: 4px 10px;
  background: var(--bg-primary);
  border: 1px solid var(--border-default);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 12px;
  width: 180px;
}
.search-input::placeholder { color: var(--text-muted); }
.search-input:focus {
  outline: none;
  border-color: var(--accent-blue);
}
.search-count {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}
.toolbar-filters {
  display: flex;
  gap: 4px;
}
.filter-btn {
  padding: 3px 10px;
  border: 1px solid var(--border-default);
  border-radius: 4px;
  background: var(--bg-primary);
  color: var(--text-secondary);
  font-size: 11px;
  cursor: pointer;
}
.filter-btn:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
.filter-btn.active {
  background: var(--accent-blue);
  color: #fff;
  border-color: var(--accent-blue);
}
.toolbar-actions {
  margin-left: auto;
  display: flex;
  gap: 4px;
}
.action-btn {
  padding: 3px 8px;
  border: 1px solid var(--border-default);
  border-radius: 4px;
  background: var(--bg-primary);
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  line-height: 1;
}
.action-btn:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
```

- [ ] **Step 2: 统计栏样式**

```css
/* === Stats bar === */
.waterfall-stats {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  font-size: 11px;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--bg-surface-deep);
}
.stats-sep { color: var(--text-muted); }
.stats-link {
  cursor: pointer;
  color: var(--accent-blue);
}
.stats-link:hover { text-decoration: underline; }
.stats-link.active {
  font-weight: 600;
  background: rgba(59, 130, 246, 0.1);
  padding: 1px 4px;
  border-radius: 2px;
}
```

- [ ] **Step 3: 折叠图标子节点数列样式**

```css
/* === Collapse children count === */
.col-children {
  flex: 0 0 70px;
  text-align: right;
  font-size: 11px;
  color: var(--text-muted);
}
.toggle-placeholder {
  display: inline-block;
  width: 14px;
}
```

更新 waterfall-header 加 col-children：

```css
/* 更新现有 header 的 flex 值 */
.col-name { flex: 0 0 280px; ... }
.col-children { flex: 0 0 70px; text-align: right; }
```

- [ ] **Step 4: 搜索高亮行样式**

```css
/* === Search & filter highlights === */
.waterfall-row.search-match {
  background: rgba(251, 191, 36, 0.08);
}
.waterfall-row.search-match:hover {
  background: rgba(251, 191, 36, 0.15);
}
.waterfall-row.filter-dimmed {
  opacity: 0.35;
}
.waterfall-row.filter-dimmed:hover {
  opacity: 0.6;
}
.match-text { font-weight: 700; }
```

- [ ] **Step 5: 运行 TypeScript 检查**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/WaterfallChart.vue
git commit -m "style: add toolbar, stats bar, and search highlight CSS for WaterfallChart"
```

---

## 验证清单

所有任务完成后，运行：

```bash
cd web && npx vue-tsc --noEmit
```
期望：无错误输出。

手动验证场景：
1. 打开一条 100+ span 的 Trace，确认默认只展开 1-2 层
2. 点击 `▶` 展开子树，点击 `▼` 折叠
3. 搜索一个 span name，确认匹配变黄高亮 + 祖先自动展开
4. 点击 LLM / Error / Tool 过滤标签，确认不匹配的变半透明
5. 点击统计栏的数字，确认激活对应过滤
6. Expand All / Collapse All 正常
7. 选中 span → drawer 正常打开（不受影响）
8. Log badge 点击正常
