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
  normalizeModelList,
  parseUpstreamUpdateMeta,
} from './upstream-update-utils'

describe('upstream update collection contracts', () => {
  test('normalizes sparse mixed collections without changing order', () => {
    const sparse = Array<unknown>(6)
    sparse[0] = ' gpt-4 '
    sparse[2] = null
    sparse[3] = 'gpt-4'
    sparse[4] = 0
    sparse[5] = 'claude'
    assert.deepEqual(normalizeModelList(sparse), ['gpt-4', 'claude'])
  })

  test('preserves update metadata precedence and rejects malformed shapes', () => {
    assert.deepEqual(
      parseUpstreamUpdateMeta({
        upstream_model_update_check_enabled: true,
        upstream_model_update_last_detected_models: [' new ', 'new'],
        upstream_model_update_last_removed_models: ['old'],
      }),
      {
        enabled: true,
        pendingAddModels: ['new'],
        pendingRemoveModels: ['old'],
      }
    )
    assert.deepEqual(parseUpstreamUpdateMeta(null), {
      enabled: false,
      pendingAddModels: [],
      pendingRemoveModels: [],
    })
  })
})
