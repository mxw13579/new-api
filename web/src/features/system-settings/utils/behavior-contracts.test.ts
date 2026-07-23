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

import { cloneTemplate } from '../general/channel-affinity/constants'
import { normalizeAmountOptions } from '../integrations/utils'
import {
  reconcileEditableListKeys,
  type EditableListKeyEntry,
} from '../models/utils'
import { extractJsonErrorPosition } from './json-parser'

describe('system settings behavior contracts', () => {
  it('preserves numeric coercion for numbers, strings, empty values, NaN, and infinities', () => {
    expect(
      normalizeAmountOptions([
        20,
        '10.5',
        '',
        Number.NaN,
        Number.POSITIVE_INFINITY,
        'not-a-number',
      ])
    ).toEqual([0, 10.5, 20, Number.POSITIVE_INFINITY])
  })

  it('deep-clones affinity templates without sharing nested collections', () => {
    const template = { rules: [{ paths: ['/v1/chat'], enabled: true }] }
    const cloned = cloneTemplate(template)

    expect(cloned).toEqual(template)
    expect(cloned === template).toBe(false)
    expect(cloned.rules === template.rules).toBe(false)
    expect(cloned.rules[0] === template.rules[0]).toBe(false)
  })

  it('extracts equivalent JSON error locations from position and line/column messages', () => {
    expect(
      extractJsonErrorPosition(
        new SyntaxError('Unexpected token at position 8'),
        '{\n  "a": 1\n}'
      )
    ).toEqual({ line: 2, column: 7, position: 8 })
    expect(
      extractJsonErrorPosition(
        new SyntaxError('JSON.parse error at line 4 column 9'),
        '{}'
      )
    ).toEqual({ line: 4, column: 9 })
    expect(extractJsonErrorPosition('not an error', '{}')).toEqual({})
  })

  it('keeps editable keys with logical items across edit, insert, delete, and reorder', () => {
    const first = { label: 'first' }
    const second = { label: 'second' }
    const third = { label: 'third' }
    let nextKey = 0
    const createKey = () => `row-${nextKey++}`
    let entries: EditableListKeyEntry<{ label: string }>[] =
      reconcileEditableListKeys([], [first, second], createKey)

    expect(entries.map((entry) => entry.key)).toEqual(['row-0', 'row-1'])

    const editedSecond = { label: 'second edited' }
    entries = reconcileEditableListKeys(
      entries,
      [first, editedSecond],
      createKey
    )
    expect(entries.map((entry) => entry.key)).toEqual(['row-0', 'row-1'])

    entries = reconcileEditableListKeys(
      entries,
      [third, first, editedSecond],
      createKey
    )
    expect(entries.map((entry) => entry.key)).toEqual([
      'row-2',
      'row-0',
      'row-1',
    ])

    entries = reconcileEditableListKeys(
      entries,
      [editedSecond, third],
      createKey
    )
    expect(entries.map((entry) => entry.key)).toEqual(['row-1', 'row-2'])
  })
})
