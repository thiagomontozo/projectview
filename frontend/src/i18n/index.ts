import i18n from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';
import en from './en.json';
import ptBR from './pt-BR.json';

export const SUPPORTED_LANGUAGES = [
  { code: 'pt-BR', label: 'Português' },
  { code: 'en', label: 'English' }
] as const;

export type LanguageCode = (typeof SUPPORTED_LANGUAGES)[number]['code'];

/**
 * Translations are bundled rather than fetched: the app is behind a login on a
 * corporate network, so two small dictionaries in the bundle beat a round trip
 * that would leave the first paint untranslated.
 *
 * The detector reads a stored choice first and falls back to the browser's own
 * language, so a Brazilian user gets Portuguese without configuring anything.
 */
void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      'pt-BR': { translation: ptBR },
      en: { translation: en }
    },
    fallbackLng: 'pt-BR',
    supportedLngs: SUPPORTED_LANGUAGES.map((l) => l.code),
    // No nonExplicitSupportedLngs here, deliberately. It strips the region
    // before checking supportedLngs, so "pt-BR" is tested as "pt" against a
    // list that only holds "pt-BR" — no match, an empty language chain, and
    // every label renders as its own key. English survived it because "en" is
    // already region-less, which is why the bug only showed in Portuguese.
    // "pt" and "pt-PT" reach Portuguese anyway: they are not in the list, so
    // they fall through to fallbackLng, which is pt-BR.
    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: 'pv_language',
      caches: ['localStorage']
    },
    interpolation: {
      // React escapes for us; escaping twice mangles apostrophes.
      escapeValue: false
    }
  });

/** Keeps <html lang> in step, which screen readers use to pick a voice. */
i18n.on('languageChanged', (language) => {
  document.documentElement.lang = language;
});

export default i18n;
