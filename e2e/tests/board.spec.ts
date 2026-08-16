import { test, expect } from '@playwright/test';
import {
  adminToken,
  createProjectViaApi,
  createTaskViaApi,
  deleteProjectViaApi,
  label,
  uniqueKey,
  waitForShell
} from './helpers';

test.describe('the board', () => {
  let projectId: string | undefined;
  const projectName = 'E2E Board Project';

  test.beforeAll(async ({ request }) => {
    projectId = await createProjectViaApi(request, projectName, uniqueKey('BRD'));
  });

  test.afterAll(async ({ request }) => {
    await deleteProjectViaApi(request, projectId);
  });

  /**
   * The third defect, and the one an API test can least reach.
   *
   * The status and priority dropdowns did nothing: Radix portals the listbox to
   * the end of the body, and it was painted *behind* the dialog that opened it.
   * Every layer of this was correct in isolation - the endpoint accepted the
   * change, the component dispatched it, the test suite passed - and a person
   * clicking the control saw an option they could not reach.
   *
   * So this test does not check that the value can be set programmatically. It
   * opens the dropdown, clicks the option where it is actually painted, and
   * then proves the change survived a reload.
   */
  test('a task status can be changed from the dialog and it sticks', async ({ page, request }) => {
    const taskTitle = `Status change ${Date.now()}`;
    await createTaskViaApi(request, projectId!, taskTitle);

    await page.goto(`/projects/${projectId}`);
    await waitForShell(page);

    await page.getByRole('button', { name: new RegExp(taskTitle, 'i') }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    const statusTrigger = dialog.getByRole('combobox').first();
    await statusTrigger.click();

    // The listbox is portalled outside the dialog, so it is addressed on the
    // page rather than within it - and it has to be *visible*, which is the
    // assertion that would have failed when it rendered behind the overlay.
    const option = page.getByRole('option', { name: /in progress/i });
    await expect(option).toBeVisible();

    // Playwright refuses a click on an element another element covers, so this
    // is not merely a click - it is the proof that nothing is on top of it.
    await option.click();
    await expect(statusTrigger).toContainText(/in progress/i);

    await dialog.getByRole('button', { name: label.save }).click();
    await expect(dialog).toBeHidden();

    // Reloaded rather than trusted: the dialog closing proves the form
    // submitted, not that anything was stored.
    await page.reload();
    await waitForShell(page);
    const inProgress = page.getByRole('region', { name: /in progress/i })
      .or(page.locator('section', { hasText: /in progress/i }).first());
    await expect(inProgress.getByRole('button', { name: new RegExp(taskTitle, 'i') })).toBeVisible();
  });

  /**
   * Dragging a card between columns.
   *
   * dnd-kit's PointerSensor only starts a drag after four pixels of movement,
   * and it tracks pointer events rather than the HTML5 drag protocol - so
   * Playwright's dragTo(), which dispatches drag events, does nothing here. The
   * movement is therefore driven by hand, in steps, because a single jump from
   * source to target can be delivered as one event and never cross the
   * activation threshold.
   */
  test('a card can be dragged to another column', async ({ page, request }) => {
    const taskTitle = `Draggable ${Date.now()}`;
    await createTaskViaApi(request, projectId!, taskTitle);

    await page.goto(`/projects/${projectId}`);
    await waitForShell(page);

    const card = page.getByRole('button', { name: new RegExp(taskTitle, 'i') });
    await expect(card).toBeVisible();

    const target = page.locator('section').filter({ hasText: /in progress/i }).first();
    await expect(target).toBeVisible();

    const from = await card.boundingBox();
    const to = await target.boundingBox();
    expect(from && to, 'the card or the target column has no layout box').toBeTruthy();

    await page.mouse.move(from!.x + from!.width / 2, from!.y + from!.height / 2);
    await page.mouse.down();
    // Past the 4px activation threshold first, as a separate movement.
    await page.mouse.move(from!.x + from!.width / 2 + 20, from!.y + from!.height / 2 + 20, { steps: 5 });
    await page.mouse.move(to!.x + to!.width / 2, to!.y + 120, { steps: 15 });
    await page.mouse.up();

    // The move is optimistic in the client, so a reload is what distinguishes
    // "the card animated" from "the server accepted it".
    await page.reload();
    await waitForShell(page);

    const moved = page.locator('section').filter({ hasText: /in progress/i }).first();
    await expect(moved.getByRole('button', { name: new RegExp(taskTitle, 'i') })).toBeVisible();
  });

  test('a project can be created from the interface', async ({ page, request }) => {
    const name = `E2E Created ${Date.now()}`;
    const key = uniqueKey('NEW');
    let createdId: string | undefined;

    await page.goto('/projects');
    await waitForShell(page);

    await page.getByRole('button', { name: label.newProject }).first().click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await dialog.getByLabel(label.name).fill(name);
    await dialog.getByLabel(label.key).fill(key);
    await dialog.getByRole('button', { name: label.create }).click();

    await expect(dialog).toBeHidden();
    await expect(page.getByText(name)).toBeVisible();

    // Cleaned up here rather than in afterAll: this project is the test's own
    // output, and leaving it behind would make a second run of the suite create
    // a second one every time.
    const listing = await request.get('/api/projects', {
      headers: { Authorization: `Bearer ${adminToken()}` }
    });
    if (listing.ok()) {
      createdId = (await listing.json()).find((p: { key: string; id: string }) => p.key === key)?.id;
    }
    await deleteProjectViaApi(request, createdId);
  });
});
