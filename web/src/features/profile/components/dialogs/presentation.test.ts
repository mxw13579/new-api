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
  getEmailVerificationButtonLabel,
  getTwoFactorSetupView,
} from './presentation'

describe('profile dialog presentation', () => {
  test('preserves countdown, sending and idle labels', () => {
    assert.deepEqual(getEmailVerificationButtonLabel(true, true, 7), {
      kind: 'literal',
      value: '7s',
    })
    assert.deepEqual(getEmailVerificationButtonLabel(false, true, 7), {
      kind: 'translation',
      value: 'Sending...',
    })
    assert.deepEqual(getEmailVerificationButtonLabel(false, false, 7), {
      kind: 'translation',
      value: 'Send',
    })
  })

  test('preserves initializing, failed and ready setup branches', () => {
    assert.equal(getTwoFactorSetupView(true, false), 'loading')
    assert.equal(getTwoFactorSetupView(false, false), 'error')
    assert.equal(getTwoFactorSetupView(false, true), 'ready')
  })
})
