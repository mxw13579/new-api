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
import * as bunTest from 'bun:test'
import assert from 'node:assert/strict'

import { createElement } from 'react'
import { renderToReadableStream } from 'react-dom/server.browser'

type MockModule = (
  specifier: string,
  factory: () => Record<string, unknown>
) => void

const { describe, it } = bunTest
const mockModule = (bunTest as unknown as { mock: { module: MockModule } }).mock
  .module

mockModule('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

const { WebPreview, WebPreviewBody } = await import('./web-preview')

async function renderPreview(bodyProps: Record<string, unknown>) {
  const stream = await renderToReadableStream(
    createElement(
      WebPreview,
      { defaultUrl: 'https://preview.example/' },
      createElement(WebPreviewBody, bodyProps)
    )
  )
  return new Response(stream).text()
}

describe('WebPreviewBody security attributes', () => {
  it('runtime props cannot inject srcDoc or override the opaque policy', async () => {
    const html = await renderPreview({
      srcDoc: '<script>parent.document.body.dataset.pwned="true"</script>',
      sandbox: 'allow-same-origin allow-top-navigation',
      referrerPolicy: 'unsafe-url',
    })

    assert.match(
      html,
      /sandbox="allow-scripts allow-forms allow-popups allow-presentation"/
    )
    assert.match(html, /referrerPolicy="no-referrer"/)
    assert.doesNotMatch(html, /srcDoc|srcdoc|allow-same-origin|unsafe-url/)
  })

  it('an invalid target renders an error without an iframe', async () => {
    const html = await renderPreview({ src: 'javascript:alert(1)' })

    assert.match(html, /Unable to open this URL safely/)
    assert.doesNotMatch(html, /<iframe/)
  })

  it('rejects a target with the application origin', async () => {
    const originalLocation = Object.getOwnPropertyDescriptor(
      globalThis,
      'location'
    )
    Object.defineProperty(globalThis, 'location', {
      configurable: true,
      value: { origin: 'https://gateway.example' },
    })

    try {
      const html = await renderPreview({
        src: 'https://gateway.example/embedded',
      })

      assert.match(html, /Unable to open this URL safely/)
      assert.doesNotMatch(html, /<iframe/)
    } finally {
      if (originalLocation) {
        Object.defineProperty(globalThis, 'location', originalLocation)
      } else {
        Reflect.deleteProperty(globalThis, 'location')
      }
    }
  })
})
