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

import type { ApiResponse, Redemption } from '../types'
import { loadRedemptionForEdit } from './redemption-edit-loader'

const redemption = { id: 7, name: 'fresh' } as Redemption

describe('redemption edit loading', () => {
  it('ignores a stale response after the selected row changes', async () => {
    let active = true
    let resetValue: Redemption | undefined
    const request = Promise.resolve({ success: true, data: redemption })

    active = false
    await loadRedemptionForEdit({
      request,
      isActive: () => active,
      onLoaded: (value) => {
        resetValue = value
      },
      onError: () => assert.fail('stale requests must not show an error'),
      fallbackMessage: 'failed',
    })

    assert.equal(resetValue, undefined)
  })

  it('makes rejected and unsuccessful requests visible while active', async () => {
    const errors: string[] = []
    const common = {
      isActive: () => true,
      onLoaded: () => assert.fail('failed requests must not reset the form'),
      onError: (message: string) => errors.push(message),
      fallbackMessage: 'failed',
    }

    await loadRedemptionForEdit({
      ...common,
      request: Promise.reject(new Error('network')),
    })
    await loadRedemptionForEdit({
      ...common,
      request: Promise.resolve({
        success: false,
        message: 'denied',
      } as ApiResponse<Redemption>),
    })

    assert.deepEqual(errors, ['failed', 'denied'])
  })
})
