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

import {
  getAsyncContentState,
  getOrderedItemState,
} from '../system-settings/models/utils'

describe('setup wizard branch contract', () => {
  it('classifies every step relation without changing precedence', () => {
    expect(getOrderedItemState(1, 0)).toBe('completed')
    expect(getOrderedItemState(1, 1)).toBe('active')
    expect(getOrderedItemState(1, 2)).toBe('pending')
  })

  it('gives loading precedence over errors and forms', () => {
    expect(getAsyncContentState(true, true, false)).toBe('loading')
    expect(getAsyncContentState(false, true, false)).toBe('error')
    expect(getAsyncContentState(false, false, false)).toBe('content')
  })
})
