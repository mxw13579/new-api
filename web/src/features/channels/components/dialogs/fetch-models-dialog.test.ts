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
  getFetchModelsContentState,
  getFetchModelsDefaultTab,
  resolveFetchChannelId,
} from '../../lib/upstream-update-utils'

describe('fetch models dialog branch contracts', () => {
  test('resolves custom, selected-channel, and missing-channel paths safely', () => {
    assert.equal(resolveFetchChannelId(true, { id: 7 }), null)
    assert.equal(resolveFetchChannelId(false, { id: 7 }), 7)
    assert.equal(resolveFetchChannelId(false, null), null)
  })

  test('covers every content and default-tab branch', () => {
    assert.equal(
      getFetchModelsContentState(false, false, false, 0, 0),
      'missing'
    )
    assert.equal(getFetchModelsContentState(true, false, true, 0, 0), 'loading')
    assert.equal(getFetchModelsContentState(true, false, false, 0, 0), 'empty')
    assert.equal(getFetchModelsContentState(false, true, false, 1, 0), 'models')
    assert.equal(getFetchModelsDefaultTab(1, 1), 'new')
    assert.equal(getFetchModelsDefaultTab(0, 1), 'removed')
    assert.equal(getFetchModelsDefaultTab(0, 0), 'existing')
  })
})
