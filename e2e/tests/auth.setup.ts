import { test as setup, expect } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { expectNoRawTranslationKeys } from './helpers';

const STATE_FILE = '.auth/admin.json';
const TOKEN_FILE = '.auth/token.json';

const USERNAME = process.env.E2E_ADMIN_USER ?? 'admin';
const PASSWORD = process.env.E2E_ADMIN_PASS ?? 'ChangeMe123!';

/**
 * Signs in through the real form, then saves the session for every other spec.
 *
 * This doubles as the regression guard for the worst of the three defects that
 * prompted the suite: the login screen rendered *nothing at all*. An anonymous
 * 401 was treated as an expired session, so the query cache cleared, refetched,
 * cleared again, and `loading` never settled. Every API assertion passed
 * throughout - the endpoint was answering correctly - while the page a user
 * opened was blank.
 *
 * So the first thing asserted here is not that sign-in works. It is that there
 * is something on the screen to sign in with.
 */
setup('the login screen renders, and signing in works', async ({ page }) => {
  await page.goto('/login');

  // The blank-page guard. Deliberately checks visible controls rather than
  // "the app mounted": a React tree that renders an empty div mounts fine.
  await expect(page.getByRole('heading', { name: 'ProjectView' })).toBeVisible();
  await expect(page.getByLabel(/usu[áa]rio|username/i)).toBeVisible();
  await expect(page.getByLabel(/senha|password/i)).toBeVisible();
  await expect(page.getByRole('button', { name: /entrar|sign in/i })).toBeVisible();

  // And the second defect, at its cheapest: if the language chain resolves to
  // nothing, i18next renders each key as itself and the page fills with
  // "auth.signIn" and "app.tagline". One assertion that no visible text looks
  // like a dotted key catches the whole class.
  await expectNoRawTranslationKeys(page);

  // When Active Directory is configured the form offers a choice and defaults
  // to the directory. The bootstrap administrator is a local account, so the
  // suite says so explicitly rather than depending on how this particular
  // installation is set up - otherwise enabling AD silently breaks every test.
  const localTab = page.getByRole('tab', { name: /conta local|local account/i });
  if (await localTab.count()) {
    await localTab.click();
  }

  await page.getByLabel(/usu[áa]rio|username/i).fill(USERNAME);
  await page.getByLabel(/senha|password/i).fill(PASSWORD);
  await page.getByRole('button', { name: /entrar|sign in/i }).click();

  // Landing on the dashboard is what proves the session was actually
  // established, rather than the form merely having been submitted.
  await page.waitForURL((url) => !url.pathname.startsWith('/login'), { timeout: 30_000 });
  await expect(page.getByRole('navigation').first()).toBeVisible();

  mkdirSync(dirname(STATE_FILE), { recursive: true });
  await page.context().storageState({ path: STATE_FILE });

  // The bearer token is lifted out of local storage and kept beside the
  // session, so specs can clean up the projects they create through the API
  // instead of driving a confirmation dialog for teardown - and so a failed
  // test still leaves a removable fixture rather than a permanent one.
  const token = await page.evaluate(() => window.localStorage.getItem('pv_token'));
  expect(token, 'the session did not store an access token').toBeTruthy();
  writeFileSync(TOKEN_FILE, JSON.stringify({ token }), 'utf8');
});
