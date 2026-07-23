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
import type { QueryClient } from '@tanstack/react-query'

import { invalidateSelfQuotaQuery } from '@/features/dashboard/hooks/use-self-quota'

import { invoiceQueryKeys } from './queries'
import type {
  InvoiceApplicationStatus,
  InvoiceDocumentStatus,
  InvoiceType,
} from './types'

interface DraftIdentity {
  forDraft(fingerprint: string): string
  reset(): void
}

/** Keeps one idempotency key until the user changes or resets a draft. */
export function createInvoiceDraftIdentity(
  createId: () => string = () => crypto.randomUUID()
): DraftIdentity {
  let fingerprint: string | undefined
  let requestId: string | undefined
  return {
    forDraft(nextFingerprint) {
      if (fingerprint !== nextFingerprint || requestId === undefined) {
        fingerprint = nextFingerprint
        requestId = createId()
      }
      return requestId
    },
    reset() {
      fingerprint = undefined
      requestId = undefined
    },
  }
}

/** Invalidates only user invoice cache domains plus the canonical quota. */
export async function invalidateUserInvoiceMutationQueries(
  queryClient: QueryClient,
  applicationId?: number
): Promise<void> {
  const invalidations = [
    queryClient.invalidateQueries({
      queryKey: invoiceQueryKeys.eligibleOrdersList(),
    }),
    queryClient.invalidateQueries({
      queryKey: invoiceQueryKeys.applicationsList(),
    }),
  ]
  if (applicationId !== undefined) {
    invalidations.push(
      queryClient.invalidateQueries({
        queryKey: invoiceQueryKeys.application(applicationId),
      })
    )
  }
  await Promise.all([...invalidations, invalidateSelfQuotaQuery(queryClient)])
}

/** Returns a nonzero number of pages for accessible pagination controls. */
export function invoicePageCount(total: number, pageSize: number): number {
  return Math.max(1, Math.ceil(total / pageSize))
}

/** Applies independent personal/company creation and application gates. */
export function isInvoiceProfileEnabled(
  config: Pick<
    import('./types').InvoiceConfig,
    'personal_enabled' | 'company_enabled'
  >,
  type: InvoiceType
): boolean {
  return type === 'personal' ? config.personal_enabled : config.company_enabled
}

/** Identifies application records whose lifecycle can change while visible. */
export function shouldPollInvoiceApplication(
  application: {
    status: InvoiceApplicationStatus
    document_status: InvoiceDocumentStatus
    document_expires_at: number | null
  },
  nowSeconds = Math.floor(Date.now() / 1000)
): boolean {
  const terminal = ['rejected', 'cancelled', 'issued'].includes(
    application.status
  )
  return (
    !terminal ||
    (application.document_status === 'available' &&
      application.document_expires_at !== null &&
      application.document_expires_at > nowSeconds)
  )
}
