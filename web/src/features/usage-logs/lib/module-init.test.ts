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
import { describe, it } from 'bun:test'
import assert from 'node:assert/strict'

import { buildQueryParams } from './query-params'

describe('usage-log module initialization', () => {
  it('initializes query construction independently from API fetchers', async () => {
    assert.equal(
      buildQueryParams({ page: 0, empty: '', missing: undefined }).toString(),
      'page=0'
    )

    const [apiModule, utilsModule] = await Promise.all([
      import('../api'),
      import('./utils'),
    ])
    assert.equal(typeof apiModule.getUserLogs, 'function')
    assert.equal(typeof utilsModule.fetchLogsByCategory, 'function')

    const sectionModule = await import('../section-registry')
    assert.deepEqual(sectionModule.USAGE_LOGS_SECTION_IDS, [
      'common',
      'invoice-fees',
      'drawing',
      'task',
    ])
  })
})
