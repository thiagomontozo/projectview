import { test, expect } from '@playwright/test';
import { expectNoRawTranslationKeys, waitForShell } from './helpers';

test.describe('settings and language', () => {
  /**
   * The second defect: every label showed as its own translation key.
   *
   * `nonExplicitSupportedLngs` against a region-qualified `supportedLngs`
   * resolved to an empty language chain, so i18next fell through to rendering
   * "settings.profile" where the heading should have been. Nothing failed -
   * the app worked perfectly, in a language made of dotted identifiers.
   *
   * Switching language is where it would show first, so that is where it is
   * checked: the switch has to change the words on the screen, in both
   * directions, and neither direction may leave a key behind.
   */
  test('the language can be switched, and both languages render real words', async ({ page }) => {
    await page.goto('/settings');
    await waitForShell(page);

    // The screen is there at all - the guard the settings page never had.
    await expect(page.getByRole('heading', { name: /configura[çc][õo]es|settings/i }).first()).toBeVisible();

    const portuguese = page.getByRole('button', { name: 'Português' });
    const english = page.getByRole('button', { name: 'English' });
    await expect(portuguese).toBeVisible();
    await expect(english).toBeVisible();

    await portuguese.click();
    // A word from the dictionary rather than the absence of a key: proving the
    // page is not showing "nav.projects" says nothing about whether it is
    // showing Portuguese.
    await expect(page.getByRole('navigation').first()).toContainText(/projetos/i);
    await expectNoRawTranslationKeys(page);

    await english.click();
    await expect(page.getByRole('navigation').first()).toContainText(/projects/i);
    await expectNoRawTranslationKeys(page);

    // The choice is persisted, and a reload is the only way to prove it: the
    // in-memory switch working tells you nothing about the next visit.
    await portuguese.click();
    await page.reload();
    await waitForShell(page);
    await expect(page.getByRole('navigation').first()).toContainText(/projetos/i);

    // Left in English so the rest of the suite, and the next run, start from a
    // known language.
    await page.getByRole('button', { name: 'English' }).click();
    await expect(page.getByRole('navigation').first()).toContainText(/projects/i);
  });

  test('the administration settings screen opens', async ({ page }) => {
    await page.goto('/admin/settings');
    await waitForShell(page);

    // Only administrators may even read this, so its rendering is also a check
    // that the role gate lets the right person through rather than blanking the
    // page for everybody.
    await expect(page.getByRole('heading').first()).toBeVisible();
    await expect(page.getByText(/ldap|smtp|oidc|directory|diret[óo]rio/i).first()).toBeVisible();
    await expectNoRawTranslationKeys(page);
  });
});
