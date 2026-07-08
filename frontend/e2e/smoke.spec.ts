import { test, expect, type APIRequestContext, type Page } from '@playwright/test'

// PAI-297 — E2E smoke: the few flows most likely to break across the
// backend/frontend boundary. Auth uses the dev-login build-tag endpoint with a
// token supplied via env (scripts/e2e.sh / CI set it); never present in prod.
const API = process.env.E2E_API_URL ?? 'http://localhost:8888'
const TOKEN = process.env.PAIMOS_DEV_LOGIN_TOKEN ?? ''

type Project = { id: number; key: string; name: string }

async function devLogin(request: APIRequestContext, username = 'dev_admin'): Promise<void> {
  const res = await request.post(`${API}/api/auth/dev-login`, {
    data: { username, token: TOKEN },
  })
  expect(res.ok(), `dev-login failed for ${username}: ${res.status()}`).toBeTruthy()
}

async function listProjects(request: APIRequestContext): Promise<Project[]> {
  const res = await request.get(`${API}/api/projects`)
  expect(res.ok(), `GET /api/projects failed: ${res.status()}`).toBeTruthy()
  const data = (await res.json()) as Project[] | { projects?: Project[] }
  return Array.isArray(data) ? data : (data.projects ?? [])
}

async function listPortalProjects(request: APIRequestContext): Promise<Project[]> {
  const res = await request.get(`${API}/api/portal/projects`)
  expect(res.ok(), `GET /api/portal/projects failed: ${res.status()}`).toBeTruthy()
  const data = (await res.json()) as Project[] | { projects?: Project[] }
  return Array.isArray(data) ? data : (data.projects ?? [])
}

async function expectProjectKeys(
  request: APIRequestContext,
  username: string,
  expected: string[],
  unexpected: string[] = [],
): Promise<Project[]> {
  await devLogin(request, username)
  const projects = await listProjects(request)
  const keys = projects.map((p) => p.key)
  for (const key of expected) expect(keys, `${username} should see ${key}`).toContain(key)
  for (const key of unexpected) expect(keys, `${username} should not see ${key}`).not.toContain(key)
  return projects
}

async function gotoProject(page: Page, projectId: number): Promise<void> {
  const apiCall = page.waitForResponse(
    (r) => r.url().includes(`/api/projects/${projectId}`) && r.ok(),
    { timeout: 15_000 },
  )
  await page.goto(`/projects/${projectId}`)
  await apiCall
}

test.beforeAll(() => {
  expect(TOKEN, 'PAIMOS_DEV_LOGIN_TOKEN must be set for E2E').not.toBe('')
})

test('backend + DB: dev-login, seeded projects, issues list endpoint', async ({ request }) => {
  await devLogin(request)
  const projects = await listProjects(request)
  expect(projects.map((p) => p.key)).toContain('PAI')

  const pai = projects.find((p) => p.key === 'PAI')
  expect(pai, 'seeded PAI project should exist').toBeTruthy()
  if (!pai) return

  const issues = await request.get(`${API}/api/projects/${pai.id}/issues`)
  expect(issues.ok(), `issues list failed: ${issues.status()}`).toBeTruthy()
})

test('frontend serves the authenticated shell (no login bounce)', async ({ page, context }) => {
  await devLogin(context.request)
  await page.goto('/')
  await expect(page).toHaveTitle(/PAIMOS/i)
  // authenticated session → the login form must not be shown
  await expect(page.locator('input[type="password"]')).toHaveCount(0)
})

test('project view renders issues fetched from the backend', async ({ page, context }) => {
  await devLogin(context.request)
  const pai = (await listProjects(context.request)).find((p) => p.key === 'PAI')
  expect(pai, 'seeded PAI project should exist').toBeTruthy()
  if (!pai) return

  // tie the rendered view to a real backend response, not just a static shell
  await gotoProject(page, pai.id)

  await expect(page.locator('input[type="password"]')).toHaveCount(0)
  await expect(page.getByText(/PAI|PAIMOS/i).first()).toBeVisible()
})

// PAI-674 — Role smoke: daily-work screens across the dev-seed access matrix.
// debug-* users are seeded by scripts/e2e.sh with generated throwaway local
// passwords, but tests still authenticate only through dev-login.
test('role matrix: seeded project visibility matches admin/editor/viewer grants', async ({ request }) => {
  await expectProjectKeys(request, 'dev_admin', ['PAI', 'ACME', 'BUGZ', 'LOGS'])
  await expectProjectKeys(request, 'debug-user', ['PAI', 'ACME', 'BUGZ'], ['LOGS'])
})

test('role smoke: admin can work in project issues and reach user settings', async ({ page, context }) => {
  const projects = await expectProjectKeys(context.request, 'dev_admin', ['PAI'])
  const pai = projects.find((p) => p.key === 'PAI')
  expect(pai, 'seeded PAI project should exist').toBeTruthy()
  if (!pai) return

  await gotoProject(page, pai.id)
  await expect(page.getByRole('button', { name: /\+ New issue/i })).toBeVisible()

  await page.goto('/settings?tab=users')
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
})

test('role smoke: member editor gets edit controls only on editor projects', async ({ page, context }) => {
  const projects = await expectProjectKeys(context.request, 'debug-user', ['PAI', 'BUGZ'], ['LOGS'])
  const pai = projects.find((p) => p.key === 'PAI')
  const bugz = projects.find((p) => p.key === 'BUGZ')
  expect(pai, 'debug-user should see PAI').toBeTruthy()
  expect(bugz, 'debug-user should see BUGZ').toBeTruthy()
  if (!pai || !bugz) return

  await gotoProject(page, pai.id)
  await expect(page.getByRole('button', { name: /\+ New issue/i })).toBeVisible()

  await gotoProject(page, bugz.id)
  await expect(page.getByRole('button', { name: /\+ New issue/i })).toHaveCount(0)
})

test('role smoke: member viewer grant sees read-only project work and no admin routes', async ({ page, context }) => {
  const projects = await expectProjectKeys(context.request, 'debug-user', ['BUGZ'])
  const bugz = projects.find((p) => p.key === 'BUGZ')
  expect(bugz, 'debug-user should see BUGZ as viewer').toBeTruthy()
  if (!bugz) return

  await gotoProject(page, bugz.id)
  await expect(page.getByRole('button', { name: /\+ New issue/i })).toHaveCount(0)

  await page.goto('/integrations')
  await expect(page).toHaveURL(/\/$/)
})

test('role smoke: external users are routed to the portal, not internal work screens', async ({ page, context }) => {
  await devLogin(context.request, 'debug-customer')

  const internalProjects = await context.request.get(`${API}/api/projects`)
  expect(internalProjects.status(), 'external users cannot list internal projects').toBe(403)
  expect((await listPortalProjects(context.request)).map((p) => p.key)).toContain('ACME')

  await page.goto('/projects/1')
  await expect(page).toHaveURL(/\/portal$/)
  await expect(page.locator('.portal-dashboard')).toBeVisible()
  await expect(page.locator('input[type="password"]')).toHaveCount(0)
})
