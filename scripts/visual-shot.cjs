// Render a route of the local PAIMOS dev UI to a PNG via headless Chromium.
//
// Prereq: the dev stack is up (`just dev-up` in another terminal) and the
// dev-login token exists (~/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env).
//
// Invoked through scripts/visual-shot.sh (which bootstraps Playwright into a
// gitignored tooling dir and sets NODE_PATH), so `require('playwright')`
// resolves without adding any dependency to the app or to CI.
//
// Usage: node visual-shot.cjs [route] [out.png]
//   no route → defaults to the first seeded project's issue list.

const { chromium } = require('playwright')
const fs = require('node:fs')
const os = require('node:os')

const TOKEN_FILE =
  process.env.PAIMOS_DEV_LOGIN_TOKEN_FILE || `${os.homedir()}/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env`
const TOKEN =
  process.env.PAIMOS_DEV_LOGIN_TOKEN ||
  (fs.existsSync(TOKEN_FILE) && fs.readFileSync(TOKEN_FILE, 'utf8').match(/PAIMOS_DEV_LOGIN_TOKEN=(\S+)/)?.[1])
const BASE = process.env.PAIMOS_DEV_URL || 'http://localhost:5173'
const USER = process.env.PAIMOS_DEV_USER || 'dev_admin'
const ROUTE = process.argv[2] || null
const OUT = process.argv[3] || '/tmp/paimos-shot.png'

;(async () => {
  if (!TOKEN) {
    console.error(`no dev-login token (set PAIMOS_DEV_LOGIN_TOKEN or create ${TOKEN_FILE})`)
    process.exit(2)
  }
  const browser = await chromium.launch()
  const context = await browser.newContext({
    baseURL: BASE,
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2,
  })
  // dev-login via the vite-proxied same-origin endpoint → cookie lands in the context
  const login = await context.request.post('/api/auth/dev-login', {
    headers: { 'Content-Type': 'application/json' },
    data: { username: USER, token: TOKEN },
  })
  if (!login.ok()) {
    console.error(`dev-login failed: ${login.status()} — is the stack up (just dev-up)?`)
    await browser.close()
    process.exit(1)
  }
  // pick a seeded project for a populated list when no route is given
  let route = ROUTE
  if (!route) {
    const res = await context.request.get('/api/projects')
    const data = await res.json().catch(() => null)
    const list = Array.isArray(data) ? data : (data && data.projects) || []
    const id = list[0] && (list[0].id ?? list[0].project_id)
    route = id ? `/projects/${id}` : '/issues'
  }
  const page = await context.newPage()
  await page.goto(route, { waitUntil: 'networkidle' }).catch(() => {})
  await page.waitForTimeout(2500) // let async lists/data settle
  await page.screenshot({ path: OUT })
  console.log(`✓ ${route} → ${OUT}  (login ${login.status()}, title "${await page.title()}")`)
  await browser.close()
})().catch((e) => {
  console.error('ERR', e)
  process.exit(1)
})
