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
import type {
  CreateInvoiceApplicationRequest,
  CreateInvoiceProfileRequest,
  DeleteInvoiceProfileRequest,
  EligibleInvoiceOrder,
  InvoiceApi,
  InvoiceApplicationDetail,
  InvoiceApplicationSummary,
  InvoiceConfig,
  InvoiceErrorCode,
  InvoicePage,
  InvoicePageRequest,
  InvoiceProfile,
  UpdateInvoiceProfileRequest,
} from './types'

interface InvoiceEnvelope<T> {
  success: boolean
  message: string
  data: T | { code: InvoiceErrorCode }
}

interface InvoiceTransportResponse {
  data: InvoiceEnvelope<unknown>
}

/** Defines the minimal HTTP transport required by the invoice adapter. */
export interface InvoiceHttpTransport {
  get(url: string): Promise<InvoiceTransportResponse>
  post(url: string, body?: unknown): Promise<InvoiceTransportResponse>
  put(url: string, body?: unknown): Promise<InvoiceTransportResponse>
  delete(
    url: string,
    config?: { data?: unknown }
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

function unwrapInvoiceEnvelope<T>(response: InvoiceTransportResponse): T {
  if (!response.data.success) {
    const errorData = response.data.data as { code?: InvoiceErrorCode }
    throw new InvoiceApiError(errorData.code || 'INVOICE_INTERNAL_ERROR')
  }
  return response.data.data as T
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
): InvoiceApi {
  return {
    async getConfig() {
      return unwrapInvoiceEnvelope<InvoiceConfig>(
        await transport.get('/api/user/invoice/config')
      )
    },
    async listProfiles() {
      return unwrapInvoiceEnvelope<InvoiceProfile[]>(
        await transport.get('/api/user/invoice/profiles')
      )
    },
    async createProfile(request: CreateInvoiceProfileRequest) {
      return unwrapInvoiceEnvelope<InvoiceProfile>(
        await transport.post('/api/user/invoice/profiles', request)
      )
    },
    async updateProfile(request: UpdateInvoiceProfileRequest) {
      return unwrapInvoiceEnvelope<InvoiceProfile>(
        await transport.put('/api/user/invoice/profiles', request)
      )
    },
    async deleteProfile(request: DeleteInvoiceProfileRequest) {
      unwrapInvoiceEnvelope<null>(
        await transport.delete('/api/user/invoice/profiles', { data: request })
      )
    },
    async listEligibleOrders(request: InvoicePageRequest) {
      return unwrapInvoiceEnvelope<InvoicePage<EligibleInvoiceOrder>>(
        await transport.get(
          pageUrl('/api/user/invoice/eligible-orders', request)
        )
      )
    },
    async createApplication(request: CreateInvoiceApplicationRequest) {
      return unwrapInvoiceEnvelope<InvoiceApplicationDetail>(
        await transport.post('/api/user/invoices', request)
      )
    },
    async listApplications(request: InvoicePageRequest) {
      return unwrapInvoiceEnvelope<InvoicePage<InvoiceApplicationSummary>>(
        await transport.get(pageUrl('/api/user/invoices', request))
      )
    },
    async getApplication(applicationId: number) {
      return unwrapInvoiceEnvelope<InvoiceApplicationDetail>(
        await transport.get(`/api/user/invoices/${applicationId}`)
      )
    },
    async cancelApplication(applicationId: number) {
      return unwrapInvoiceEnvelope<InvoiceApplicationDetail>(
        await transport.post(`/api/user/invoices/${applicationId}/cancel`)
      )
    },
    getDocumentDownloadUrl(applicationId: number) {
      return `/api/user/invoices/${applicationId}/document`
    },
  }
}
