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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import i18next from 'i18next'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'
import { getStatus } from '@/lib/api'
import { STATUS_QUERY_KEY, STATUS_STORAGE_KEY } from '@/lib/status-query'

import { useDocumentMetadata } from '../use-document-metadata'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  getStatus: vi.fn(),
}))

const SERVER_TITLE = 'Server rendered title'

let client: QueryClient

beforeEach(async () => {
  localStorage.clear()
  i18next.addResourceBundle('zhCN', 'translation', zh.translation)
  await i18next.changeLanguage('en')
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  // The status request stays pending unless a test resolves it.
  vi.mocked(getStatus).mockReturnValue(new Promise(() => {}))
  document.title = SERVER_TITLE
  document.documentElement.lang = ''
})

afterEach(async () => {
  client.clear()
  localStorage.clear()
  await i18next.changeLanguage('en')
  i18next.removeResourceBundle('zhCN', 'translation')
})

/** Render with status already known, as after a cached or fetched status. */
function renderAt(pathname: string, systemName = 'Muze AI') {
  client.setQueryData(STATUS_QUERY_KEY, { system_name: systemName })
  return renderWithClient(pathname)
}

function renderWithClient(pathname: string) {
  return renderHook(
    (props: { pathname: string }) => useDocumentMetadata(props.pathname),
    {
      initialProps: { pathname },
      wrapper: (props: { children: ReactNode }) => (
        <QueryClientProvider client={client}>
          {props.children}
        </QueryClientProvider>
      ),
    }
  )
}

describe('document title', () => {
  it('keeps the server title on a public page at load even with status', () => {
    renderAt('/pricing')

    expect(document.title).toBe(SERVER_TITLE)
  })

  it('keeps the server title on a non-public page at load', () => {
    renderAt('/dashboard')

    expect(document.title).toBe(SERVER_TITLE)
  })

  it('sets the Chinese public page titles after navigation in zhCN', async () => {
    await i18next.changeLanguage('zhCN')
    const view = renderAt('/sign-in')

    const expected: Array<[string, string]> = [
      ['/', 'Muze AI · Claude、Codex、Gemini 与 OpenAI 兼容模型的统一 API'],
      ['/pricing', '模型广场与价格 · Muze AI'],
      ['/about/', '关于 · Muze AI'],
      ['/privacy-policy', '隐私政策 · Muze AI'],
      ['/user-agreement', '用户协议 · Muze AI'],
      ['/dashboard/overview', 'Muze AI'],
      ['/sign-in', 'Muze AI'],
    ]
    for (const [pathname, title] of expected) {
      view.rerender({ pathname })
      expect(document.title).toBe(title)
    }
  })

  it('sets the title after a language change on the load page', async () => {
    renderAt('/about')
    expect(document.title).toBe(SERVER_TITLE)

    await act(() => i18next.changeLanguage('zhCN'))

    expect(document.title).toBe('关于 · Muze AI')
  })

  it('keeps special characters in the system name unescaped', () => {
    const view = renderAt('/', 'A & <B>')

    view.rerender({ pathname: '/about' })

    expect(document.title).toBe('About · A & <B>')
  })

  it('keeps the server title without status and sets it once status arrives after navigation', async () => {
    let resolveStatus: (status: Record<string, unknown>) => void = () => {}
    vi.mocked(getStatus).mockReturnValue(
      new Promise((resolve) => {
        resolveStatus = resolve
      })
    )

    const view = renderWithClient('/pricing')
    expect(getStatus).toHaveBeenCalledTimes(1)
    expect(document.title).toBe(SERVER_TITLE)

    view.rerender({ pathname: '/about' })
    expect(document.title).toBe(SERVER_TITLE)

    await act(async () => {
      resolveStatus({ system_name: 'Fresh Name' })
    })
    await waitFor(() => expect(document.title).toBe('About · Fresh Name'))
  })

  it('does not override the load page when status arrives later', async () => {
    let resolveStatus: (status: Record<string, unknown>) => void = () => {}
    vi.mocked(getStatus).mockReturnValue(
      new Promise((resolve) => {
        resolveStatus = resolve
      })
    )
    const view = renderWithClient('/pricing')

    await act(async () => {
      resolveStatus({ system_name: 'Fresh Name' })
    })
    await waitFor(() =>
      expect(client.getQueryData(STATUS_QUERY_KEY)).toEqual({
        system_name: 'Fresh Name',
      })
    )
    expect(document.title).toBe(SERVER_TITLE)

    view.rerender({ pathname: '/' })
    view.rerender({ pathname: '/pricing' })
    expect(document.title).toBe('Model marketplace and pricing · Fresh Name')
  })

  it('uses the cached status name after navigation before the request returns', () => {
    localStorage.setItem(
      STATUS_STORAGE_KEY,
      JSON.stringify({ system_name: 'Cached Name' })
    )
    const view = renderWithClient('/sign-in')

    view.rerender({ pathname: '/dashboard' })

    expect(document.title).toBe('Cached Name')
  })
})

describe('document language', () => {
  it('sets a BCP 47 lang on mount and follows language changes', async () => {
    renderAt('/')
    expect(document.documentElement.lang).toBe('en')

    await act(() => i18next.changeLanguage('zhCN'))
    expect(document.documentElement.lang).toBe('zh-CN')

    await act(() => i18next.changeLanguage('zhTW'))
    expect(document.documentElement.lang).toBe('zh-TW')
  })
})
