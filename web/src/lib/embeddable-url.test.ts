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

import { resolveEmbeddableUrl, validateEmbeddableUrl } from './embeddable-url'

const applicationOrigin = 'https://gateway.example'

describe('validateEmbeddableUrl', () => {
  for (const url of [
    'https://chat.example/path',
    'https://chat.example/path?key={key}',
  ]) {
    test(`accepts an absolute external HTTPS URL: ${url}`, () => {
      assert.equal(validateEmbeddableUrl(url, { applicationOrigin }), url)
    })
  }

  for (const url of [
    '//chat.example/path',
    'javascript:alert(1)',
    'ftp://chat.example/path',
    'https://user:password@chat.example/path',
    'https://gateway.example/embedded',
    'not a URL',
    'http://chat.example/path',
  ]) {
    test(`rejects an unsafe embeddable URL: ${url}`, () => {
      assert.equal(validateEmbeddableUrl(url, { applicationOrigin }), null)
    })
  }

  test('permits only localhost HTTP when the development exception is enabled', () => {
    assert.equal(
      validateEmbeddableUrl('http://localhost:3000/preview', {
        applicationOrigin,
        allowLocalhostHttp: true,
      }),
      'http://localhost:3000/preview'
    )
    assert.equal(
      validateEmbeddableUrl('http://127.0.0.1:3000/preview', {
        applicationOrigin,
        allowLocalhostHttp: true,
      }),
      null
    )
  })
})

describe('resolveEmbeddableUrl', () => {
  test('validates the template before resolving a secret-bearing URL', () => {
    let resolverCalls = 0
    const resolver = () => {
      resolverCalls += 1
      return 'https://chat.example/?key=test-fake-key'
    }

    assert.equal(
      resolveEmbeddableUrl('https://gateway.example/?key={key}', resolver, {
        applicationOrigin,
      }),
      null
    )
    assert.equal(resolverCalls, 0)
  })

  test('returns a validated resolved URL', () => {
    let resolverCalls = 0
    const resolver = () => {
      resolverCalls += 1
      return 'https://chat.example/?key=test-fake-key'
    }

    assert.equal(
      resolveEmbeddableUrl('https://chat.example/?key={key}', resolver, {
        applicationOrigin,
      }),
      'https://chat.example/?key=test-fake-key'
    )
    assert.equal(resolverCalls, 1)
  })
})
