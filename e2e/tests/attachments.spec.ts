import { test, expect } from '@playwright/test';
import { createProjectViaApi, createTaskViaApi, deleteProjectViaApi, uniqueKey, waitForShell } from './helpers';

/**
 * Attaching a file by dropping it.
 *
 * Attachments shipped with a drag-and-drop target and an upload progress bar
 * that nothing exercised: a drop zone whose handler never fired would have
 * passed every API assertion, because the API was never involved. This is the
 * gap that was named in the backlog and left open.
 */
test('a file can be dropped onto a task, and it sticks', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Attachments', uniqueKey('ATT'));
  const title = `Droppable ${Date.now()}`;
  await createTaskViaApi(request, projectId, title);

  try {
    await page.goto(`/projects/${projectId}`);
    await waitForShell(page);
    await page.getByRole('button', { name: new RegExp(title, 'i') }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    const config = await page.request.get('/api/attachments/config');
    const enabled = (await config.json()).enabled;
    test.skip(!enabled, 'this installation has no object storage configured');

    // A real drop, not a click on the hidden input. The dataTransfer is built
    // in the page so the File is a genuine one from the browser's own
    // constructor - a synthetic event carrying a plain object would exercise
    // the handler without proving a dropped file reaches it.
    const dropZone = dialog.getByText(/drag them here|arraste-os at[ée] aqui/i);
    await expect(dropZone).toBeVisible();

    const dataTransfer = await page.evaluateHandle(() => {
      const data = new DataTransfer();
      data.items.add(new File(['dropped-by-the-browser'], 'dropped.txt', { type: 'text/plain' }));
      return data;
    });
    await dropZone.dispatchEvent('drop', { dataTransfer });

    // Named in the list is the assertion: the upload reached the server and
    // came back, rather than the drop merely being accepted by the DOM.
    await expect(dialog.getByText('dropped.txt')).toBeVisible({ timeout: 30_000 });

    // And it survives a reload, which is what separates "rendered optimistically"
    // from "stored".
    await page.reload();
    await waitForShell(page);
    await page.getByRole('button', { name: new RegExp(title, 'i') }).click();
    await expect(page.getByRole('dialog').getByText('dropped.txt')).toBeVisible({ timeout: 30_000 });
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});
