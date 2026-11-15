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
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { Toaster, toast } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { LanguageSwitcher } from '../language-switcher'

const LOAD_FAILED =
  'Failed to load language. Please refresh the page and try again.'

beforeEach(async () => {
  await act(() => i18next.changeLanguage('en'))
  useAuthStore.getState().auth.setUser({ id: 1, username: 'alice', role: 1 })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

afterEach(async () => {
  toast.dismiss()
  useAuthStore.getState().auth.reset()
  await act(() => i18next.changeLanguage('en'))
  i18next.removeResourceBundle('fr', 'translation')
})

async function chooseLanguage(label: string) {
  const user = userEvent.setup()
  render(
    <>
      <LanguageSwitcher />
      <Toaster />
    </>
  )
  await user.click(screen.getByRole('button', { name: 'Change language' }))
  await user.click(await screen.findByRole('menuitem', { name: label }))
}

test('choosing a language whose translations fail to load keeps the language, warns, and saves nothing', async () => {
  // The shared test instance has no backend, so `ja` resources never load.
  await chooseLanguage('日本語')

  expect(await screen.findByText(LOAD_FAILED)).toBeInTheDocument()
  expect(i18next.language).toBe('en')
  expect(api.put).not.toHaveBeenCalled()
})

test('choosing a language whose translations are available switches and saves it', async () => {
  i18next.addResourceBundle('fr', 'translation', {
    'Change language': 'Changer de langue',
  })

  await chooseLanguage('Français')

  expect(
    await screen.findByRole('button', { name: 'Changer de langue' })
  ).toBeInTheDocument()
  expect(api.put).toHaveBeenCalledWith('/api/user/self', { language: 'fr' })
})
