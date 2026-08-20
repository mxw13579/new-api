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
  createHttpInvoiceFeeLedgerApi,
  createHttpInvoiceApi,
  downloadInvoiceDocument,
  saveInvoiceDocumentBlob,
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
            data: body,
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
    const feeLedgerApi = createHttpInvoiceFeeLedgerApi(transport)

    await invoiceApi.getConfig()
    await invoiceApi.listProfiles()
    await invoiceApi.createProfile({
      type: 'personal',
      title: 'Alice',
      tax_number: '',
      identity_card_number: '11010519491231002X',
      is_default: true,
    })
    await invoiceApi.updateProfile({
      id: 4,
      expected_version: 9,
      title: 'Acme',
      tax_number: '91310000',
      identity_card_number: '',
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
    await feeLedgerApi.list({ page: 4, page_size: 25 })
    assert.deepEqual(calls[3].body, {
      id: 4,
      expected_version: 9,
      title: 'Acme',
      tax_number: '91310000',
      is_default: true,
    })
    assert.deepEqual(calls[4].body, { id: 4, expected_version: 10 })
    assert.equal(
      calls.at(-1)?.url,
      '/api/user/invoice/fee-ledger?page=4&page_size=25'
    )
    assert.equal(calls.length, 11)
    assert.equal('requestDocumentDownloadUrl' in invoiceApi, false)
    assert.equal('getDocumentDownloadUrl' in invoiceApi, false)
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

  test('downloads the authenticated document as a blob without a URL envelope', async () => {
    const expected = new Blob(['%PDF-1.7 test'], { type: 'application/pdf' })
    const calls: Array<{ url: string; config: unknown }> = []
    const transport = {
      get: async (url: string, config: unknown) => {
        calls.push({ url, config })
        return { data: expected }
      },
    }

    const document = await downloadInvoiceDocument(transport, 7)

    assert.equal(document, expected)
    assert.deepEqual(calls, [
      {
        url: '/api/user/invoices/7/document',
        config: {
          skipBusinessError: true,
          skipErrorHandler: true,
          responseType: 'blob',
        },
      },
    ])
  })

  test('decodes a binary-response JSON error without exposing response text', async () => {
    const transport = {
      get: async () => {
        throw {
          response: {
            data: new Blob(
              [
                JSON.stringify({
                  success: false,
                  message: 'provider key must stay hidden',
                  data: { code: 'INVOICE_NOT_FOUND' },
                }),
              ],
              { type: 'application/json' }
            ),
          },
        }
      },
    }

    await assert.rejects(
      downloadInvoiceDocument(transport, 99),
      (error: unknown) =>
        error instanceof InvoiceApiError && error.code === 'INVOICE_NOT_FOUND'
    )
  })

  test('uses a detached anchor and revokes the handler-local object URL', () => {
    const originalDocument = globalThis.document
    const originalCreateObjectURL = URL.createObjectURL
    const originalRevokeObjectURL = URL.revokeObjectURL
    const events: string[] = []
    const anchor = {
      download: '',
      href: '',
      click() {
        events.push('click')
      },
    }
    try {
      Object.defineProperty(globalThis, 'document', {
        configurable: true,
        value: {
          body: {
            appendChild() {
              throw new Error('download anchor must remain detached')
            },
          },
          createElement(tagName: string) {
            assert.equal(tagName, 'a')
            events.push('create-anchor')
            return anchor
          },
        },
      })
      URL.createObjectURL = () => {
        events.push('create-url')
        return 'blob:invoice-handler-local'
      }
      URL.revokeObjectURL = (url) => {
        assert.equal(url, 'blob:invoice-handler-local')
        events.push('revoke-url')
      }

      saveInvoiceDocumentBlob(new Blob(['%PDF-1.7']), 42)

      assert.equal(anchor.href, 'blob:invoice-handler-local')
      assert.equal(anchor.download, 'invoice-42.pdf')
      assert.deepEqual(events, [
        'create-url',
        'create-anchor',
        'click',
        'revoke-url',
      ])
    } finally {
      Object.defineProperty(globalThis, 'document', {
        configurable: true,
        value: originalDocument,
      })
      URL.createObjectURL = originalCreateObjectURL
      URL.revokeObjectURL = originalRevokeObjectURL
    }
  })

  test('revokes the object URL when the detached anchor click fails', () => {
    const originalDocument = globalThis.document
    const originalCreateObjectURL = URL.createObjectURL
    const originalRevokeObjectURL = URL.revokeObjectURL
    let revoked = false
    try {
      Object.defineProperty(globalThis, 'document', {
        configurable: true,
        value: {
          createElement() {
            return {
              download: '',
              href: '',
              click() {
                throw new Error('browser click failed')
              },
            }
          },
        },
      })
      URL.createObjectURL = () => 'blob:invoice-handler-local'
      URL.revokeObjectURL = () => {
        revoked = true
      }

      assert.throws(
        () => saveInvoiceDocumentBlob(new Blob(['%PDF-1.7']), 42),
        /browser click failed/
      )
      assert.equal(revoked, true)
    } finally {
      Object.defineProperty(globalThis, 'document', {
        configurable: true,
        value: originalDocument,
      })
      URL.createObjectURL = originalCreateObjectURL
      URL.revokeObjectURL = originalRevokeObjectURL
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
        identity_card_number: '11010519491231002X',
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

  test('preserves issuance conflicts from rejected response envelopes', async () => {
    const transport: InvoiceHttpTransport = {
      get: async () => {
        throw new Error('unused')
      },
      post: async () => {
        throw {
          response: {
            data: {
              success: false,
              message: 'duplicate invoice number',
              data: { code: 'INVOICE_ISSUANCE_CONFLICT' },
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
      createHttpInvoiceApi(transport).createApplication({
        request_id: 'issuance-conflict',
        profile_id: 1,
        profile_version: 1,
        topup_ids: [7],
      }),
      (error: unknown) =>
        error instanceof InvoiceApiError &&
        error.code === 'INVOICE_ISSUANCE_CONFLICT'
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
