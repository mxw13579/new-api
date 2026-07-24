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

const componentSources = {
  settings: new URL('./settings-form.tsx', import.meta.url),
  upload: new URL('./document-upload.tsx', import.meta.url),
  review: new URL('./review-actions.tsx', import.meta.url),
}

describe('invoice administrator field state contracts', () => {
  test('keeps disabled state on each pending Field and its control', async () => {
    const settings = await readFile(componentSources.settings, 'utf8')
    const upload = await readFile(componentSources.upload, 'utf8')
    const review = await readFile(componentSources.review, 'utf8')

    assert.match(
      settings,
      /<Field[\s\S]*?data-disabled=\{props\.pending \|\| undefined\}[\s\S]*?<Input[\s\S]*?disabled=\{props\.pending\}/
    )
    assert.match(
      upload,
      /<Field[\s\S]*?data-disabled=\{props\.pending \|\| undefined\}[\s\S]*?<Input[\s\S]*?id='invoice-pdf-file'[\s\S]*?disabled=\{props\.pending\}/
    )
    assert.match(
      review,
      /<Field[\s\S]*?data-disabled=\{\s*props\.pendingAction === 'reject' \|\| undefined\s*\}[\s\S]*?<Textarea[\s\S]*?disabled=\{props\.pendingAction === 'reject'\}/
    )
  })

  test('preserves invalid state and accessible error descriptions', async () => {
    for (const sourceUrl of Object.values(componentSources)) {
      const source = await readFile(sourceUrl, 'utf8')

      assert.match(source, /data-invalid=/)
      assert.match(source, /aria-invalid=/)
      assert.match(source, /aria-describedby=/)
      assert.match(source, /<FieldDescription/)
    }
  })
})
