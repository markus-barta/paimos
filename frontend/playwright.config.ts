import { defineConfig, devices } from '@playwright/test'

// PAI-297 — E2E smoke suite. The stack (Go backend :8888 + vite :5173) is
// booted by scripts/e2e.sh (locally and in CI) BEFORE this runs — we
// deliberately do NOT use Playwright's `webServer` so boot/teardown lives in
// one debuggable place and the same script works on a dev box and on a runner.
export default defineConfig({
  testDir: './e2e',
  snapshotPathTemplate: '{testDir}/__screenshots__/{testFilePath}/{arg}{ext}',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:5173',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
