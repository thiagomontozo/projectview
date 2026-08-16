import { expect, type APIRequestContext, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';

/**
 * Shared plumbing.
 *
 * The suite is written against *roles and labels* rather than CSS classes, and
 * that is not stylistic. Two of the three defects these tests exist for were
 * accessibility failures wearing a different hat - a portalled dropdown painted
 * behind its dialog, and labels that never resolved - so a test that finds its
 * targets the way a screen reader does is testing the thing that broke.
 */

export function adminToken(): string {
  const { token } = JSON.parse(readFileSync('.auth/token.json', 'utf8'));
  return token;
}

/**
 * Both languages, so a spec does not have to know which one the browser picked.
 *
 * Deliberately unanchored. A required field renders its label with a trailing
 * asterisk inside the same element, so `^Name$` matches nothing while `Name`
 * matches what a person reads - and the accessible name is exactly what these
 * tests should be finding things by.
 */
export const label = {
  newProject: /novo projeto|new project/i,
  name: /nome|name/i,
  key: /chave|key/i,
  create: /criar|create/i,
  save: /salvar|save/i,
  cancel: /cancelar|cancel/i,
  settings: /configura[çc][õo]es|settings/i,
  projects: /projetos|projects/i,
  taskTitle: /t[íi]tulo|title/i,
  newTask: /nova tarefa|new task/i
};

/**
 * A key unique per run.
 *
 * Project keys are unique in the schema, so a fixed one makes the second run of
 * the day fail on a conflict that has nothing to do with the test. The clock is
 * trimmed to stay inside whatever length the field accepts.
 */
export function uniqueKey(prefix = 'E2E'): string {
  return `${prefix}${Date.now().toString().slice(-6)}`;
}

/**
 * Creates a project through the API for tests that need one to already exist.
 *
 * Used for setup, never for the thing under test: the "create a project" spec
 * drives the real form. Building fixtures through the interface would make
 * every spec depend on the correctness of a screen it is not examining.
 */
export async function createProjectViaApi(
  request: APIRequestContext,
  name: string,
  key: string
): Promise<string> {
  const response = await request.post('/api/projects', {
    headers: { Authorization: `Bearer ${adminToken()}` },
    data: { name, key, description: 'Created by the browser test suite.' }
  });
  expect(response.ok(), `could not create the fixture project: ${await response.text()}`).toBeTruthy();
  return (await response.json()).id;
}

export async function createTaskViaApi(
  request: APIRequestContext,
  projectId: string,
  title: string
): Promise<string> {
  const response = await request.post(`/api/projects/${projectId}/tasks`, {
    headers: { Authorization: `Bearer ${adminToken()}` },
    data: { title, priority: 'high' }
  });
  expect(response.ok(), `could not create the fixture task: ${await response.text()}`).toBeTruthy();
  return (await response.json()).id;
}

/**
 * Removes a fixture project and everything under it.
 *
 * Deliberately tolerant of failure: teardown that throws turns one red test
 * into two and hides which one actually broke.
 */
export async function deleteProjectViaApi(request: APIRequestContext, id: string | undefined) {
  if (!id) return;
  await request
    .delete(`/api/projects/${id}`, { headers: { Authorization: `Bearer ${adminToken()}` } })
    .catch(() => undefined);
}

/**
 * Waits for the shell to be interactive rather than merely navigated.
 *
 * `waitForURL` returns as soon as the address changes, which on a SPA is before
 * anything has rendered - the exact window in which a blank page looks like a
 * pass.
 */
export async function waitForShell(page: Page) {
  await expect(page.getByRole('navigation').first()).toBeVisible();
}

/**
 * Fails if any visible text looks like an untranslated i18next key.
 *
 * The defect this guards was `nonExplicitSupportedLngs` against a
 * region-qualified `supportedLngs` resolving to an empty language chain, which
 * turned every label into its own key. Matching the *shape* of a key rather
 * than any particular one is what keeps it useful as strings are added: a
 * dotted token with no spaces is either a translation key or a filename, and
 * neither belongs in a rendered label.
 */
export async function expectNoRawTranslationKeys(page: Page) {
  const leaked = await page.evaluate(() => {
    const pattern = /^[a-z][a-zA-Z0-9]*(\.[a-zA-Z0-9_]+)+$/;
    const found: string[] = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    let node: Node | null;
    while ((node = walker.nextNode())) {
      const text = node.textContent?.trim() ?? '';
      if (text && !text.includes(' ') && pattern.test(text)) found.push(text);
    }
    return found;
  });
  expect(leaked, `untranslated keys rendered as text: ${leaked.join(', ')}`).toEqual([]);
}

/**
 * Fails if the error boundary caught something.
 *
 * A component that throws is replaced by the boundary's message, which is a
 * perfectly valid render - the page is "up", the API answered, and the screen
 * is useless. Checking for it explicitly is the difference between a test that
 * proves a page works and one that proves it responded.
 */
export async function expectNoErrorBoundary(page: Page) {
  await expect(
    page.getByText(/algo deu errado|something went wrong/i),
    'the error boundary rendered instead of the page'
  ).toHaveCount(0);
}
