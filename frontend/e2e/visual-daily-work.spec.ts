import { test, expect, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test'

// PAI-673 — committed visual baseline for daily-work screens. The spec is
// intentionally opt-in so the normal smoke suite stays fast; run it through
// `just visual-baseline`, which boots the dev stack with throwaway fixtures.
test.skip(
  process.env.PAIMOS_VISUAL_BASELINE !== '1',
  'Run visual baselines with `just visual-baseline`.',
)

const API = process.env.E2E_API_URL ?? 'http://localhost:8888'
const TOKEN = process.env.PAIMOS_DEV_LOGIN_TOKEN ?? ''
const FROZEN_NOW = '2026-07-08T10:00:00.000Z'

type Project = { id: number; key: string; name: string }
type Issue = { id: number; issue_key?: string; key?: string; title: string }
type Customer = { id: number; name: string }

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'narrow', width: 390, height: 844 },
] as const

async function devLogin(request: APIRequestContext, username = 'debug-admin'): Promise<void> {
  const res = await request.post(`${API}/api/auth/dev-login`, {
    data: { username, token: TOKEN },
  })
  expect(res.ok(), `dev-login failed for ${username}: ${res.status()}`).toBeTruthy()
}

async function apiGet<T>(request: APIRequestContext, path: string): Promise<T> {
  const res = await request.get(`${API}/api${path}`)
  expect(res.ok(), `GET ${path} failed: ${res.status()}`).toBeTruthy()
  return (await res.json()) as T
}

async function csrfHeaders(context: BrowserContext): Promise<Record<string, string>> {
  const csrf = (await context.cookies()).find((cookie) => cookie.name === 'csrf_token')?.value ?? ''
  expect(csrf, 'dev-login should set csrf_token cookie').not.toBe('')
  return { Origin: API, 'X-CSRF-Token': csrf }
}

async function apiPost<T>(
  context: BrowserContext,
  path: string,
  data: Record<string, unknown>,
): Promise<T> {
  const res = await context.request.post(`${API}/api${path}`, {
    data,
    headers: await csrfHeaders(context),
  })
  expect(res.ok(), `POST ${path} failed: ${res.status()}`).toBeTruthy()
  return (await res.json()) as T
}

function unwrapList<T>(data: T[] | { projects?: T[]; issues?: T[]; customers?: T[] }): T[] {
  if (Array.isArray(data)) return data
  return data.projects ?? data.issues ?? data.customers ?? []
}

async function findProject(request: APIRequestContext, key: string): Promise<Project> {
  const projects = unwrapList<Project>(await apiGet<Project[] | { projects?: Project[] }>(request, '/projects'))
  const project = projects.find((p) => p.key === key)
  expect(project, `seeded ${key} project should exist`).toBeTruthy()
  return project as Project
}

async function firstIssue(request: APIRequestContext, projectId: number): Promise<Issue> {
  const data = await apiGet<Issue[] | { issues?: Issue[] }>(
    request,
    `/projects/${projectId}/issues?fields=list&limit=25`,
  )
  const issue = unwrapList<Issue>(data)[0]
  expect(issue, 'seeded project should have at least one issue').toBeTruthy()
  return issue as Issue
}

async function firstCustomer(context: BrowserContext): Promise<Customer> {
  const data = await apiGet<Customer[] | { customers?: Customer[] }>(context.request, '/customers')
  const customer = unwrapList<Customer>(data)[0]
  if (customer) return customer
  return apiPost<Customer>(context, '/customers', {
    name: 'Visual Baseline GmbH',
    industry: 'Product engineering',
    contact_name: 'Casey Visual',
    contact_email: 'visual-baseline@example.invalid',
    website: 'https://example.invalid',
    phone: '+43 1 555 0100',
    address: 'Hauptplatz 1',
    country: 'Austria',
    billing_address_street: 'Hauptplatz 1',
    billing_address_city: 'Graz',
    billing_address_zip: '8010',
    billing_address_country: 'Austria',
    notes: 'Fixture customer for the daily-work visual regression baseline.',
  })
}

async function stabilizePage(page: Page): Promise<void> {
  await page.addInitScript(`
    (() => {
      const fixedNow = new Date('${FROZEN_NOW}').valueOf();
      const RealDate = Date;
      const FrozenDate = new Proxy(RealDate, {
        construct(target, args) {
          return args.length === 0 ? new target(fixedNow) : new target(...args);
        },
        apply(target, thisArg, args) {
          return args.length === 0
            ? new target(fixedNow).toString()
            : Reflect.apply(target, thisArg, args);
        },
      });
      FrozenDate.now = () => fixedNow;
      FrozenDate.parse = RealDate.parse;
      FrozenDate.UTC = RealDate.UTC;
      FrozenDate.prototype = RealDate.prototype;
      globalThis.Date = FrozenDate;
      const css = \`
        html { scroll-behavior: auto !important; }
        *, *::before, *::after {
          animation-delay: 0s !important;
          animation-duration: 0s !important;
          caret-color: transparent !important;
          transition-delay: 0s !important;
          transition-duration: 0s !important;
        }
      \`;
      const installStyle = () => {
        if (document.getElementById('paimos-visual-stability')) return;
        const style = document.createElement('style');
        style.id = 'paimos-visual-stability';
        style.textContent = css;
        document.head.appendChild(style);
      };
      if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', installStyle, { once: true });
      } else {
        installStyle();
      }
    })();
  `)
}

async function gotoAndSettle(page: Page, path: string): Promise<void> {
  await page.goto(path, { waitUntil: 'domcontentloaded' })
  await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(350)
}

async function capture(page: Page, name: string): Promise<void> {
  await expect(page).toHaveScreenshot(`${name}.png`, {
    animations: 'disabled',
    caret: 'hide',
    fullPage: false,
    maxDiffPixelRatio: 0.01,
    scale: 'css',
  })
}

test.beforeAll(() => {
  expect(TOKEN, 'PAIMOS_DEV_LOGIN_TOKEN must be set for visual baselines').not.toBe('')
})

for (const viewport of VIEWPORTS) {
  test.describe(`daily-work baseline: ${viewport.name}`, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height })
      await stabilizePage(page)
    })

    test(`project issue list (${viewport.name})`, async ({ page, context }) => {
      await devLogin(context.request)
      const pai = await findProject(context.request, 'PAI')

      await gotoAndSettle(page, `/projects/${pai.id}`)
      await expect(page.getByRole('button', { name: /\+ New issue/i })).toBeVisible()
      await expect(page.locator('.issue-table-wrap, .issue-tree-wrap, .empty-state').first()).toBeVisible()
      await capture(page, `project-issue-list-${viewport.name}`)
    })

    test(`issue detail AI workbench (${viewport.name})`, async ({ page, context }) => {
      await devLogin(context.request)
      const pai = await findProject(context.request, 'PAI')
      const issue = await firstIssue(context.request, pai.id)
      const issueRef = issue.issue_key ?? issue.key ?? String(issue.id)

      await gotoAndSettle(page, `/projects/${pai.id}/issues/${issueRef}`)
      const workbench = page.locator('#ai-workbench')
      await expect(workbench).toBeVisible()
      await workbench.scrollIntoViewIfNeeded()
      await page.waitForTimeout(250)
      await capture(page, `issue-ai-workbench-${viewport.name}`)
    })

    test(`settings users (${viewport.name})`, async ({ page, context }) => {
      await devLogin(context.request)

      await gotoAndSettle(page, '/settings?tab=users')
      await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
      await expect(page.locator('.tab-bar')).toBeVisible()
      await capture(page, `settings-users-${viewport.name}`)
    })

    test(`customer detail (${viewport.name})`, async ({ page, context }) => {
      await devLogin(context.request)
      const customer = await firstCustomer(context)

      await gotoAndSettle(page, `/customers/${customer.id}`)
      await expect(page.locator('.cd-name')).toContainText(customer.name)
      await capture(page, `customer-detail-${viewport.name}`)
    })

    test(`portal dashboard (${viewport.name})`, async ({ page, context }) => {
      await devLogin(context.request, 'debug-customer')

      await gotoAndSettle(page, '/portal')
      await expect(page.locator('.portal-dashboard')).toBeVisible()
      await capture(page, `portal-dashboard-${viewport.name}`)
    })
  })
}
