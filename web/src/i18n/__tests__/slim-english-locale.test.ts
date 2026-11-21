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
import i18next, { type i18n as I18nInstance } from 'i18next'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { initI18n } from '../init'
import en from '../locales/en.json'
import { slimEnglishLocale } from '../slim-english-locale'

const slimEn = slimEnglishLocale(en)

async function createEnglishInstance(
  resource: Record<string, Record<string, string>>
): Promise<I18nInstance> {
  const instance = i18next.createInstance()
  await initI18n(instance, {
    en: () => Promise.resolve({ default: resource }),
  })
  return instance
}

/** Interpolation values for every `{{name}}` in the key, plus a plural count. */
function interpolationValues(key: string): Record<string, unknown> {
  const values: Record<string, unknown> = { count: 2 }
  for (const match of key.matchAll(/\{\{\s*([^}\s,]+)/g)) {
    values[match[1]] = match[1] === 'count' ? 2 : `<${match[1]}>`
  }
  return values
}

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('i18nextLng', 'en')
})

afterEach(() => {
  localStorage.clear()
})

describe('slim English locale', () => {
  it('drops entries whose value equals the key and keeps the rest', () => {
    const slim = slimEnglishLocale({
      translation: {
        'Save changes': 'Save changes',
        '1 Hour': '1H',
        Tokens_usage: 'Tokens',
        item_other: 'item_other',
      },
    })

    expect(slim).toEqual({
      translation: {
        '1 Hour': '1H',
        Tokens_usage: 'Tokens',
        item_other: 'item_other',
      },
    })
  })

  it('renders keys with dots, colons, and interpolation the same as the full locale', async () => {
    const full = await createEnglishInstance(en)
    const slim = await createEnglishInstance(slimEn)
    const keys = [
      '. This action cannot be undone.',
      '(Optional: redirect model names)',
      '{{count}} announcements will be removed from the list.',
      '{{count}} enabled channels and {{tasks}} in-flight tasks still use this plugin.',
    ]

    for (const key of keys) {
      expect(key in slimEn.translation).toBe(false)
      expect(slim.t(key, interpolationValues(key))).toBe(
        full.t(key, interpolationValues(key))
      )
    }
    expect(
      slim.t('{{count}} announcements will be removed from the list.', {
        count: 3,
      })
    ).toBe('3 announcements will be removed from the list.')
  })

  it('renders every English key the same as the full locale', async () => {
    const full = await createEnglishInstance(en)
    const slim = await createEnglishInstance(slimEn)

    const mismatches = Object.keys(en.translation).filter(
      (key) =>
        slim.t(key, interpolationValues(key)) !==
        full.t(key, interpolationValues(key))
    )

    expect(mismatches).toEqual([])
  })

  it('still returns the translated value for an entry that differs from its key', async () => {
    const slim = await createEnglishInstance(slimEn)

    expect(slim.t('1 Hour')).toBe('1H')
    expect(slim.t('Tokens', { context: 'usage' })).toBe('Tokens')
  })
})
