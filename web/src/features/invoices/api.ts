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
import type { ApiRequestConfig as ProjectApiRequestConfig } from '@/lib/api'

import type {
  AuthenticatedInvoiceApi,
  CreateInvoiceApplicationRequest,
  CreateInvoiceProfileRequest,
  DeleteInvoiceProfileRequest,
  EligibleInvoiceOrder,
  InvoiceApplicationDetail,
  InvoiceApplicationSummary,
  InvoiceConfig,
  InvoiceErrorCode,
  InvoicePage,
  InvoicePageRequest,
  InvoiceProfile,
  UpdateInvoiceProfileRequest,
} from './types'

/** Represents the common backend response envelope for invoice APIs. */
export interface InvoiceEnvelope<T> {
  success: boolean
  message: string
  data: T | { code: InvoiceErrorCode }
}

/** Represents the transport wrapper around an invoice response envelope. */
export interface InvoiceTransportResponse {
  data: InvoiceEnvelope<unknown>
}

/** Configures invoice requests to defer all user-visible errors to the feature. */
export type ApiRequestConfig = ProjectApiRequestConfig & {
  skipBusinessError: true
  skipErrorHandler: true
}

/** Suppresses transport-level error notifications for invoice requests. */
export const INVOICE_REQUEST_CONFIG: ApiRequestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
}

/** Defines the minimal HTTP transport required by the invoice adapter. */
export interface InvoiceHttpTransport {
  get(url: string, config: ApiRequestConfig): Promise<InvoiceTransportResponse>
  post(
    url: string,
    body: unknown,
    config: ApiRequestConfig
  ): Promise<InvoiceTransportResponse>
  put(
    url: string,
    body: unknown,
    config: ApiRequestConfig
  ): Promise<InvoiceTransportResponse>
  delete(
    url: string,
    config: ApiRequestConfig
  ): Promise<InvoiceTransportResponse>
}

/** Carries a stable invoice error code across the frontend API boundary. */
export class InvoiceApiError extends Error {
  code: InvoiceErrorCode

  constructor(code: InvoiceErrorCode) {
    super(code)
    this.name = 'InvoiceApiError'
    this.code = code
  }
}

const invoiceErrorCodes = new Set<InvoiceErrorCode>([
  'INVOICE_INVALID_REQUEST',
  'INVOICE_FORBIDDEN',
  'INVOICE_QUOTA_INSUFFICIENT',
  'INVOICE_NOT_FOUND',
  'INVOICE_IDEMPOTENCY_CONFLICT',
  'INVOICE_STATE_CONFLICT',
  'INVOICE_TOPUP_INELIGIBLE',
  'INVOICE_PAYMENT_EVIDENCE_CONFLICT',
  'INVOICE_DOCUMENT_UNAVAILABLE',
  'INVOICE_INTERNAL_ERROR',
])

/** Decodes resolved or rejected invoice envelopes into a stable feature error. */
export function decodeInvoiceApiError(value: unknown): InvoiceApiError {
  if (typeof value !== 'object' || value === null) {
    return new InvoiceApiError('INVOICE_INTERNAL_ERROR')
  }
  const envelope = value as {
    success?: unknown
    data?: { code?: unknown }
    response?: { data?: unknown }
  }
  if (envelope.response) return decodeInvoiceApiError(envelope.response.data)
  const code = envelope.success === false ? envelope.data?.code : undefined
  return new InvoiceApiError(
    typeof code === 'string' && invoiceErrorCodes.has(code as InvoiceErrorCode)
      ? (code as InvoiceErrorCode)
      : 'INVOICE_INTERNAL_ERROR'
  )
}

/** Executes one invoice request and normalizes both response paths. */
export async function invoiceRequest<T>(
  request: Promise<InvoiceTransportResponse>
): Promise<T> {
  try {
    const response = await request
    if (!response.data.success) throw decodeInvoiceApiError(response.data)
    return response.data.data as T
  } catch (error) {
    if (error instanceof InvoiceApiError) throw error
    throw decodeInvoiceApiError(error)
  }
}

function pageUrl(path: string, request: InvoicePageRequest): string {
  const params = new URLSearchParams({
    page: String(request.page),
    page_size: String(request.page_size),
  })
  return `${path}?${params.toString()}`
}

/**
 * Creates the production invoice API adapter for the supplied transport.
 *
 * @param transport - HTTP client used to call authenticated invoice routes.
 * @returns An invoice API backed exclusively by live HTTP requests.
 */
export function createHttpInvoiceApi(
  transport: InvoiceHttpTransport
): AuthenticatedInvoiceApi {
  return {
    async getConfig() {
      return invoiceRequest<InvoiceConfig>(
        transport.get('/api/user/invoice/config', INVOICE_REQUEST_CONFIG)
      )
    },
    async listProfiles() {
      return invoiceRequest<InvoiceProfile[]>(
        transport.get('/api/user/invoice/profiles', INVOICE_REQUEST_CONFIG)
      )
    },
    async createProfile(request: CreateInvoiceProfileRequest) {
      return invoiceRequest<InvoiceProfile>(
        transport.post(
          '/api/user/invoice/profiles',
          request,
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async updateProfile(request: UpdateInvoiceProfileRequest) {
      return invoiceRequest<InvoiceProfile>(
        transport.put(
          '/api/user/invoice/profiles',
          request,
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async deleteProfile(request: DeleteInvoiceProfileRequest) {
      await invoiceRequest<null>(
        transport.delete('/api/user/invoice/profiles', {
          ...INVOICE_REQUEST_CONFIG,
          data: request,
        })
      )
    },
    async listEligibleOrders(request: InvoicePageRequest) {
      return invoiceRequest<InvoicePage<EligibleInvoiceOrder>>(
        transport.get(
          pageUrl('/api/user/invoice/eligible-orders', request),
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async createApplication(request: CreateInvoiceApplicationRequest) {
      return invoiceRequest<InvoiceApplicationDetail>(
        transport.post('/api/user/invoices', request, INVOICE_REQUEST_CONFIG)
      )
    },
    async listApplications(request: InvoicePageRequest) {
      return invoiceRequest<InvoicePage<InvoiceApplicationSummary>>(
        transport.get(
          pageUrl('/api/user/invoices', request),
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async getApplication(applicationId: number) {
      return invoiceRequest<InvoiceApplicationDetail>(
        transport.get(
          `/api/user/invoices/${applicationId}`,
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async cancelApplication(applicationId: number) {
      return invoiceRequest<InvoiceApplicationDetail>(
        transport.post(
          `/api/user/invoices/${applicationId}/cancel`,
          undefined,
          INVOICE_REQUEST_CONFIG
        )
      )
    },
    async requestDocumentDownloadUrl(applicationId: number) {
      const response = await invoiceRequest<{ download_url: string }>(
        transport.post(
          `/api/user/invoices/${applicationId}/document-url`,
          undefined,
          INVOICE_REQUEST_CONFIG
        )
      )
      return response.download_url
    },
    getDocumentDownloadUrl(applicationId: number) {
      return `/api/user/invoices/${applicationId}/document`
    },
  }
}
