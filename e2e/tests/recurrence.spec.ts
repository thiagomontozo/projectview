import { test, expect } from '@playwright/test';
import { createProjectViaApi, createTaskViaApi, deleteProjectViaApi, uniqueKey, waitForShell } from './helpers';

/**
 * Making a task repeat, from the dialog.
 *
 * The mode choice is what this really guards. "When completed" and "on
 * schedule" behave very differently when nobody does the work - one stops
 * quietly, the other keeps producing overdue instances - so the control has to
 * present both with their consequence, and it has to actually be operable. A
 * radio nobody can reach is the same defect as the dropdown painted behind its
 * dialog that this suite was created for.
 */
test('a task can be made to repeat, and the choice explains itself', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Recurrence', uniqueKey('RCR'));
  const title = `Repeating ${Date.now()}`;
  await createTaskViaApi(request, projectId, title);

  try {
    await page.goto(`/projects/${projectId}`);
    await waitForShell(page);
    await page.getByRole('button', { name: new RegExp(title, 'i') }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Not repeating is the starting state, and it says so rather than showing
    // an empty control.
    await expect(dialog.getByText(/does not repeat|não se repete/i)).toBeVisible();

    await dialog.getByRole('button', { name: /make it repeat|fazer repetir/i }).click();

    // Both modes are offered with the sentence that distinguishes them.
    await expect(dialog.getByText(/nothing piles up|nada se acumula/i)).toBeVisible();
    await expect(dialog.getByText(/stays open and goes overdue|fica aberta e atrasa/i)).toBeVisible();

    const scheduled = dialog.getByRole('radio', { name: /due date|data de prazo/i });
    await expect(scheduled).toBeVisible();

    // Monthly, and it sticks across a reload - the rule is stored, not local.
    await dialog.getByRole('button', { name: /^months$|^meses$/i }).click();
    await expect(dialog.getByRole('button', { name: /^months$|^meses$/i })).toHaveAttribute('aria-pressed', 'true');

    await page.reload();
    await waitForShell(page);
    await page.getByRole('button', { name: new RegExp(title, 'i') }).click();
    await expect(
      page.getByRole('dialog').getByRole('button', { name: /^months$|^meses$/i })
    ).toHaveAttribute('aria-pressed', 'true');
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});
