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
import { describe, expect, it } from 'bun:test'

import { getCacheStatsView, settleCacheStatsRequest } from './constants'

describe('cache stats request and view contract', () => {
  it('turns request rejection into a visible retryable error outcome', async () => {
    const outcome = await settleCacheStatsRequest(
      Promise.reject(new Error('network unavailable')),
      () => true
    )

    expect(outcome).toEqual({ kind: 'error', message: 'Request failed' })
  })

  it('ignores stale success and rejection outcomes', async () => {
    expect(
      await settleCacheStatsRequest(
        Promise.resolve({ success: true, data: { hit: 1 } }),
        () => false
      )
    ).toEqual({ kind: 'stale' })
    expect(
      await settleCacheStatsRequest(
        Promise.reject(new Error('late')),
        () => false
      )
    ).toEqual({ kind: 'stale' })
  })

  it('preserves loading, rows, and empty view branches', () => {
    expect(getCacheStatsView(true, 0)).toBe('loading')
    expect(getCacheStatsView(false, 2)).toBe('rows')
    expect(getCacheStatsView(false, 0)).toBe('empty')
  })
})
