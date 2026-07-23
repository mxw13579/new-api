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
  getBillingHistoryView,
  parseBillingPageSize,
} from './billing-history-presentation'

describe('billing history presentation', () => {
  test('preserves loading, empty and populated branches', () => {
    assert.equal(getBillingHistoryView(true, 3), 'loading')
    assert.equal(getBillingHistoryView(false, 0), 'empty')
    assert.equal(getBillingHistoryView(false, 3), 'records')
  })

  test('preserves page-size numeric coercion', () => {
    assert.equal(parseBillingPageSize('20'), 20)
    assert.equal(Number.isNaN(parseBillingPageSize('')), true)
    assert.equal(Number.isNaN(parseBillingPageSize('x')), true)
    assert.equal(parseBillingPageSize('Infinity'), Number.NaN)
  })
})
