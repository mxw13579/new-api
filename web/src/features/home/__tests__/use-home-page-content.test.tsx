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
import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { prefetchHomePageContent } from '../api'
import { useHomePageContent } from '../hooks'

const STORAGE_KEY = 'home_page_content'

type Deferred = { resolve: (data: string) => void }

/** Holds each `/api/home_page_content` request until the test answers it. */
function mockContentRequests() {
  const pending: Deferred[] = []
  const get = vi.spyOn(api, 'get').mockImplementation(
    () =>
      new Promise((resolve) => {
        pending.push({
          resolve: (data) => resolve({ data: { success: true, data } }),
        })
      })
  )
  return { get, pending }
}

function renderCountingHook() {
  let renders = 0
  const hook = renderHook(() => {
    renders += 1
    return useHomePageContent()
  })
  return { ...hook, renders: () => renders }
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  localStorage.clear()
})

describe('useHomePageContent', () => {
  it('renders cached content immediately while the request is pending', () => {
    localStorage.setItem(STORAGE_KEY, 'https://home.example.com')
    mockContentRequests()

    const { result } = renderHook(() => useHomePageContent())

    expect(result.current).toEqual({
      content: 'https://home.example.com',
      isLoaded: true,
      isUrl: true,
    })
  })

  it('does not re-render when the response matches the cached content', async () => {
    localStorage.setItem(STORAGE_KEY, 'https://home.example.com')
    const { pending } = mockContentRequests()
    const { result, renders } = renderCountingHook()
    const rendersBeforeResponse = renders()

    pending[0].resolve('https://home.example.com')
    await waitFor(() =>
      expect(localStorage.getItem(STORAGE_KEY)).toBe('https://home.example.com')
    )

    expect(renders()).toBe(rendersBeforeResponse)
    expect(result.current.content).toBe('https://home.example.com')
  })

  it('replaces cached content when the response differs', async () => {
    localStorage.setItem(STORAGE_KEY, 'https://old.example.com')
    const { pending } = mockContentRequests()
    const { result } = renderHook(() => useHomePageContent())

    pending[0].resolve('# New home')

    await waitFor(() => expect(result.current.content).toBe('# New home'))
    expect(result.current.isUrl).toBe(false)
    expect(localStorage.getItem(STORAGE_KEY)).toBe('# New home')
  })

  it('stays loading without a cache until the first response arrives', async () => {
    const { pending } = mockContentRequests()
    const { result } = renderHook(() => useHomePageContent())

    expect(result.current.isLoaded).toBe(false)

    pending[0].resolve('# Welcome')

    await waitFor(() => expect(result.current.isLoaded).toBe(true))
    expect(result.current.content).toBe('# Welcome')
  })

  it('reuses the request prefetched at boot instead of sending another', async () => {
    const { get, pending } = mockContentRequests()
    prefetchHomePageContent()

    const { result } = renderHook(() => useHomePageContent())
    pending[0].resolve('# Welcome')

    await waitFor(() => expect(result.current.content).toBe('# Welcome'))
    expect(get).toHaveBeenCalledTimes(1)
  })
})
