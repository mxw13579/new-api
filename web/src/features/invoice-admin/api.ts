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
import { api } from '@/lib/api'

import {
  INVOICE_REQUEST_CONFIG,
  invoiceRequest,
  type InvoiceHttpTransport,
} from '../invoices/api'
import type {
  AdminInvoiceApi,
  InvoiceApplicationDetail,
  InvoiceApplicationSummary,
  InvoiceFeeLedgerApi,
  InvoiceFeeLedgerPage,
  InvoicePage,
  InvoicePageRequest,
  InvoiceSetting,
  InvoiceSettingsApi,
  RejectInvoiceApplicationRequest,
  ReviewInvoiceApplicationRequest,
  UpdateInvoiceSettingRequest,
} from '../invoices/types'

function pageUrl(path: string, request: InvoicePageRequest): string {
  const params = new URLSearchParams({
    page: String(request.page),
    page_size: String(request.page_size),
  })
  return `${path}?${params.toString()}`
}

/** Creates the global invoice-fee ledger adapter for authorized reviewers. */
export function createHttpAdminInvoiceFeeLedgerApi(
  transport: InvoiceHttpTransport
): InvoiceFeeLedgerApi {
  return {
    async list(request: InvoicePageRequest) {
      return invoiceRequest<InvoiceFeeLedgerPage>(
        transport.get(
          pageUrl('/api/admin/invoice/fee-ledger', request),
          INVOICE_REQUEST_CONFIG
        )
      )
    },
  }
}

/** Creates the independently cached administrator invoice adapter. */
export function createHttpAdminInvoiceApi(
  transport: InvoiceHttpTransport
): AdminInvoiceApi {
  return {
    async listApplications(request: InvoicePageRequest) {
      return invoiceRequest<InvoicePage<InvoiceApplicationSummary>>(
        transport.get(
          pageUrl('/api/admin/invoices', request),
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async getApplication(applicationId: number) {
      return invoiceRequest<InvoiceApplicationDetail>(
        transport.get(
          `/api/admin/invoices/${applicationId}`,
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async reviewApplication(
      applicationId: number,
      request: ReviewInvoiceApplicationRequest
    ) {
      return invoiceRequest<InvoiceApplicationDetail>(
        transport.post(
          `/api/admin/invoices/${applicationId}/review`,
          request,
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async rejectApplication(
      applicationId: number,
      request: RejectInvoiceApplicationRequest
    ) {
      return invoiceRequest<InvoiceApplicationDetail>(
        transport.post(
          `/api/admin/invoices/${applicationId}/reject`,
          request,
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async uploadDocument(applicationId: number, document: FormData) {
      return invoiceRequest<InvoiceApplicationDetail>(
        transport.post(
          `/api/admin/invoices/${applicationId}/document`,
          document,
          INVOICE_REQUEST_CONFIG
        )
      )
    },
  }
}

/** Creates the independently permissioned complete-object settings adapter. */
export function createHttpInvoiceSettingsApi(
  transport: InvoiceHttpTransport
): InvoiceSettingsApi {
  return {
    async getSetting() {
      return invoiceRequest<InvoiceSetting>(
        transport.get('/api/option/invoice', INVOICE_REQUEST_CONFIG)
      )
    },
    async updateSetting(setting: UpdateInvoiceSettingRequest) {
      return invoiceRequest<InvoiceSetting>(
        transport.put('/api/option/invoice', setting, INVOICE_REQUEST_CONFIG)
      )
    },
  }
}

const invoiceTransport = api as unknown as InvoiceHttpTransport

/** Production administrator invoice API. */
export const adminInvoiceApi = createHttpAdminInvoiceApi(invoiceTransport)

/** Production global invoice-fee ledger API for authorized reviewers. */
export const adminInvoiceFeeLedgerApi =
  createHttpAdminInvoiceFeeLedgerApi(invoiceTransport)

/** Production invoice policy settings API. */
export const invoiceSettingsApi = createHttpInvoiceSettingsApi(invoiceTransport)
