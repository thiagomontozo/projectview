import { test, expect } from '@playwright/test';
import { adminToken, createProjectViaApi, deleteProjectViaApi, uniqueKey, waitForShell } from './helpers';

/**
 * Intake forms, and the suggestion that appears beside a submission.
 *
 * Intake shipped as an API with no screen at all, which meant the whole
 * feature was unreachable by the people it was built for. The triage
 * suggestion made that worse rather than better: a suggestion nobody can see
 * is a suggestion nobody can reject, and "a person confirms" was the entire
 * safety argument for letting a model near this data.
 *
 * So this drives the screen rather than the API: builds a form through the
 * form builder, submits through the public page the way an outsider would, and
 * checks the submission comes back with what was typed.
 */
test('a form can be built, filled in from outside, and read back', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Intake', uniqueKey('INT'));

  try {
    await page.goto(`/projects/${projectId}`);
    await waitForShell(page);

    await page.getByRole('button', { name: /intake|solicita[çc][õo]es/i }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // The first form on a project comes from the empty state rather than the
    // tab strip, which only exists once there is something to strip.
    await dialog.getByRole('button', { name: /new form|novo formul[áa]rio/i }).click();

    const title = `Broken export ${Date.now()}`;
    await dialog.getByLabel(/form title|t[íi]tulo do formul[áa]rio/i).fill(title);
    await dialog.getByLabel(/allow anonymous|permitir envios an[ôo]nimos/i).check();
    await dialog.getByRole('button', { name: /^(create|criar)$/i }).click();

    // The address is the secret - 128 bits, not a name - so it is shown in
    // full for copying. Reading it back is also how this test finds the
    // public page.
    const addressField = dialog.getByLabel(/public address|endere[çc]o p[úu]blico/i);
    await expect(addressField).toBeVisible();
    const publicUrl = await addressField.inputValue();
    expect(publicUrl).toContain('/intake/');

    // Submitted the way somebody outside the company would: no token, no
    // session, nothing but the address.
    const slug = publicUrl.split('/intake/')[1];
    const anonymous = await page.request.post(`/api/public/intake/${slug}`, {
      headers: { Authorization: '' },
      data: {
        // The builder derives a field's key from its label, and the default
        // first question keeps the key "summary".
        answers: { summary: 'The nightly export has failed since Friday' },
        submitterName: 'Outside person'
      }
    });
    expect(anonymous.ok()).toBeTruthy();

    await page.reload();
    await waitForShell(page);
    await page.getByRole('button', { name: /intake|solicita[çc][õo]es/i }).click();
    await expect(page.getByRole('dialog').getByText('Outside person')).toBeVisible();
    await expect(page.getByRole('dialog').getByText(/nightly export/i)).toBeVisible();
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});

/**
 * The suggestion is only shown when there is one.
 *
 * Most installations run with no model configured, and that is a supported
 * deployment rather than a broken one: the submission still has to be
 * readable, with no empty "Suggested" chip implying somebody was asked and had
 * nothing to say.
 */
test('a submission with no suggestion shows no suggestion', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Intake quiet', uniqueKey('INQ'));

  try {
    const form = await request.post(`/api/projects/${projectId}/intake`, {
      headers: { Authorization: `Bearer ${adminToken()}` },
      data: {
        title: 'Quiet form',
        public: true,
        fields: [{ key: 'what', label: 'What happened', type: 'text', required: true }]
      }
    });
    const { slug } = await form.json();
    await request.post(`/api/public/intake/${slug}`, {
      headers: { Authorization: '' },
      data: { answers: { what: 'Nothing a model was asked about' } }
    });

    await page.goto(`/projects/${projectId}`);
    await waitForShell(page);
    await page.getByRole('button', { name: /intake|solicita[çc][õo]es/i }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog.getByText(/Nothing a model was asked about/i)).toBeVisible();
    await expect(dialog.getByText(/^(suggested|sugerido)$/i)).toHaveCount(0);
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});
