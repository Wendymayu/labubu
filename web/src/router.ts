import { createRouter, createWebHistory } from 'vue-router'
import TraceList from './views/TraceList.vue'
import TraceDetail from './views/TraceDetail.vue'
import SessionList from './views/SessionList.vue'
import SessionDetail from './views/SessionDetail.vue'
import Dashboard from './views/Dashboard.vue'
import CostDashboard from './views/CostDashboard.vue'
import LogList from './views/LogList.vue'
import PricingManager from './views/PricingManager.vue'
import LlmConfig from './views/LlmConfig.vue'
import RuleList from './views/alerts/RuleList.vue'
import RuleForm from './views/alerts/RuleForm.vue'
import AlertHistory from './views/alerts/AlertHistory.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/traces' },
    { path: '/traces', name: 'trace-list', component: TraceList },
    { path: '/traces/:id', name: 'trace-detail', component: TraceDetail },
    { path: '/sessions', name: 'session-list', component: SessionList },
    { path: '/sessions/:sessionId', name: 'session-detail', component: SessionDetail },
    { path: '/dashboards', name: 'dashboards', component: Dashboard },
    { path: '/cost', name: 'cost-dashboard', component: CostDashboard },
    { path: '/logs', name: 'log-list', component: LogList },
    { path: '/settings/pricing', name: 'pricing-manager', component: PricingManager },
    { path: '/settings/llm-configs', name: 'llm-configs', component: LlmConfig },
    { path: '/alerts/rules', name: 'rule-list', component: RuleList },
    { path: '/alerts/rules/new', name: 'rule-create', component: RuleForm },
    { path: '/alerts/rules/:id/edit', name: 'rule-edit', component: RuleForm },
    { path: '/alerts/history', name: 'alert-history', component: AlertHistory },
  ]
})
