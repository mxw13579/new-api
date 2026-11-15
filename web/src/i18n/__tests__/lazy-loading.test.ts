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
import i18next from 'i18next'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import {
  changeInterfaceLanguage,
  initI18n,
  localeLoaders,
  type LocaleLoader,
} from '../init'
import fr from '../locales/fr.json'

const KEY = 'Language preference saved'
const FR_VALUE = fr.translation[KEY]
const EN_VALUE = 'Language preference saved'

function failingLoader(): LocaleLoader {
  return vi.fn(() => Promise.reject(new Error('Loading chunk locale failed.')))
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  localStorage.clear()
})

test('init with a stored non-English language loads only it and the en fallback', async () => {
  localStorage.setItem('i18nextLng', 'fr')
  const instance = i18next.createInstance()

  const result = await initI18n(instance)

  expect(instance.language).toBe('fr')
  expect(result.activeLanguageError).toBeUndefined()
  expect(instance.hasResourceBundle('fr', 'translation')).toBe(true)
  expect(instance.hasResourceBundle('en', 'translation')).toBe(true)
  for (const language of ['zhCN', 'ru', 'ja', 'vi', 'zhTW']) {
    expect(instance.hasResourceBundle(language, 'translation')).toBe(false)
  }
  expect(instance.t(KEY)).toBe(FR_VALUE)
})

test('init without a stored language maps a zh-CN browser to zhCN', async () => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['zh-CN'])
  vi.spyOn(navigator, 'language', 'get').mockReturnValue('zh-CN')
  const instance = i18next.createInstance()

  await initI18n(instance)

  expect(instance.language).toBe('zhCN')
  expect(instance.hasResourceBundle('zhCN', 'translation')).toBe(true)
})

test('init detects the stored language before later startup code clears localStorage', async () => {
  localStorage.setItem('i18nextLng', 'fr')
  const instance = i18next.createInstance()

  const ready = initI18n(instance)
  localStorage.clear()
  await ready

  expect(instance.language).toBe('fr')
  expect(instance.t(KEY)).toBe(FR_VALUE)
})

test('init reports the error when the active language fails but en loads', async () => {
  localStorage.setItem('i18nextLng', 'fr')
  const instance = i18next.createInstance()

  const result = await initI18n(instance, {
    ...localeLoaders,
    fr: failingLoader(),
  })

  expect(result.activeLanguageError).toBeInstanceOf(Error)
  expect(instance.hasResourceBundle('en', 'translation')).toBe(true)
  expect(instance.t(KEY)).toBe(EN_VALUE)
})

test('init reports no error when only the en fallback fails', async () => {
  localStorage.setItem('i18nextLng', 'fr')
  const instance = i18next.createInstance()

  const result = await initI18n(instance, {
    ...localeLoaders,
    en: failingLoader(),
  })

  expect(result.activeLanguageError).toBeUndefined()
  expect(instance.t(KEY)).toBe(FR_VALUE)
})

test('init still resolves when every locale fails and t() falls back to the key', async () => {
  localStorage.setItem('i18nextLng', 'fr')
  const instance = i18next.createInstance()

  const result = await initI18n(instance, {
    ...localeLoaders,
    fr: failingLoader(),
    en: failingLoader(),
  })

  expect(instance.isInitialized).toBe(true)
  expect(result.activeLanguageError).toBeInstanceOf(Error)
  expect(instance.t(KEY)).toBe(KEY)
})

test('switching to a language whose chunk loads changes and translates with it', async () => {
  localStorage.setItem('i18nextLng', 'en')
  const instance = i18next.createInstance()
  await initI18n(instance)

  const changed = await changeInterfaceLanguage(instance, 'fr')

  expect(changed).toBe(true)
  expect(instance.language).toBe('fr')
  expect(localStorage.getItem('i18nextLng')).toBe('fr')
  expect(instance.t(KEY)).toBe(FR_VALUE)
})

test('switching to a language whose chunk fails keeps the current language and stored value', async () => {
  localStorage.setItem('i18nextLng', 'en')
  const instance = i18next.createInstance()
  await initI18n(instance, { ...localeLoaders, ja: failingLoader() })

  const changed = await changeInterfaceLanguage(instance, 'ja')

  expect(changed).toBe(false)
  expect(instance.language).toBe('en')
  expect(localStorage.getItem('i18nextLng')).toBe('en')
  expect(instance.t(KEY)).toBe(EN_VALUE)
})

test('selecting a previously failed language again still reports failure without switching', async () => {
  localStorage.setItem('i18nextLng', 'en')
  const jaLoader = failingLoader()
  const instance = i18next.createInstance()
  await initI18n(instance, { ...localeLoaders, ja: jaLoader })
  await changeInterfaceLanguage(instance, 'ja')

  const changed = await changeInterfaceLanguage(instance, 'ja')

  expect(changed).toBe(false)
  expect(jaLoader).toHaveBeenCalledTimes(1)
  expect(instance.language).toBe('en')
  expect(localStorage.getItem('i18nextLng')).toBe('en')
})

function deferredLoader(language: keyof typeof localeLoaders) {
  let finish: () => void = () => {}
  const gate = new Promise<void>((resolve) => {
    finish = resolve
  })
  const loader = vi.fn<LocaleLoader>(() =>
    gate.then(() => localeLoaders[language]())
  )
  return { loader, finish }
}

test('selecting the same language twice while it is loading succeeds for both calls with one load', async () => {
  localStorage.setItem('i18nextLng', 'en')
  const ja = deferredLoader('ja')
  const instance = i18next.createInstance()
  await initI18n(instance, { ...localeLoaders, ja: ja.loader })

  const first = changeInterfaceLanguage(instance, 'ja')
  const second = changeInterfaceLanguage(instance, 'ja')
  ja.finish()

  expect(await Promise.all([first, second])).toEqual([true, true])
  expect(ja.loader).toHaveBeenCalledTimes(1)
  expect(instance.language).toBe('ja')
})

test('selecting another language while one is still loading applies the later selection', async () => {
  localStorage.setItem('i18nextLng', 'en')
  const ja = deferredLoader('ja')
  const instance = i18next.createInstance()
  await initI18n(instance, { ...localeLoaders, ja: ja.loader })

  const first = changeInterfaceLanguage(instance, 'ja')
  const second = changeInterfaceLanguage(instance, 'fr')
  ja.finish()

  expect(await Promise.all([first, second])).toEqual([true, true])
  expect(instance.language).toBe('fr')
  expect(localStorage.getItem('i18nextLng')).toBe('fr')
})

test('selecting an already loaded language while another is loading still applies the later selection', async () => {
  localStorage.setItem('i18nextLng', 'en')
  const ja = deferredLoader('ja')
  const instance = i18next.createInstance()
  await initI18n(instance, { ...localeLoaders, ja: ja.loader })
  await changeInterfaceLanguage(instance, 'fr')
  await changeInterfaceLanguage(instance, 'en')

  const first = changeInterfaceLanguage(instance, 'ja')
  const second = changeInterfaceLanguage(instance, 'fr')
  ja.finish()

  expect(await Promise.all([first, second])).toEqual([true, true])
  expect(instance.language).toBe('fr')
})

test('selecting an unsupported language code falls back to en', async () => {
  localStorage.setItem('i18nextLng', 'fr')
  const instance = i18next.createInstance()
  await initI18n(instance)

  const changed = await changeInterfaceLanguage(instance, 'de')

  expect(changed).toBe(true)
  expect(instance.language).toBe('en')
  expect(instance.t(KEY)).toBe(EN_VALUE)
})
