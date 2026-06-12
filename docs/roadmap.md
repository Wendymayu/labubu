# Labubu 项目特性计划

> 最后更新：2026-06-06

## 特性总览

| # | 特性 | 状态 | 完成时间 | 备注 |
|---|------|------|----------|------|
| 1 | Trace 接收与展示 | ✅ 已完成 | 2026-06-03 | OTLP receiver + TraceList + TraceDetail |
| 2 | Metric 接收 | ✅ 已完成 | 2026-06-03 | OTLP metrics gRPC/HTTP + tstorage |
| 3 | 指标图表自动生成 | ✅ 已完成 | 2026-06-03 | Dashboard + PanelChart + PanelForm |
| 4 | Trace 中的上下文窗口分析 | ✅ 已完成 | 2026-06-03 | TokenPieChart 组件 |
| 5 | Trace 展示优化 | ✅ 已完成 | 2026-06-04 | Waterfall 重设计 + slide-in drawer |
| 6 | Session 可观测 | ✅ 已完成 | 2026-06-05 | SessionList + SessionDetail + API |
| 7 | 前端菜单和表格支持国际化 | ✅ 已完成 | 2026-06-05 | vue-i18n, 中英文, 菜单+表格 |
| 8 | 项目支持打成一个整体包，推送到中心仓库供下载使用 | ✅ 已完成 | 2026-06-05 | pip wheel, Go embed, CI/CD |
| 9 | Claude Code 可观测数据接入 Labubu | ✅ 已完成 | 2026-06-06 | Metrics + Traces + Logs 全部信号已支持；见 docs/integrations/claude-code-metrics.md |
| 10 | 会话详情新增上下文窗口使用详情图 | 📋 计划中 | — | |
| 11 | 页面风格支持黑白两种背景 | ✅ 已完成 | 2026-06-06 | CSS 变量主题系统 + ThemeToggle 组件 + useTheme composable |
| 12 | 观测 Claude Code 会话任务状态详情 | 📋 计划中 | — | |
| 13 | 观测 JiuwenClaw 页面会话任务状态详情 | 📋 计划中 | — | |
| 14 | Trace 支持持久化，只保留一天或 1 万条，支持 YAML 配置 | ✅ 已完成 | 2026-06-06 | YAML 配置加载 + Purge + 定时清理 goroutine，默认 24h/10000 条 |
| 15 | Metric 默认保存一天数据，支持配置 | ✅ 已完成 | 2026-06-06 | MetricRetentionConfig + tstorage WithRetention，默认 24h |
| 16 | **OTLP Logs 接收与展示** | ✅ 已完成 | 2026-06-06 | OTLP `/v1/logs` gRPC+HTTP 端点 + log 存储(memstore/chDB) + LogList 页面 + TraceDetail 日志面板 |
| 17 | **AI 成本追踪** | ✅ 已完成 | 2026-06-12 | 模型单价配置 + 按 Trace/Session/模型维度自动计算费用 + Cost Dashboard |
| 18 | **Agent 任务成功率分析** | 📋 计划中 | — | 工具调用成功率、重试次数、loop 深度聚合，Session 级别 Agent 运行稳定性指标（Tier 1） |
| 19 | **Trace 对比（Diff View）** | 📋 计划中 | — | 选中两条 Trace 并排对比：Span 树、token 用量、tool call 路径差异（Tier 2） |
| 20 | **实时 Trace 流（Live Tail）** | 📋 计划中 | — | WebSocket 推送新 Trace 实时展示，自动滚动，支持过滤，观察运行中的 Agent（Tier 2） |
| 21 | **告警规则** | ✅ 已完成 | 2026-06-12 | 阈值告警 + 规则 CRUD + 历史查看 + SMTP 通知 |
| 22 | **LLM-as-judge 自动评估** | 📋 计划中 | — | 利用已配置 LLM 对 Trace 自动评分（延迟/成本/错误/准确性/效率 5 维度），基于已有 DiagnosisResult（Tier 1） |
| 23 | **简单标注/反馈** | 📋 计划中 | — | 单人 thumbs up/down + 星级评分 + 文本注释，JSON 文件存储，Span 和 Trace 级标注（Tier 1） |
| 24 | **Prompt Playground** | 📋 计划中 | — | 交互式 Playground：prompt 输入 + 模型选择 + 参数调节 + 对比输出 + 自动 trace 记录（Tier 2） |
| 25 | **Guardrails 检测** | 📋 计划中 | — | 毒性标记/PII 泄露/事实性检查，作为诊断评估扩展维度（Tier 3） |
| 26 | **Prompt 版本管理** | 📋 计划中 | — | Playground 延伸：prompt 保存为版本、历史对比、trace 关联（Tier 3） |

## 状态说明

| 状态 | 含义 |
|------|------|
| ✅ 已完成 | 已实现并合入主干 |
| 🔧 进行中 | 正在开发 |
| 📋 计划中 | 已规划，未开始 |
| ❌ 已取消 | 不再计划 |
