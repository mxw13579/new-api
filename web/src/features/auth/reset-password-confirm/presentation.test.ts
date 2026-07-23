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

import { getResetPasswordAction } from './presentation'

describe('reset-password action presentation', () => {
  test('preserves every success, countdown and idle branch', () => {
    assert.deepEqual(getResetPasswordAction(true, true, true, false), {
      disabled: false,
      labelKey: 'auth.resetPasswordConfirm.backToLogin',
    })
    assert.deepEqual(getResetPasswordAction(false, false, true, true), {
      disabled: true,
      labelKey: 'auth.resetPasswordConfirm.retry',
    })
    assert.deepEqual(getResetPasswordAction(false, false, false, false), {
      disabled: true,
      labelKey: 'auth.resetPasswordConfirm.confirm',
    })
    assert.deepEqual(getResetPasswordAction(false, true, false, false), {
      disabled: false,
      labelKey: 'auth.resetPasswordConfirm.confirm',
    })
    assert.deepEqual(getResetPasswordAction(false, false, true, false), {
      disabled: true,
      labelKey: 'auth.resetPasswordConfirm.retry',
    })
  })
})
