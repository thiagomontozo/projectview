import { defineConfig, devices } from '@playwright/test';

/**
 * Browser-level tests against a running stack.
 *
 * These exist because of a specific history. Three defects reached users while
 * five CI jobs stayed green: the login screen rendered nothing, every label
 * showed as its own translation key, and the status and priority selects did
 * nothing at all. Each was invisible to an API test and obvious in a rendered
 * page. The suite below is shaped around that: it is not an attempt to
 * re-verify the API, which 316 assertions already cover, but to prove the
 * screens are there and respond.
 *
 * The target is the edge proxy, not the Vite dev server, so what is tested is
 * the built bundle behind the real routing and TLS rules - which is where two
 * of those three defects lived.
 */

const BASE_URL = process.env.E2E_BASE_URL ?? 'https://localhost';

export default defineConfig({
  testDir: './tests',
  // Generous: the first navigation may land while the stack is still warming,
  // and a timeout that fires early reads as a broken app rather than a slow one.
  timeout: 60_000,
  expect: { timeout: 15_000 },

  // Serial. The tests share one administrator account and create real projects
  // through the interface; running them in parallel would have them competing
  // over the same board and the same settings screen.
  fullyParallel: false,
  workers: 1,

  // Never on a developer's machine: a test that passes on the second attempt
  // is a test that has told you something, and retrying locally hides it. CI
  // retries once, because a genuinely flaky browser test should not block a
  // merge on its own.
  retries: process.env.CI ? 1 : 0,
  forbidOnly: !!process.env.CI,

  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : [['list']],

  use: {
    baseURL: BASE_URL,
    // The default deployment serves a self-signed certificate.
    ignoreHTTPSErrors: true,
    // Kept only for failures: an artefact for every passing test is noise
    // nobody reads and a CI cache nobody wants.
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off'
  },

  projects: [
    // Signs in once and saves the session. The login endpoint is deliberately
    // rate-limited to ten attempts a minute, so a suite that authenticated per
    // test would start measuring the rate limiter and fail for a reason that
    // has nothing to do with the screens.
    { name: 'setup', testMatch: /auth\.setup\.ts/ },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], storageState: '.auth/admin.json' },
      dependencies: ['setup']
    }
  ]
});
