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
import { QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY, statusQueryOptions } from '@/lib/status-query'

import { readSetupCompleted } from '../lib/setup-completion'

type Reply = { setup?: boolean } | Error

let queryClient: QueryClient

/** Answers `/api/status` and `/api/setup` the way the backend does. */
function mockServer(replies: { status: Reply; setup: Reply }) {
  return vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/status') {
      if (replies.status instanceof Error) throw replies.status
      return { data: { success: true, data: replies.status } }
    }
    if (url === '/api/setup') {
      if (replies.setup instanceof Error) throw replies.setup
      return {
        data: { success: true, data: { status: replies.setup.setup } },
      }
    }
    throw new Error(`unexpected request ${String(url)}`)
  })
}

function requestedUrls(get: ReturnType<typeof mockServer>) {
  return get.mock.calls.map((call) => call[0])
}

function createClient() {
  // Production retries a failed status read with backoff; a long delay
  // proves the guard does not wait for those retries.
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: 3, retryDelay: 60_000 } },
  })
  return queryClient
}

afterEach(() => {
  queryClient.clear()
  localStorage.clear()
})

describe('readSetupCompleted', () => {
  it('trusts a status that reports setup done and skips /api/setup', async () => {
    const get = mockServer({ status: { setup: true }, setup: { setup: true } })

    await expect(readSetupCompleted(createClient())).resolves.toBe(true)
    expect(requestedUrls(get)).toEqual(['/api/status'])
  })

  it('joins the status request already in flight instead of sending another', async () => {
    const get = mockServer({ status: { setup: true }, setup: { setup: true } })
    const client = createClient()
    const prefetch = client.ensureQueryData(statusQueryOptions)

    await expect(readSetupCompleted(client)).resolves.toBe(true)
    await prefetch
    expect(requestedUrls(get)).toEqual(['/api/status'])
  })

  it('confirms with /api/setup when status reports setup not done', async () => {
    const get = mockServer({
      status: { setup: false },
      setup: { setup: false },
    })

    await expect(readSetupCompleted(createClient())).resolves.toBe(false)
    expect(requestedUrls(get)).toEqual(['/api/status', '/api/setup'])
  })

  it('lets /api/setup override a status that still says setup is not done', async () => {
    mockServer({ status: { setup: false }, setup: { setup: true } })

    await expect(readSetupCompleted(createClient())).resolves.toBe(true)
  })

  it('falls back to /api/setup after the first failed status attempt without waiting for retries', async () => {
    const get = mockServer({
      status: new Error('Network Error'),
      setup: { setup: true },
    })

    await expect(readSetupCompleted(createClient())).resolves.toBe(true)
    expect(requestedUrls(get)).toEqual(['/api/status', '/api/setup'])
  })

  it('falls back to /api/setup when the prefetched status already failed before the guard ran', async () => {
    const get = mockServer({
      status: new Error('Network Error'),
      setup: { setup: true },
    })
    const client = createClient()
    // main.tsx starts status before the guard runs; its first attempt fails
    // and the query waits for a retry.
    client.ensureQueryData(statusQueryOptions).catch(() => undefined)
    await vi.waitFor(() =>
      expect(client.getQueryState(STATUS_QUERY_KEY)?.fetchFailureCount).toBe(1)
    )

    await expect(readSetupCompleted(client)).resolves.toBe(true)
    expect(requestedUrls(get)).toEqual(['/api/status', '/api/setup'])
  })

  it('resolves to null when neither status nor /api/setup answers', async () => {
    mockServer({
      status: new Error('Network Error'),
      setup: new Error('Network Error'),
    })

    await expect(readSetupCompleted(createClient())).resolves.toBeNull()
  })
})
