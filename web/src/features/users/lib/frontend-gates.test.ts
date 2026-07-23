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

import {
  getQuotaModeLabel,
  loadUserForDrawer,
  parseQuotaInput,
  resolveDisabledUserRowClass,
} from './frontend-gates'

describe('user presentation and numeric coercion', () => {
  test('preserves quota parsing at the input boundary', () => {
    assert.equal(parseQuotaInput('12.5'), 12.5)
    assert.equal(parseQuotaInput(''), 0)
    assert.equal(Number.isNaN(parseQuotaInput('not-a-number')), true)
    assert.equal(parseQuotaInput('Infinity'), Number.POSITIVE_INFINITY)
  })

  test('preserves quota labels and disabled row classes', () => {
    assert.equal(getQuotaModeLabel('add'), 'Add')
    assert.equal(getQuotaModeLabel('subtract'), 'Subtract')
    assert.equal(getQuotaModeLabel('override'), 'Override')
    assert.equal(resolveDisabledUserRowClass(false, true), undefined)
    assert.equal(resolveDisabledUserRowClass(true, true), 'mobile')
    assert.equal(resolveDisabledUserRowClass(true, false), 'desktop')
  })
})

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
