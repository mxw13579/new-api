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
  InvoiceApplicationStatus,
  InvoiceApplicationSummary,
  InvoiceDocumentStatus,
  InvoiceErrorCode,
  InvoiceFeeStatus,
  InvoicePaymentReviewStatus,
} from './types'

type StatusVariant = 'default' | 'secondary' | 'destructive' | 'outline'
interface StatusConfig {
  labelKey: string
  variant: StatusVariant
}

/** Maps application states to localized badge presentation. */
export const APPLICATION_STATUS_CONFIG: Record<
  InvoiceApplicationStatus,
  StatusConfig
> = {
  submitted: { labelKey: 'Invoice submitted', variant: 'secondary' },
  reviewing: { labelKey: 'Invoice reviewing', variant: 'outline' },
  approved: { labelKey: 'Invoice approved', variant: 'default' },
  rejected: { labelKey: 'Invoice rejected', variant: 'destructive' },
  cancelled: { labelKey: 'Invoice cancelled', variant: 'secondary' },
  issued: { labelKey: 'Invoice issued', variant: 'default' },
}

/** Maps fee states to localized badge presentation. */
export const FEE_STATUS_CONFIG: Record<InvoiceFeeStatus, StatusConfig> = {
  not_required: { labelKey: 'Invoice fee not required', variant: 'secondary' },
  paid: { labelKey: 'Invoice fee paid', variant: 'default' },
  refund_pending: {
    labelKey: 'Invoice fee refund pending',
    variant: 'outline',
  },
  refunded: { labelKey: 'Invoice fee refunded', variant: 'secondary' },
}

/** Maps payment-review states to localized badge presentation. */
export const PAYMENT_REVIEW_STATUS_CONFIG: Record<
  InvoicePaymentReviewStatus,
  StatusConfig
> = {
  none: { labelKey: 'Invoice payment review clear', variant: 'secondary' },
  pre_issue_hold: {
    labelKey: 'Invoice payment review pre-issue hold',
    variant: 'destructive',
  },
  post_issue_hold: {
    labelKey: 'Invoice payment review post-issue hold',
    variant: 'destructive',
  },
  resolved_valid: {
    labelKey: 'Invoice payment review resolved valid',
    variant: 'default',
  },
  resolved_voided: {
    labelKey: 'Invoice payment review resolved voided',
    variant: 'destructive',
  },
}

/** Maps document states to localized badge presentation. */
export const DOCUMENT_STATUS_CONFIG: Record<
  InvoiceDocumentStatus,
  StatusConfig
> = {
  uploading: { labelKey: 'Invoice document uploading', variant: 'outline' },
  validating: { labelKey: 'Invoice document validating', variant: 'outline' },
  available: { labelKey: 'Invoice document available', variant: 'default' },
  superseded: { labelKey: 'Invoice document superseded', variant: 'secondary' },
  upload_failed: {
    labelKey: 'Invoice document upload failed',
    variant: 'destructive',
  },
  deleting: { labelKey: 'Invoice document deleting', variant: 'outline' },
  deleted: { labelKey: 'Invoice document deleted', variant: 'secondary' },
  delete_failed: {
    labelKey: 'Invoice document delete failed',
    variant: 'destructive',
  },
  missing: { labelKey: 'Invoice document missing', variant: 'destructive' },
}

const ERROR_MESSAGE_KEYS: Record<InvoiceErrorCode, string> = {
  INVOICE_INVALID_REQUEST: 'Invoice error: invalid request',
  INVOICE_FORBIDDEN: 'Invoice error: permission denied',
  INVOICE_QUOTA_INSUFFICIENT: 'Invoice error: insufficient wallet quota',
  INVOICE_NOT_FOUND: 'Invoice error: not found',
  INVOICE_IDEMPOTENCY_CONFLICT: 'Invoice error: duplicate request conflict',
  INVOICE_STATE_CONFLICT: 'Invoice error: data changed, refresh and try again',
  INVOICE_TOPUP_INELIGIBLE:
    'Invoice error: selected order is no longer eligible',
  INVOICE_PAYMENT_EVIDENCE_CONFLICT:
    'Invoice error: payment evidence changed, refresh and try again',
  INVOICE_DOCUMENT_UNAVAILABLE: 'Invoice error: document unavailable',
  INVOICE_STORAGE_NOT_CONFIGURED:
    'Invoice PDF storage is not configured or invalid',
  INVOICE_ISSUANCE_CONFLICT:
    'Invoice error: invoice number or issuance facts conflict',
  INVOICE_INTERNAL_ERROR: 'Invoice error: service unavailable',
}

const INVOICE_CURRENCY_FORMATTER = new Intl.NumberFormat(undefined, {
  style: 'currency',
  currency: 'CNY',
})

/** Returns the localization key for a stable invoice error code. */
export function getInvoiceErrorMessageKey(code: InvoiceErrorCode): string {
  return ERROR_MESSAGE_KEYS[code]
}

/** Formats integer CNY minor units for invoice-domain presentation. */
export function formatInvoiceAmount(minor: number): string {
  return INVOICE_CURRENCY_FORMATTER.format(minor / 100)
}

/** Calculates the wallet quota charged for a percentage-based invoice fee. */
export function calculateInvoiceFeeQuota(
  amountMinor: number,
  feePercent: number,
  quotaPerUnit: number
): number {
  if (
    !Number.isSafeInteger(amountMinor) ||
    amountMinor < 0 ||
    !Number.isSafeInteger(feePercent) ||
    feePercent < 0 ||
    feePercent > 100 ||
    !Number.isFinite(quotaPerUnit) ||
    quotaPerUnit <= 0
  ) {
    return Number.POSITIVE_INFINITY
  }
  return Math.round((amountMinor * feePercent * quotaPerUnit) / 10_000)
}

/**
 * Determines whether an application currently permits document download.
 *
 * @param application - Application and active-document summary.
 * @param nowSeconds - Current Unix time in seconds.
 * @returns Whether the download action should be exposed.
 */
export function canDownloadInvoice(
  application: InvoiceApplicationSummary,
  nowSeconds: number
): boolean {
  const paymentReviewPermitsDownload =
    application.payment_review_status === 'none' ||
    application.payment_review_status === 'resolved_valid'
  return (
    application.can_download &&
    application.status === 'issued' &&
    paymentReviewPermitsDownload &&
    application.document_status === 'available' &&
    application.document_expires_at !== null &&
    application.document_expires_at > nowSeconds
  )
}
