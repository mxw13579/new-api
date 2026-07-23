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
  categorizeModelsWithRedirect,
  extractMappingSourceModels,
  extractRedirectModels,
  formatModelsArray,
} from './model-mapping-validation'

describe('model mapping collection contracts', () => {
  test('preserves first-seen order while removing duplicates', () => {
    const sparseModels = Array<string>(4)
    sparseModels[0] = 'gpt-4'
    sparseModels[2] = 'gpt-4'
    sparseModels[3] = 'claude-3'

    assert.equal(formatModelsArray(sparseModels), 'gpt-4,,claude-3')
    assert.deepEqual(
      extractMappingSourceModels('{" alias ":"gpt-4","alias":"claude-3"}'),
      ['alias']
    )
    assert.deepEqual(
      extractRedirectModels('{"a":" gpt-4 ","b":"gpt-4","c":3}'),
      ['gpt-4']
    )
  })

  test('keeps redirect-only precedence separate from configured models', () => {
    const result = categorizeModelsWithRedirect(
      [' gpt-4 ', 'claude-3'],
      ['gpt-4', 'gemini-pro', 'gemini-pro']
    )

    assert.deepEqual(
      [...result.classificationSet],
      ['gpt-4', 'claude-3', 'gemini-pro']
    )
    assert.deepEqual([...result.redirectOnlySet], ['gemini-pro'])
  })
})
