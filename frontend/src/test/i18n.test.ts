import { describe, expect, it } from 'vitest';
import i18n from 'i18next';
import en from '../i18n/en.json';
import ptBR from '../i18n/pt-BR.json';
import { SUPPORTED_LANGUAGES } from '../i18n';

/**
 * Regression tests for the screen rendering raw keys ("nav.dashboard") instead
 * of words.
 *
 * A dictionary that fails to resolve does not throw and does not fail the
 * build — i18next simply returns the key, so every label silently becomes its
 * own identifier. These assertions make that visible in CI instead of in a
 * screenshot.
 */
describe('translation dictionaries', () => {
  it('offers a code for every bundled dictionary', () => {
    // The switcher writes these codes into i18n.changeLanguage, so a code the
    // resources are not keyed by resolves to nothing at all.
    const codes = SUPPORTED_LANGUAGES.map((language) => language.code);
    expect(codes).toContain('pt-BR');
    expect(codes).toContain('en');
  });

  it('resolves keys in both languages', async () => {
    const instance = i18n.createInstance();
    await instance.init({
      resources: { 'pt-BR': { translation: ptBR }, en: { translation: en } },
      lng: 'pt-BR',
      fallbackLng: 'pt-BR',
      supportedLngs: SUPPORTED_LANGUAGES.map((l) => l.code),
      interpolation: { escapeValue: false }
    });

    // An empty chain is the signature of the bug: i18next resolved the
    // language to nothing, so every lookup falls through to its own key.
    expect(instance.languages.length).toBeGreaterThan(0);
    expect(instance.t('nav.dashboard')).not.toBe('nav.dashboard');
    expect(instance.t('app.name')).toBe('ProjectView');
    expect(instance.t('dashboard.resetLayout')).toBe('Restaurar layout');

    await instance.changeLanguage('en');
    expect(instance.t('dashboard.resetLayout')).toBe('Reset layout');

    // Switching back must restore Portuguese rather than leave the keys bare.
    await instance.changeLanguage('pt-BR');
    expect(instance.t('dashboard.resetLayout')).toBe('Restaurar layout');
  });

  it('resolves a bare "pt" onto the pt-BR dictionary', async () => {
    // A browser reporting "pt" or "pt-PT", or a stale value left in
    // localStorage by an earlier build, must not land on raw keys.
    const instance = i18n.createInstance();
    await instance.init({
      resources: { 'pt-BR': { translation: ptBR }, en: { translation: en } },
      lng: 'pt',
      fallbackLng: 'pt-BR',
      supportedLngs: SUPPORTED_LANGUAGES.map((l) => l.code),
      interpolation: { escapeValue: false }
    });

    expect(instance.t('nav.dashboard')).not.toBe('nav.dashboard');
  });
});
