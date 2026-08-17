import { test, expect } from '@playwright/test';
import { adminToken, waitForShell } from './helpers';

/**
 * Staffing a team.
 *
 * This is the gap a user actually reported: teams could be created and never
 * staffed, because the add/remove endpoints existed and nothing in the
 * interface called them. An API test would have found nothing wrong — both
 * endpoints answered correctly the whole time.
 */
test('a team can be created and then staffed', async ({ page, request }) => {
  const name = `E2E Team ${Date.now()}`;
  let teamId: string | undefined;

  try {
    await page.goto('/teams');
    await waitForShell(page);

    await page.getByRole('button', { name: /nova equipe|new team/i }).first().click();
    const create = page.getByRole('dialog');
    await create.getByLabel(/nome|name/i).fill(name);
    await create.getByRole('button', { name: /criar|create/i }).click();
    await expect(create).toBeHidden();
    await expect(page.getByText(name)).toBeVisible();

    // The control that did not exist before.
    const card = page.locator('li').filter({ hasText: name });
    await card.getByRole('button', { name: /membros|members/i }).click();

    const members = page.getByRole('dialog');
    await expect(members).toBeVisible();
    await expect(members.getByText(/ninguém nesta equipe|nobody is on this team/i)).toBeVisible();

    // Search the people already in the system and allocate one.
    await members.getByRole('textbox', { name: /buscar pessoas|search people/i }).fill('admin');
    const candidate = members.getByRole('button').filter({ hasText: /admin/i }).first();
    await expect(candidate).toBeVisible();
    await candidate.click();

    // The allocation is real, not local state: it survives closing and
    // reopening, which is what proves the endpoint was called.
    await expect(members.getByText(/ninguém nesta equipe|nobody is on this team/i)).toBeHidden();
    // Escape rather than a "Close" button: the dialog has two of those - its
    // own dismiss affordance and the footer action - and pressing Escape is
    // what a person does anyway.
    await page.keyboard.press('Escape');
    await page.reload();
    await waitForShell(page);
    await page.locator('li').filter({ hasText: name }).getByRole('button', { name: /membros|members/i }).click();
    await expect(page.getByRole('dialog').getByText(/admin@/i).first()).toBeVisible();
  } finally {
    const listing = await request.get('/api/teams', {
      headers: { Authorization: `Bearer ${adminToken()}` }
    });
    if (listing.ok()) {
      teamId = (await listing.json()).find((candidate: { name: string; id: string }) => candidate.name === name)?.id;
    }
    if (teamId) {
      await request
        .delete(`/api/teams/${teamId}`, { headers: { Authorization: `Bearer ${adminToken()}` } })
        .catch(() => undefined);
    }
  }
});
