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
  InvoiceApiError,
  createHttpInvoiceApi,
  type InvoiceHttpTransport,
} from './api'

describe('HTTP InvoiceApi adapter', () => {
  test('uses the frozen endpoints and preserves profile expected_version', async () => {
    const calls: Array<{ method: string; url: string; body?: unknown }> = []
    const transport: InvoiceHttpTransport = {
      get: async (url) => {
        calls.push({ method: 'GET', url })
        return { data: { success: true, message: '', data: [] } }
      },
      post: async (url, body) => {
        calls.push({ method: 'POST', url, body })
        return { data: { success: true, message: '', data: body } }
      },
      put: async (url, body) => {
        calls.push({ method: 'PUT', url, body })
        return { data: { success: true, message: '', data: body } }
      },
      delete: async (url, config) => {
        calls.push({ method: 'DELETE', url, body: config?.data })
        return { data: { success: true, message: '', data: null } }
      },
    }
    const invoiceApi = createHttpInvoiceApi(transport)

    await invoiceApi.updateProfile({
      id: 4,
      expected_version: 9,
      title: 'Acme',
      tax_number: '91310000',
      is_default: true,
    })
    await invoiceApi.deleteProfile({ id: 4, expected_version: 10 })

    assert.deepEqual(calls, [
      {
        method: 'PUT',
        url: '/api/user/invoice/profiles',
        body: {
          id: 4,
          expected_version: 9,
          title: 'Acme',
          tax_number: '91310000',
          is_default: true,
        },
      },
      {
        method: 'DELETE',
        url: '/api/user/invoice/profiles',
        body: { id: 4, expected_version: 10 },
      },
    ])
  })

  test('surfaces stable 409 code for profile conflict recovery', async () => {
    const transport: InvoiceHttpTransport = {
      get: async () => {
        throw new Error('unused')
      },
      post: async () => {
        throw new Error('unused')
      },
      put: async () => ({
        data: {
          success: false,
          message: 'server wording must not drive UI',
          data: { code: 'INVOICE_STATE_CONFLICT' },
        },
      }),
      delete: async () => {
        throw new Error('unused')
      },
    }
    const invoiceApi = createHttpInvoiceApi(transport)

    await assert.rejects(
      invoiceApi.updateProfile({
        id: 4,
        expected_version: 2,
        title: 'Updated',
        tax_number: '',
        is_default: false,
      }),
      (error: unknown) =>
        error instanceof InvoiceApiError &&
        error.code === 'INVOICE_STATE_CONFLICT'
    )
  })
})
