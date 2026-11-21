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
import { hashKey, type QueryClient } from '@tanstack/react-query'

import {
  ensureStatus,
  STATUS_QUERY_KEY,
  type StatusData,
} from '@/lib/status-query'

import { getSetupStatus } from '../api'

const STATUS_QUERY_HASH = hashKey(STATUS_QUERY_KEY)

/**
 * The shared `/api/status` result, or null as soon as one attempt fails.
 * Status queries retry with backoff, and the setup guard must not hold the
 * first navigation through those retries.
 */
function readStatusOnce(queryClient: QueryClient): Promise<StatusData | null> {
  return new Promise((resolve) => {
    const cache = queryClient.getQueryCache()
    const unsubscribe = cache.subscribe((event) => {
      if (
        event.query.queryHash === STATUS_QUERY_HASH &&
        event.query.state.fetchFailureCount > 0
      ) {
        settle(null)
      }
    })
    function settle(status: StatusData | null) {
      unsubscribe()
      resolve(status)
    }
    ensureStatus(queryClient)
      .then(settle)
      .catch(() => settle(null))
  })
}

/**
 * Whether this instance has finished initial setup, for the root route guard.
 *
 * Reads the `setup` flag of the shared `/api/status` request, which `main.tsx`
 * starts before React mounts, so an initialized site needs no extra request.
 * The flag is the same server value `/api/setup` reports. Only when status
 * says "not set up" (possibly a cache entry from before setup finished) or
 * cannot be read does this ask `/api/setup`, so a failed status read never
 * sends an initialized site to the setup page.
 *
 * Resolves to null when neither request answers: the caller then neither
 * redirects nor remembers the check.
 */
export async function readSetupCompleted(
  queryClient: QueryClient
): Promise<boolean | null> {
  const status = await readStatusOnce(queryClient)
  if (status?.setup === true) return true

  const setup = await getSetupStatus().catch((error: unknown) => {
    if (import.meta.env.DEV) {
      // eslint-disable-next-line no-console
      console.warn('[root.beforeLoad] setup status check failed', error)
    }
    return null
  })
  if (setup?.success && setup.data) return setup.data.status === true
  return null
}
