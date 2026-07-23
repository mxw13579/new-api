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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { loadUserForDrawer } from './frontend-gates'

describe('user drawer request lifecycle', () => {
  test('does not apply a stale response after the drawer target changes', async () => {
    const applied: number[] = []
    await loadUserForDrawer(
      2,
      async () => ({ success: true, data: { id: 2 } }),
      (user) => applied.push(user.id),
      () => assert.fail('unexpected error'),
      () => false
    )
    assert.deepEqual(applied, [])
  })

  test('surfaces a current request rejection without applying data', async () => {
    const applied: number[] = []
    const errors: unknown[] = []
    await loadUserForDrawer(
      2,
      async () => {
        throw new Error('offline')
      },
      (user) => applied.push(user.id),
      (error) => errors.push(error),
      () => true
    )
    assert.deepEqual(applied, [])
    assert.equal(errors.length, 1)
  })
})
