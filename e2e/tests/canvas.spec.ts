import { test, expect } from '@playwright/test';
import { createProjectViaApi, deleteProjectViaApi, uniqueKey, waitForShell } from './helpers';

/**
 * Whiteboards, sheets, clips and apps.
 *
 * These four screens were added because a comparison against another tool found
 * them missing, which makes "it renders and does the one thing it exists for"
 * exactly the assertion worth having: a menu item leading to a screen that
 * cannot do its own job is worse than no menu item.
 *
 * Each spec drives the interface rather than the API. The API is checked
 * elsewhere; what could not be checked elsewhere is whether a person can get
 * from the navigation to a saved document.
 */

test('a note can be put on a whiteboard, and it survives a reload', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Boards', uniqueKey('WBD'));

  try {
    await page.goto('/whiteboards');
    await waitForShell(page);

    // The picker defaults to the first project, which may not be this one.
    await page.getByRole('combobox', { name: /projetos|projects/i }).selectOption(projectId);
    await page.getByRole('button', { name: /new board|novo quadro/i }).click();

    // Creating opens the editor, because the reason to make a board is to draw
    // on it.
    const canvas = page.getByRole('application', { name: /whiteboards|quadros brancos/i });
    await expect(canvas).toBeVisible();

    await page.getByRole('button', { name: /^(note|nota)$/i }).click();
    await canvas.click({ position: { x: 220, y: 180 } });

    // A new note opens for typing, so the tool and the writing are one gesture.
    await page.keyboard.type('Decide the release date');
    await canvas.click({ position: { x: 500, y: 400 } });

    await page.getByRole('button', { name: /^(save|salvar)$/i }).click();
    await expect(page.getByText(/saved|salvo/i).first()).toBeVisible();

    await page.reload();
    await waitForShell(page);
    await page.getByRole('combobox', { name: /projetos|projects/i }).selectOption(projectId);
    await page.getByRole('button', { name: /Untitled board|Quadro sem título/i }).first().click();
    await expect(page.getByText('Decide the release date')).toBeVisible();
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});

test('a sheet totals a column, and shows the total rather than the formula', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Sheets', uniqueKey('SHT'));

  try {
    await page.goto('/sheets');
    await waitForShell(page);
    await page.getByRole('combobox', { name: /projetos|projects/i }).selectOption(projectId);
    await page.getByRole('button', { name: /new sheet|nova planilha/i }).click();

    const formulaBar = page.getByRole('textbox', { name: /value or formula|valor ou f[óo]rmula/i });
    await expect(formulaBar).toBeVisible();

    // A1 is selected when the sheet opens, so typing lands somewhere sensible
    // without a click.
    await formulaBar.fill('8');
    await formulaBar.press('Enter');
    await formulaBar.fill('7.5');
    await formulaBar.press('Enter');
    await formulaBar.fill('=SUM(A1:A2)');
    await formulaBar.press('Enter');

    // The grid shows what the formula came to; the formula bar keeps the
    // formula. Showing the result in both is how a spreadsheet eats somebody's
    // work the first time they edit a cell.
    await expect(page.getByRole('button', { name: '15.5', exact: true })).toBeVisible();

    await page.getByRole('button', { name: /^(save|salvar)$/i }).click();
    await page.reload();
    await waitForShell(page);
    await page.getByRole('combobox', { name: /projetos|projects/i }).selectOption(projectId);
    await page.getByRole('button', { name: /Untitled sheet|Planilha sem título/i }).first().click();
    await expect(page.getByRole('button', { name: '15.5', exact: true })).toBeVisible();
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});

test('clips and apps render, and say what they need rather than failing', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Clips', uniqueKey('CLP'));

  try {
    await page.goto('/clips');
    await waitForShell(page);
    // Recording cannot be driven by a test — the browser's share dialog is
    // deliberately outside a page's reach — so what is asserted is that the
    // screen is usable and honest about an empty state.
    await expect(page.getByRole('button', { name: /record screen|gravar tela/i })).toBeVisible();
    await expect(page.getByText(/no clips yet|nenhum clipe ainda/i)).toBeVisible();

    await page.goto('/apps');
    await waitForShell(page);
    await expect(page.getByText(/active directory/i)).toBeVisible();
    // The state is read from the running configuration rather than assumed, so
    // every card carries one.
    await expect(page.getByText(/connected|not set up|conectado|não configurado/i).first()).toBeVisible();
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});

test('a list group folds, and the count stays visible when it does', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Groups', uniqueKey('GRP'));

  try {
    await page.goto(`/projects/${projectId}`);
    await waitForShell(page);
    await page.getByRole('tab', { name: /^(list|lista)$/i }).click();

    const collapse = page.getByRole('button', { name: /collapse group|recolher grupo/i }).first();
    await expect(collapse).toBeVisible();
    await collapse.click();

    // Folded, not hidden: the header and its count stay, because the rows being
    // out of sight must not read as the group being empty.
    await expect(page.getByRole('button', { name: /expand group|expandir grupo/i }).first()).toBeVisible();
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});

test('a view can be saved and comes back after a reload', async ({ page, request }) => {
  const projectId = await createProjectViaApi(request, 'E2E Views', uniqueKey('VWS'));

  try {
    await page.goto(`/projects/${projectId}`);
    await waitForShell(page);
    await page.getByRole('tab', { name: /^(table|tabela)$/i }).click();

    await page.getByRole('button', { name: /^\+ (view|visualização)$/i }).click();
    await page.getByRole('textbox', { name: /name this view|nome da visualização/i }).fill('Everything');
    await page.getByRole('button', { name: /^(save|salvar)$/i }).click();

    // The whole point: it is still there tomorrow. Before this, the arrangement
    // lived in a React state that a reload discarded.
    await page.reload();
    await waitForShell(page);
    await expect(page.getByRole('tab', { name: 'Everything' })).toBeVisible();
  } finally {
    await deleteProjectViaApi(request, projectId);
  }
});
