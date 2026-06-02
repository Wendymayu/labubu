import { createRouter, createWebHistory } from 'vue-router'
import TraceList from './views/TraceList.vue'
import TraceDetail from './views/TraceDetail.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/traces' },
    { path: '/traces', name: 'trace-list', component: TraceList },
    { path: '/traces/:id', name: 'trace-detail', component: TraceDetail }
  ]
})
