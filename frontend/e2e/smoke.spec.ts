import { test, expect, type APIRequestContext } from '@playwright/test'

// PAI-297 — E2E smoke: the few flows most likely to break across the
// backend/frontend boundary. Auth uses the dev-login build-tag endpoint with a
// token supplied via env (scripts/e2e.sh / CI set it); never present in prod.
const API = process.env.E2E_API_URL ?? 'http://localhost:8888'
const TOKEN = process.env.PAIMOS_DEV_LOGIN_TOKEN ?? ''

type Project = { id: number; key: string; name: string }

async function devLogin(request: APIRequestContext): Promise<void> {
  const res = await request.post(`${API}/api/auth/dev-login`, {
    data: { username: 'dev_admin', token: TOKEN },
  })
  expect(res.ok(), `dev-login failed: ${res.status()}`).toBeTruthy()
}

async function listProjects(request: APIRequestContext): Promise<Project[]> {
  const res = await request.get(`${API}/api/projects`)
  expect(res.ok(), `GET /api/projects failed: ${res.status()}`).toBeTruthy()
  const data = (await res.json()) as Project[] | { projects?: Project[] }
  return Array.isArray(data) ? data : (data.projects ?? [])
}

test.beforeAll(() => {
  expect(TOKEN, 'PAIMOS_DEV_LOGIN_TOKEN must be set for E2E').not.toBe('')
})

test('backend + DB: dev-login, seeded projects, issues list endpoint', async ({ request }) => {
  await devLogin(request)
  const projects = await listProjects(request)
  expect(projects.map((p) => p.key)).toContain('PAIT')

  const pait = projects.find((p) => p.key === 'PAIT')
  expect(pait, 'seeded PAIT project should exist').toBeTruthy()
  if (!pait) return

  const issues = await request.get(`${API}/api/projects/${pait.id}/issues`)
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
  const pait = (await listProjects(context.request)).find((p) => p.key === 'PAIT')
  expect(pait, 'seeded PAIT project should exist').toBeTruthy()
  if (!pait) return

  // tie the rendered view to a real backend response, not just a static shell
  const apiCall = page.waitForResponse(
    (r) => r.url().includes(`/api/projects/${pait.id}`) && r.ok(),
    { timeout: 15_000 },
  )
  await page.goto(`/projects/${pait.id}`)
  await apiCall

  await expect(page.locator('input[type="password"]')).toHaveCount(0)
  await expect(page.getByText(/PAIT|Paimos Testing/i).first()).toBeVisible()
})
