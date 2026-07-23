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

import type { QuotaDataItem } from '../types'
import { processChartData } from './charts'
import { safeDivide } from './stats'

describe('dashboard numeric and nullability contracts', () => {
  it('accepts only finite division results, including numeric edge cases', () => {
    assert.equal(safeDivide(1, 2), 0.5)
    assert.equal(safeDivide(1, 0), 0)
    assert.equal(safeDivide(0, 0), 0)
    assert.equal(safeDivide(Number.NaN, 2), 0)
    assert.equal(safeDivide(Number.POSITIVE_INFINITY, 2), 0)
  })

  it('aggregates duplicate chart buckets without nullable map assertions', () => {
    const rows = [
      {
        created_at: 1_800_000_000,
        model_name: 'model-a',
        quota: 10,
        count: 1,
        token_used: 2,
      },
      {
        created_at: 1_800_000_000,
        model_name: 'model-a',
        quota: 20,
        count: 2,
        token_used: 3,
      },
    ] as QuotaDataItem[]

    const result = processChartData(rows, 'day', (key) => key)
    const barValues = result.spec_line.data[0].values as Array<
      Record<string, unknown>
    >
    const modelPoint = barValues.find((point) => point.rawQuota === 30)

    assert.equal(modelPoint?.rawQuota, 30)
    assert.equal(modelPoint?.TimeSum, 30)
    assert.equal(result.totalCountDisplay, '3')
  })
})
