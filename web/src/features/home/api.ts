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
import { api } from '@/lib/api'

import type { HomePageContentResponse } from './types'

// ============================================================================
// Home Page APIs
// ============================================================================

/**
 * Get custom home page content
 * Returns Markdown/HTML content or iframe URL
 */
export async function getHomePageContent(): Promise<HomePageContentResponse> {
  // See getNotice in @/lib/api: the global `Cache-Control: no-store` is dropped
  // so the browser can hold an ETag and revalidate, letting the server answer
  // 304. Server-side `no-cache` keeps admin edits immediate.
  const res = await api.get('/api/home_page_content', {
    headers: { 'Cache-Control': null },
  })
  return res.data
}

// The request `main.tsx` starts at boot on the home path, so it runs alongside
// `/api/status` instead of after the home route chunk mounts.
type HomePageContentPrefetch = {
  request: Promise<HomePageContentResponse>
  /** When the response arrived; undefined while it is pending. */
  receivedAt?: number
}

let prefetchedHomePageContent: HomePageContentPrefetch | null = null

/**
 * How long a prefetched response stays usable. It only covers the first page
 * load; a home page mounted later (after a redirect elsewhere and in-app
 * navigation back) requests fresh content.
 */
export const HOME_PAGE_CONTENT_PREFETCH_TTL_MS = 10_000

/** Start loading the home page content before the home route mounts. */
export function prefetchHomePageContent(): void {
  if (prefetchedHomePageContent) return
  const prefetch: HomePageContentPrefetch = {
    request: getHomePageContent(),
  }
  prefetchedHomePageContent = prefetch
  prefetch.request
    .then(() => {
      prefetch.receivedAt = Date.now()
    })
    .catch(() => {
      // A failed prefetch is dropped; the home page requests again when it
      // mounts and reports that request's failure itself.
      if (prefetchedHomePageContent === prefetch) {
        prefetchedHomePageContent = null
      }
    })
}

/**
 * The prefetched request while it is pending or recently answered (each
 * prefetch is used once), or a new request otherwise.
 */
export function takeHomePageContent(): Promise<HomePageContentResponse> {
  const prefetch = prefetchedHomePageContent
  prefetchedHomePageContent = null
  const usable =
    prefetch &&
    (prefetch.receivedAt === undefined ||
      Date.now() - prefetch.receivedAt <= HOME_PAGE_CONTENT_PREFETCH_TTL_MS)
  return usable ? prefetch.request : getHomePageContent()
}
