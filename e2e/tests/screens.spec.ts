import { test, expect } from '@playwright/test';
import { expectNoErrorBoundary, expectNoRawTranslationKeys, waitForShell } from './helpers';

/**
 * Every screen, opened, with the cheapest assertion that would have caught all
 * three of the defects this suite exists for: does a person see something?
 *
 * The backlog put it plainly - "one test that loads the app and asserts a
 * visible word would have caught all three above". This is that test, once per
 * route. It deliberately does not exercise behaviour; the specs beside it do
 * that for the screens where behaviour broke. What it covers is the failure
 * mode the API tests are structurally blind to: a route that answers 200,
 * mounts cleanly, and paints nothing.
 *
 * Three things are asserted per screen, because they fail differently:
 *
 *   - a heading is visible          (the page rendered)
 *   - no error boundary             (a component did not throw and get caught)
 *   - no raw translation keys       (the labels resolved)
 */
const SCREENS: Array<{ path: string; name: string }> = [
  { path: '/', name: 'dashboard' },
  { path: '/my-tasks', name: 'my tasks' },
  { path: '/spaces', name: 'spaces' },
  { path: '/projects', name: 'projects' },
  { path: '/teams', name: 'teams' },
  { path: '/resources', name: 'resource allocation' },
  { path: '/reports', name: 'reports' },
  { path: '/chat', name: 'chat' },
  { path: '/docs', name: 'docs' },
  { path: '/goals', name: 'goals' },
  { path: '/portfolio', name: 'portfolio' },
  { path: '/settings', name: 'settings' },
  { path: '/admin/users', name: 'user administration' },
  { path: '/admin/settings', name: 'system settings' }
];

for (const screen of SCREENS) {
  test(`${screen.name} renders`, async ({ page }) => {
    const failures: string[] = [];
    // A screen that renders while the console fills with errors is a screen
    // about to break, so the noise is collected and reported with the failure
    // rather than swallowed. It does not fail the test on its own: third-party
    // warnings are not this suite's business.
    page.on('pageerror', (error) => failures.push(error.message));

    await page.goto(screen.path);
    await waitForShell(page);

    await expect(
      page.getByRole('heading').first(),
      `${screen.name} rendered no heading`
    ).toBeVisible();

    await expectNoErrorBoundary(page);
    await expectNoRawTranslationKeys(page);

    expect(failures, `uncaught errors on ${screen.name}: ${failures.join(' | ')}`).toEqual([]);
  });
}

/**
 * The navigation is what makes those routes reachable by a person rather than
 * only by a URL. A link that renders but points nowhere leaves every screen
 * above passing and the application unusable.
 */
test('the primary navigation moves between screens', async ({ page }) => {
  await page.goto('/');
  await waitForShell(page);

  const nav = page.getByRole('navigation').first();
  await nav.getByRole('link', { name: /^(projetos|projects)$/i }).click();
  await expect(page).toHaveURL(/\/projects$/);
  await expect(page.getByRole('heading', { name: /projetos|projects/i }).first()).toBeVisible();

  await nav.getByRole('link', { name: /minhas tarefas|my tasks/i }).click();
  await expect(page).toHaveURL(/\/my-tasks$/);
  await expect(page.getByRole('heading').first()).toBeVisible();
});
