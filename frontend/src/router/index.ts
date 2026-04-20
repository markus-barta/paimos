/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

// Route meta shape. `projectIdParam` names the URL param that holds the
// project ID — the beforeEach guard uses it to enforce per-project view
// access before the component mounts.
declare module 'vue-router' {
  interface RouteMeta {
    public?: boolean
    adminOnly?: boolean
    portal?: boolean
    projectIdParam?: string
  }
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login',    component: () => import('@/views/LoginView.vue'),         meta: { public: true } },
    { path: '/forgot',   component: () => import('@/views/ForgotPasswordView.vue'), meta: { public: true } },
    { path: '/reset/:token', component: () => import('@/views/ResetPasswordView.vue'), meta: { public: true } },
    { path: '/',         component: () => import('@/views/DashboardView.vue') },
    { path: '/projects', component: () => import('@/views/ProjectsView.vue') },
    { path: '/projects/accruals/print', component: () => import('@/views/AccrualsPrintView.vue'), meta: { adminOnly: true } },
    { path: '/projects/:id', component: () => import('@/views/ProjectDetailView.vue'), meta: { projectIdParam: 'id' } },
    { path: '/projects/:id/issues/:issueId', component: () => import('@/views/IssueDetailView.vue'), meta: { projectIdParam: 'id' } },
    { path: '/issues',   component: () => import('@/views/IssuesView.vue') },
    { path: '/sprints',       redirect: '/sprint-board' },
    { path: '/sprint-board', component: () => import('@/views/SprintBoardView.vue') },
    { path: '/users',    redirect: '/settings?tab=users' },
    { path: '/integrations', component: () => import('@/views/IntegrationsView.vue'), meta: { adminOnly: true } },
    { path: '/import',   redirect: '/integrations' },
    { path: '/search',   redirect: '/issues' },
    { path: '/settings',    component: () => import('@/views/SettingsView.vue') },
    { path: '/development', redirect: '/settings?tab=development' },
    // Portal routes (external users)
    { path: '/portal',                        component: () => import('@/views/portal/PortalDashboard.vue'), meta: { portal: true } },
    { path: '/portal/projects/:id',           component: () => import('@/views/portal/PortalProjectView.vue'), meta: { portal: true } },
    { path: '/portal/projects/:id/issues/:issueId', component: () => import('@/views/portal/PortalIssueView.vue'), meta: { portal: true } },
    { path: '/reporting', component: () => import('@/views/ReportingView.vue') },
    { path: '/reporting/lieferbericht', component: () => import('@/views/LieferberichtView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.checked) await auth.fetchMe()
  if (!to.meta.public && !auth.user) return '/login'
  if (to.path === '/login' && auth.user) {
    return auth.user.role === 'external' ? '/portal' : '/'
  }
  // Admin-only routes
  if (to.meta.adminOnly && auth.user?.role !== 'admin') return '/'
  // External users: redirect away from internal routes to portal
  if (auth.user?.role === 'external' && !to.meta.portal && !to.meta.public && to.path !== '/login') {
    return '/portal'
  }
  // Internal users accessing portal (admins can, members redirect home)
  if (auth.user && auth.user.role === 'member' && to.meta.portal) {
    return '/'
  }
  // Per-project view access. Routes opt in by setting meta.projectIdParam
  // to the URL parameter that carries the project ID. If the param is
  // missing or not numeric, fall through — the underlying handler will
  // produce the 404 instead.
  const pidParam = to.meta.projectIdParam
  if (pidParam && auth.user) {
    const raw = to.params[pidParam]
    const pid = Array.isArray(raw) ? Number(raw[0]) : Number(raw)
    if (!Number.isNaN(pid) && pid > 0 && !auth.canView(pid)) {
      return '/'
    }
  }
})

// Auto-reload on stale chunks after deploy — dynamic import fails when
// the server-side chunk files have changed but the browser still has
// the old router config referencing old hashed filenames.
router.onError((error, to) => {
  if (
    error.message.includes('Failed to fetch dynamically imported module') ||
    error.message.includes('Importing a module script failed') ||
    error.message.includes('error loading dynamically imported module')
  ) {
    window.location.href = to.fullPath
  }
})

export default router
