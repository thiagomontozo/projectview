import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * Regression test for a select that would not open inside the task editor.
 *
 * Radix portals a select, menu or tooltip to <body>, which makes it a sibling
 * of the dialog rather than a child. Below the modal in the stacking order it
 * still opens and still receives the click — it just paints behind the dialog
 * surface, so nothing appears to happen and the field looks broken.
 *
 * The tokens are read from the stylesheet rather than from a rendered
 * component on purpose: jsdom computes no paint order, so a DOM assertion
 * would have passed happily while the bug was live. The ordering itself is the
 * invariant worth protecting.
 */
function layerTokens(): Record<string, number> {
  // Resolved from the vitest root rather than from import.meta.url: the test
  // environment is jsdom, where that URL is not a file path.
  const css = readFileSync(join(process.cwd(), 'src/styles/tokens.css'), 'utf8');
  const tokens: Record<string, number> = {};
  for (const [, name, value] of css.matchAll(/--(z-[a-z]+):\s*(\d+);/g)) {
    tokens[name] = Number(value);
  }
  return tokens;
}

describe('layering scale', () => {
  const z = layerTokens();

  it('defines every layer the overlays use', () => {
    for (const layer of ['z-sticky', 'z-overlay', 'z-modal', 'z-dropdown', 'z-toast']) {
      expect(z[layer], `--${layer} is missing`).toBeTypeOf('number');
    }
  });

  it('puts popups above modals', () => {
    // The actual bug: a select portalled out of a dialog paints underneath it.
    expect(z['z-dropdown']).toBeGreaterThan(z['z-modal']);
    expect(z['z-dropdown']).toBeGreaterThan(z['z-overlay']);
  });

  it('keeps the modal above its own overlay', () => {
    expect(z['z-modal']).toBeGreaterThan(z['z-overlay']);
  });

  it('keeps toasts on top of everything', () => {
    // A toast reports something that already happened; it has to be readable
    // whatever else is open.
    expect(z['z-toast']).toBeGreaterThan(z['z-dropdown']);
    expect(z['z-toast']).toBeGreaterThan(z['z-modal']);
  });

  it('keeps sticky chrome below every popup', () => {
    expect(z['z-sticky']).toBeLessThan(z['z-overlay']);
  });
});
