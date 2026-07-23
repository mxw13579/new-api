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

import { normalizeCondition } from './billing-expr'
import { StablePricingKeyRegistry } from './stable-pricing-keys'

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

  it('keeps existing duplicate-object keys through identical insertion and reorder', () => {
    const registry = new StablePricingKeyRegistry()
    const first = {
      label: 'small',
      conditions: [{ var: 'p', op: '<', value: 10 }],
    }
    const second = {
      label: 'large',
      conditions: [{ var: 'p', op: '>=', value: 10 }],
    }
    const duplicate = structuredClone(first)
    const insertedDuplicate = structuredClone(first)

    const before = registry.withKeys([first, duplicate, second], 'tier')
    const afterInsert = registry.withKeys(
      [insertedDuplicate, first, duplicate, second],
      'tier'
    )
    const afterReorder = registry.withKeys(
      [second, duplicate, insertedDuplicate, first],
      'tier'
    )

    assert.equal(new Set(before.map(({ key }) => key)).size, before.length)
    assert.notEqual(afterInsert[0].key, before[0].key)
    assert.equal(afterInsert[1].key, before[0].key)
    assert.equal(afterInsert[2].key, before[1].key)
    assert.equal(afterInsert[3].key, before[2].key)
    assert.equal(afterReorder[0].key, before[2].key)
    assert.equal(afterReorder[1].key, before[1].key)
    assert.equal(afterReorder[2].key, afterInsert[0].key)
    assert.equal(afterReorder[3].key, before[0].key)
  })
})
