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
  EligibleInvoiceOrder,
  InvoiceApplicationStatus,
  InvoiceDocumentStatus,
  InvoiceErrorCode,
  InvoiceType,
} from './types'

interface DraftIdentity {
  forDraft(fingerprint: string): string
  reset(): void
}

export type InvoiceOrderSelection = ReadonlyMap<number, number>

interface InvoiceOrderSelectionSummary {
  topupIds: number[]
  amountMinor: number
  minimumReached: boolean
}

/** Creates an empty selection that retains amounts across order pages. */
export function createInvoiceOrderSelection(): InvoiceOrderSelection {
  return new Map()
}

/** Adds or removes one complete order from the invoice draft selection. */
export function updateInvoiceOrderSelection(
  selection: InvoiceOrderSelection,
  order: Pick<EligibleInvoiceOrder, 'topup_id' | 'paid_amount_minor'>,
  selected: boolean
): InvoiceOrderSelection {
  const next = new Map(selection)
  if (selected) next.set(order.topup_id, order.paid_amount_minor)
  else next.delete(order.topup_id)
  return next
}

/** Derives display, validation, and submission values from one selection. */
export function getInvoiceOrderSelectionSummary(
  selection: InvoiceOrderSelection,
  minimumAmountMinor: number
): InvoiceOrderSelectionSummary {
  const topupIds = [...selection.keys()].sort((left, right) => left - right)
  const amountMinor = [...selection.values()].reduce(
    (total, amount) => total + amount,
    0
  )
  return {
    topupIds,
    amountMinor,
    minimumReached: amountMinor >= minimumAmountMinor,
  }
}

/** Reconciles selected orders after a server eligibility decision. */
export function reconcileInvoiceOrderSelectionAfterError(
  selection: InvoiceOrderSelection,
  code: InvoiceErrorCode
): InvoiceOrderSelection {
  const eligibilityConflict =
    code === 'INVOICE_TOPUP_INELIGIBLE' ||
    code === 'INVOICE_PAYMENT_EVIDENCE_CONFLICT'
  return eligibilityConflict ? createInvoiceOrderSelection() : selection
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
