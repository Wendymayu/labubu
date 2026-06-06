# WaterfallChart 导航优化 — 设计规范

> 创建日期：2026-06-06
> 状态：已批准
> 关联路线图：Trace 展示优化 (#5 增强)

## 目标

优化 WaterfallChart 在大 Trace（数百个 span）下的浏览体验，通过结构化折叠 + 搜索过滤，让用户在 2-3 秒内定位到感兴趣的 span。

## 范围

- WaterfallChart.vue 单一组件改造
- 不改 props/emits 接口，不影响 TraceDetail 等消费方

### 不做

- 虚拟滚动（几百个 span 性能足够，不需要）
- 后端 API 变更
- 其他页面的 Waterfall 使用（SessionDetail 等暂不涉及）

---

## 1. 折叠逻辑

### 默认行为

- 默认展开深度 1（根 span + 直接子节点）
- 更深层的子树默认折叠，显示 `▶` 图标 + 子节点计数

### 数据结构

```typescript
const collapsedParents = ref<Set<string>>(new Set())  // 被折叠的父 span ID
const DEFAULT_EXPAND_DEPTH = 1

// 初始化：所有 depth > 1 且有子节点的 span 加入 collapsedParents
// walk 到 collapsed 节点时跳过子节点，在行尾显示 "[N children]"
```

### 用户操作

| 操作 | 行为 |
|------|------|
| 点击 `▶`/`▼` 图标 | toggle 该节点的折叠状态 |
| 点击行其他区域 | 选中 span → 打开 drawer（不变） |
| Expand All 按钮 | 清空 `collapsedParents` |
| Collapse All 按钮 | 所有有子节点的 span（depth ≥ 1）加入 `collapsedParents` |

### 自动展开规则

当用户通过搜索跳转到被折叠子树内的 span 时，自动从该 span 逐级往上展开所有祖先，直到根节点。

---

## 2. 搜索框

### 交互

- 输入 span name 关键字，500ms debounce
- 搜索结果计数：`"3/234 matches"`
- 匹配项的祖先链自动展开
- 不匹配的 span 保留（不移除），保持瀑布流结构完整

### 视觉

- 匹配 span 行：`background: rgba(251, 191, 36, 0.1)`（淡黄色）
- 匹配文字加粗

---

## 3. 快速过滤标签

### 标签定义

| 标签 | 匹配规则 |
|------|----------|
| All | 清除过滤 |
| LLM | `total_tokens > 0` |
| Error | `status === 'ERROR'` |
| Tool Call | `kind === 'CLIENT'` 且 name 包含 `tool` |

### 行为

- 支持多选（同时显示 LLM + Error）
- 过滤时：不匹配的 span `opacity: 0.4`（半透明），不移除
- 搜索 + 过滤取交集

---

## 4. 统计摘要栏

Waterfall 顶部新增一行：

```
共 234 spans  ·  LLM 12  ·  Error 3  ·  Tool 45  ·  最长 span: chat_completion (12.3s)
```

- 数字可点击激活对应过滤标签（如点击 "LLM 12" = 激活 LLM 过滤）
- 全部从现有 span 数据计算，无额外请求

---

## 5. 视觉规范

### 折叠行

```
 ▶  · tool_call · read_file    5.2s    [23 children]
 ▼  · agent_loop · claude      12.3s   [5 children]
```

- `▶`/`▼` 在 kind dot 左侧，`@click.stop` 阻止冒泡
- `[N children]` 在行尾 duration 列后，`color: var(--text-muted)`
- 叶子节点不显示 `[N children]`

### 工具栏布局

```
┌──────────────────────────────────────────────────────────────┐
│ [搜索框: "chat"] 3/234  [All] [LLM] [Error] [Tool] [⇤] [⇥] │
│ 共 234 spans · LLM 12 · Error 3 · Tool 45 · max: chat (12s) │
├──────────────────────────────────────────────────────────────┤
│  · chat_completion           ████████████  12.3s  🎯 3.2K    │
│    · ▶ tool_call             ████           5.2s   [23]      │
│  · agent_loop                █████████████  8.1s             │
└──────────────────────────────────────────────────────────────┘
```

- 工具栏按钮：`All` 默认激活，其他按钮 `var(--bg-surface)` 背景
- `Expand All`（⇤）和 `Collapse All`（⇥）在最右侧

---

## 6. 变更清单

| 操作 | 文件 |
|------|------|
| 修改 | `web/src/components/WaterfallChart.vue` |

- Props 不变：`spans`, `traceStartMs`, `traceDurationMs`, `selectedSpanId`, `logCounts`
- Emits 不变：`select-span`, `filter-logs`
- 新增内部状态：`collapsedParents`, `searchQuery`, `activeFilters`

---

## 7. 测试

- TypeScript 类型检查 `npx vue-tsc --noEmit`
- 手动验证：加载一个 100+ span 的 Trace，确认默认折叠、搜索、过滤、展开/折叠切换均正常
