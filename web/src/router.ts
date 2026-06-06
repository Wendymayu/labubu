import { createRouter, createWebHistory } from 'vue-router'
import TraceList from './views/TraceList.vue'
import TraceDetail from './views/TraceDetail.vue'
import SessionList from './views/SessionList.vue'
import SessionDetail from './views/SessionDetail.vue'
import Dashboard from './views/Dashboard.vue'
import LogList from './views/LogList.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/traces' },
    { path: '/traces', name: 'trace-list', component: TraceList },
    { path: '/traces/:id', name: 'trace-detail', component: TraceDetail },
    { path: '/sessions', name: 'session-list', component: SessionList },
    { path: '/sessions/:sessionId', name: 'session-detail', component: SessionDetail },
    { path: '/dashboards', name: 'dashboards', component: Dashboard },
    { path: '/logs', name: 'log-list', component: LogList },
  ]
})
