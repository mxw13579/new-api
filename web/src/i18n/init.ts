/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type {
  BackendModule,
  i18n as I18nInstance,
  ResourceLanguage,
} from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import {
  convertDetectedLanguage,
  type InterfaceLanguageCode,
} from './languages'

export type LocaleLoader = () => Promise<{ default: ResourceLanguage }>

// Each locale is its own async chunk so the entry bundle carries no
// translations; i18next fetches only the active language plus the `en` fallback.
export const localeLoaders: Record<InterfaceLanguageCode, LocaleLoader> = {
  en: () => import(/* webpackChunkName: "locale-en" */ './locales/en.json'),
  zhCN: () => import(/* webpackChunkName: "locale-zh" */ './locales/zh.json'),
  fr: () => import(/* webpackChunkName: "locale-fr" */ './locales/fr.json'),
  ru: () => import(/* webpackChunkName: "locale-ru" */ './locales/ru.json'),
  ja: () => import(/* webpackChunkName: "locale-ja" */ './locales/ja.json'),
  vi: () => import(/* webpackChunkName: "locale-vi" */ './locales/vi.json'),
  zhTW: () =>
    import(/* webpackChunkName: "locale-zh-TW" */ './locales/zh-TW.json'),
}

export type I18nInitResult = {
  /** Why the active language failed to load; undefined when it loaded. */
  activeLanguageError: unknown
}

/**
 * Initialize an i18next instance with lazily loaded locale resources. The
 * returned promise always resolves, so callers can render even when a locale
 * chunk fails to load (missing translations then fall back to their keys).
 */
export function initI18n(
  instance: I18nInstance,
  loaders: Partial<Record<string, LocaleLoader>> = localeLoaders
): Promise<I18nInitResult> {
  const loadErrors = new Map<string, unknown>()
  const backend: BackendModule = {
    type: 'backend',
    init() {},
    // The two-argument promise form is part of the backend contract; a
    // rejection is reported without i18next's read retries, so a failed
    // chunk does not delay the first render.
    read(language, namespace) {
      const load = loaders[language]
      if (!load) return Promise.resolve({})
      return load().then(
        (module) => {
          loadErrors.delete(language)
          return module.default[namespace]
        },
        (error: unknown) => {
          loadErrors.set(language, error)
          throw error
        }
      )
    },
  }

  return instance
    .use(backend)
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
      // Detect the language synchronously at import, as bundled resources did,
      // so it runs before startup code such as `initializeFrontendCache()`
      // clears localStorage; only the resource fetch itself is asynchronous.
      initAsync: false,
      fallbackLng: 'en',
      supportedLngs: ['en', 'zhCN', 'fr', 'ru', 'ja', 'vi', 'zhTW'],
      load: 'currentOnly',
      nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
      // Keep browser debugging in dev, but not the full resource dumps in tests.
      debug: import.meta.env.DEV && !import.meta.env.VITEST,
      interpolation: {
        escapeValue: false, // not needed for react as it escapes by default
      },
      detection: {
        order: ['localStorage', 'navigator'],
        caches: ['localStorage'],
        // Browsers report `zh-CN`/`zh-TW`/`zh`; map them onto our `zhCN`/`zhTW`
        // codes (non-Chinese codes pass through for normal supportedLngs matching).
        convertDetectedLanguage,
      },
    })
    .then(
      () => undefined,
      () => undefined
    )
    .then(() => ({
      activeLanguageError: instance.hasResourceBundle(
        instance.language,
        'translation'
      )
        ? undefined
        : loadErrors.get(instance.language),
    }))
}

// The most recent `changeInterfaceLanguage` target per instance, so a slower
// earlier selection never overrides the one the user made last.
const latestTargets = new WeakMap<I18nInstance, string>()

/**
 * Switch the interface language only after its translations are available.
 * When they cannot be loaded, the current language (and its cached
 * `i18nextLng` value) stays unchanged and false is returned, so callers must
 * not persist the new language. When a later call starts before this one
 * finishes loading, this call returns true without switching and the later
 * selection decides the language.
 */
export async function changeInterfaceLanguage(
  instance: I18nInstance,
  language: string
): Promise<boolean> {
  // Resolve the code the same way `changeLanguage` does (unsupported codes
  // fall back to `en`), so the check below targets the language it would use.
  const target: string =
    instance.services.languageUtils.getBestMatchFromCodes([language]) ??
    language
  latestTargets.set(instance, target)
  if (!instance.hasResourceBundle(target, 'translation')) {
    // Load only this language through the backend connector: a load already
    // in flight for it is awaited instead of skipped (`loadLanguages` skips
    // it), and other in-flight languages are not waited on. An array is not
    // expanded to the fallback hierarchy, and a language whose load failed is
    // not retried this session, so it keeps failing here (callers report it)
    // until the page reloads.
    await instance.reloadResources([target])
  }
  if (!instance.hasResourceBundle(target, 'translation')) return false
  if (latestTargets.get(instance) !== target) return true

  await instance.changeLanguage(language)
  return true
}
