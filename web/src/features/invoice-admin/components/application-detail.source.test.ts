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
import { readFile } from 'node:fs/promises'
import { describe, test } from 'node:test'

describe('invoice administrator detail composition', () => {
  test('delegates immutable facts to stable domain sections', async () => {
    const detailSource = await readFile(
      new URL('./application-detail.tsx', import.meta.url),
      'utf8'
    )
    const sectionsSource = await readFile(
      new URL('./application-detail-sections.tsx', import.meta.url),
      'utf8'
    )

    for (const section of [
      'ApplicationProfileSection',
      'ApplicationItemsSection',
      'ApplicationIssuanceSection',
    ]) {
      assert.match(detailSource, new RegExp(`<${section}`))
      assert.match(sectionsSource, new RegExp(`function ${section}`))
    }
    assert.doesNotMatch(detailSource, /detail\.items\.map/)
    assert.doesNotMatch(detailSource, /detail\.profile_snapshot\.tax_number/)
  })

  test('keeps review and upload permission boundaries in the detail shell', async () => {
    const source = await readFile(
      new URL('./application-detail.tsx', import.meta.url),
      'utf8'
    )

    assert.match(source, /<ReviewActions/)
    assert.match(source, /props\.canUploadDocument && uploadEligible/)
    assert.match(
      source,
      /maskInvoiceSensitiveDetail\([\s\S]*props\.canReadSensitive/
    )
  })
})
