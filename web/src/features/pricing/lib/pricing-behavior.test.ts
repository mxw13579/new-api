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

import { getPricingItemIdentity, normalizeCondition } from './billing-expr'

describe('pricing behavior contracts', () => {
  it('normalizes every condition-source branch without changing valid input', () => {
    assert.equal(normalizeCondition(undefined).source, 'param')
    assert.equal(normalizeCondition({ source: 'header' }).source, 'header')
    assert.deepEqual(
      normalizeCondition({
        source: 'time',
        timeFunc: 'minute',
        timezone: 'UTC',
        mode: 'gte',
        value: '5',
      }),
      {
        source: 'time',
        timeFunc: 'minute',
        timezone: 'UTC',
        mode: 'gte',
        value: '5',
        rangeStart: '',
        rangeEnd: '',
      }
    )
  })

  it('uses content identity that survives insertion and reorder', () => {
    const first = {
      label: 'small',
      conditions: [{ var: 'p', op: '<', value: 10 }],
    }
    const second = {
      label: 'large',
      conditions: [{ var: 'p', op: '>=', value: 10 }],
    }

    const before = [first, second].map(getPricingItemIdentity)
    const after = [second, first].map(getPricingItemIdentity)

    assert.equal(before[0], after[1])
    assert.equal(before[1], after[0])
    assert.notEqual(before[0], before[1])
  })
})
