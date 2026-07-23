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
  cloneAdvancedCustomConfig,
  createAdvancedCustomConfig,
  getParameterOverrideFieldEditor,
  getParameterOverrideValueEditor,
  getParameterOverrideView,
} from './advanced-custom'

describe('advanced custom configuration cloning', () => {
  test('preserves nested values without sharing route identity', () => {
    const original = createAdvancedCustomConfig()
    const clone = cloneAdvancedCustomConfig(original)
    const originalRoutes = original.advanced_routes
    const clonedRoutes = clone.advanced_routes

    assert.deepEqual(clone, original)
    assert.notEqual(clone, original)
    assert.ok(originalRoutes)
    assert.ok(clonedRoutes)
    assert.notEqual(clonedRoutes, originalRoutes)
    assert.notEqual(clonedRoutes[0], originalRoutes[0])
  })
})

describe('parameter override editor truth tables', () => {
  test('selects the exact top-level and specialized editor branches', () => {
    assert.equal(getParameterOverrideView('json', 'legacy'), 'json')
    assert.equal(getParameterOverrideView('visual', 'legacy'), 'legacy')
    assert.equal(getParameterOverrideView('visual', 'operations'), 'operations')

    for (const hasDraft of [false, true]) {
      assert.equal(
        getParameterOverrideValueEditor('return_error', hasDraft, false),
        hasDraft ? 'return-error' : 'default'
      )
      assert.equal(
        getParameterOverrideValueEditor('prune_objects', false, hasDraft),
        hasDraft ? 'prune-objects' : 'default'
      )
    }
    assert.equal(getParameterOverrideValueEditor('set', true, true), 'default')
  })

  test('preserves sync-fields precedence over generic from/to fields', () => {
    assert.equal(
      getParameterOverrideFieldEditor('sync_fields', true, true),
      'sync-fields'
    )
    assert.equal(
      getParameterOverrideFieldEditor('sync_fields', false, true),
      'none'
    )
    assert.equal(getParameterOverrideFieldEditor('set', false, true), 'fields')
    assert.equal(getParameterOverrideFieldEditor('set', false, false), 'none')
  })
})
