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

import { InvoiceApiError, type InvoiceHttpTransport } from '../invoices/api'
import {
  createHttpAdminInvoiceApi,
  createHttpAdminInvoiceFeeLedgerApi,
  createHttpInvoiceSettingsApi,
} from './api'

describe('admin invoice HTTP adapters', () => {
  test('use isolated endpoints and both skip flags on every request', async () => {
    const calls: Array<{
      method: string
      url: string
      body?: unknown
      config: unknown
    }> = []
    const transport: InvoiceHttpTransport = {
      get: async (url, config) => {
        calls.push({ method: 'GET', url, config })
        return { data: { success: true, message: '', data: {} } }
      },
      post: async (url, body, config) => {
        calls.push({ method: 'POST', url, body, config })
        return { data: { success: true, message: '', data: {} } }
      },
      put: async (url, body, config) => {
        calls.push({ method: 'PUT', url, body, config })
        return { data: { success: true, message: '', data: body } }
      },
      delete: async () => {
        throw new Error('unused')
      },
    }
    const adminApi = createHttpAdminInvoiceApi(transport)
    const adminFeeApi = createHttpAdminInvoiceFeeLedgerApi(transport)
    const settingsApi = createHttpInvoiceSettingsApi(transport)
    const document = new FormData()
    document.set(
      'file',
      new File(['%PDF-1.7'], 'invoice.pdf', { type: 'application/pdf' })
    )

    await adminApi.listApplications({ page: 3, page_size: 20 })
    await adminApi.getApplication(17)
    await adminApi.reviewApplication(17, {
      action: 'reviewing',
      expected_status: 'submitted',
    })
    await adminApi.rejectApplication(17, {
      expected_status: 'reviewing',
      reason: 'Incorrect tax identity',
    })
    await adminApi.uploadDocument(17, document)
    await adminFeeApi.list({ page: 2, page_size: 10 })
    await settingsApi.getSetting()
    await settingsApi.updateSetting({
      personal_enabled: true,
      company_enabled: false,
      application_window_days: 30,
      minimum_amount_minor: 100,
      fee_percent: 5,
      pdf_retention_days: 90,
      r2_endpoint:
        'https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com',
      r2_bucket: 'private-invoices',
      r2_access_key_id: 'access-id',
      r2_secret_access_key: '',
    })

    assert.deepEqual(
      calls.map(({ method, url }) => ({ method, url })),
      [
        { method: 'GET', url: '/api/admin/invoices?page=3&page_size=20' },
        { method: 'GET', url: '/api/admin/invoices/17' },
        { method: 'POST', url: '/api/admin/invoices/17/review' },
        { method: 'POST', url: '/api/admin/invoices/17/reject' },
        { method: 'POST', url: '/api/admin/invoices/17/document' },
        {
          method: 'GET',
          url: '/api/admin/invoice/fee-ledger?page=2&page_size=10',
        },
        { method: 'GET', url: '/api/option/invoice' },
        { method: 'PUT', url: '/api/option/invoice' },
      ]
    )
    assert.deepEqual(calls[2].body, {
      action: 'reviewing',
      expected_status: 'submitted',
    })
    assert.deepEqual(calls[3].body, {
      expected_status: 'reviewing',
      reason: 'Incorrect tax identity',
    })
    assert.deepEqual(calls.at(-1)?.body, {
      personal_enabled: true,
      company_enabled: false,
      application_window_days: 30,
      minimum_amount_minor: 100,
      fee_percent: 5,
      pdf_retention_days: 90,
      r2_endpoint:
        'https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com',
      r2_bucket: 'private-invoices',
      r2_access_key_id: 'access-id',
      r2_secret_access_key: '',
    })
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

  test('preserves stable conflict codes from rejected admin envelopes', async () => {
    const transport: InvoiceHttpTransport = {
      get: async () => {
        throw new Error('unused')
      },
      post: async () => {
        throw {
          response: {
            data: {
              success: false,
              message: 'do not render server text',
              data: { code: 'INVOICE_STATE_CONFLICT' },
            },
          },
        }
      },
      put: async () => {
        throw new Error('unused')
      },
      delete: async () => {
        throw new Error('unused')
      },
    }

    await assert.rejects(
      createHttpAdminInvoiceApi(transport).reviewApplication(9, {
        action: 'approve',
        expected_status: 'reviewing',
      }),
      (error: unknown) =>
        error instanceof InvoiceApiError &&
        error.code === 'INVOICE_STATE_CONFLICT'
    )
  })
})
