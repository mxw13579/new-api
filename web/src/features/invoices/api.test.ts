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
  test('uses frozen endpoints and both skip flags for every user request', async () => {
    const calls: Array<{
      method: string
      url: string
      body?: unknown
      config?: unknown
    }> = []
    const transport: InvoiceHttpTransport = {
      get: async (url, config) => {
        calls.push({ method: 'GET', url, config })
        return { data: { success: true, message: '', data: [] } }
      },
      post: async (url, body, config) => {
        calls.push({ method: 'POST', url, body, config })
        return {
          data: {
            success: true,
            message: '',
            data: url.endsWith('/document-url')
              ? { download_url: 'https://private.example.test/invoice.pdf' }
              : body,
          },
        }
      },
      put: async (url, body, config) => {
        calls.push({ method: 'PUT', url, body, config })
        return { data: { success: true, message: '', data: body } }
      },
      delete: async (url, config) => {
        calls.push({ method: 'DELETE', url, body: config?.data, config })
        return { data: { success: true, message: '', data: null } }
      },
    }
    const invoiceApi = createHttpInvoiceApi(transport)

    await invoiceApi.getConfig()
    await invoiceApi.listProfiles()
    await invoiceApi.createProfile({
      type: 'personal',
      title: 'Alice',
      tax_number: '',
      is_default: true,
    })
    await invoiceApi.updateProfile({
      id: 4,
      expected_version: 9,
      title: 'Acme',
      tax_number: '91310000',
      is_default: true,
    })
    await invoiceApi.deleteProfile({ id: 4, expected_version: 10 })
    await invoiceApi.listEligibleOrders({ page: 2, page_size: 20 })
    await invoiceApi.createApplication({
      request_id: 'request-1',
      profile_id: 4,
      profile_version: 9,
      topup_ids: [8],
    })
    await invoiceApi.listApplications({ page: 3, page_size: 10 })
    await invoiceApi.getApplication(7)
    await invoiceApi.cancelApplication(7)
    const downloadUrl = await invoiceApi.requestDocumentDownloadUrl(7)

    assert.deepEqual(calls[3].body, {
      id: 4,
      expected_version: 9,
      title: 'Acme',
      tax_number: '91310000',
      is_default: true,
    })
    assert.deepEqual(calls[4].body, { id: 4, expected_version: 10 })
    assert.equal(calls.at(-1)?.url, '/api/user/invoices/7/document-url')
    assert.equal(downloadUrl, 'https://private.example.test/invoice.pdf')
    assert.equal(calls.length, 11)
    for (const call of calls) {
      assert.equal(
        (call.config as { skipBusinessError?: boolean }).skipBusinessError,
        true
      )
      assert.equal(
        (call.config as { skipErrorHandler?: boolean }).skipErrorHandler,
        true
      )
    }
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

  test('preserves stable codes from rejected response envelopes', async () => {
    const transport: InvoiceHttpTransport = {
      get: async () => {
        throw {
          response: {
            data: {
              success: false,
              message: 'server wording must not drive UI',
              data: { code: 'INVOICE_NOT_FOUND' },
            },
          },
        }
      },
      post: async () => {
        throw new Error('unused')
      },
      put: async () => {
        throw new Error('unused')
      },
      delete: async () => {
        throw new Error('unused')
      },
    }

    await assert.rejects(
      createHttpInvoiceApi(transport).getApplication(99),
      (error: unknown) =>
        error instanceof InvoiceApiError && error.code === 'INVOICE_NOT_FOUND'
    )
  })

  test('maps unrecognized rejected errors to the safe stable code', async () => {
    const transport: InvoiceHttpTransport = {
      get: async () => {
        throw new Error('network details')
      },
      post: async () => {
        throw new Error('unused')
      },
      put: async () => {
        throw new Error('unused')
      },
      delete: async () => {
        throw new Error('unused')
      },
    }

    await assert.rejects(
      createHttpInvoiceApi(transport).getConfig(),
      (error: unknown) =>
        error instanceof InvoiceApiError &&
        error.code === 'INVOICE_INTERNAL_ERROR'
    )
  })
})
