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
import { hasPermission } from '@/lib/admin-permissions'
import type { AuthUser } from '@/stores/auth-store'

import type {
  AdminInvoiceApi,
  InvoiceApplicationDetail,
  InvoiceApplicationStatus,
  UpdateInvoiceSettingRequest,
} from '../invoices/types'
import type {
  InvoiceAdminCapabilities,
  InvoiceDocumentUploadInput,
  InvoiceValidationResult,
} from './types'

/** Maps invoice capabilities to backend-provided permission actions. */
export const INVOICE_ADMIN_PERMISSIONS = {
  resource: 'invoice',
  review: 'review',
  documentUpload: 'document.upload',
  sensitiveRead: 'sensitive.read',
  settings: 'settings',
} as const

/** Maximum PDF size accepted by the invoice upload contract. */
export const MAX_INVOICE_PDF_BYTES = 10 * 1024 * 1024

/** Localization key substituted for permission-protected invoice values. */
export const PROTECTED_INVOICE_VALUE_KEY = 'Protected invoice value'

/** Resolves each invoice administrator capability independently. */
export function getInvoiceAdminCapabilities(
  user: AuthUser | null | undefined
): InvoiceAdminCapabilities {
  const resource = INVOICE_ADMIN_PERMISSIONS.resource
  return {
    canReview: hasPermission(user, resource, INVOICE_ADMIN_PERMISSIONS.review),
    canUploadDocument: hasPermission(
      user,
      resource,
      INVOICE_ADMIN_PERMISSIONS.documentUpload
    ),
    canReadSensitive: hasPermission(
      user,
      resource,
      INVOICE_ADMIN_PERMISSIONS.sensitiveRead
    ),
    canManageSettings: hasPermission(
      user,
      resource,
      INVOICE_ADMIN_PERMISSIONS.settings
    ),
  }
}

/** Removes sensitive values from the render model when permission is absent. */
export function maskInvoiceSensitiveDetail(
  detail: InvoiceApplicationDetail,
  canReadSensitive: boolean
): InvoiceApplicationDetail {
  if (canReadSensitive) return detail
  return {
    ...detail,
    profile_snapshot: {
      ...detail.profile_snapshot,
      title: PROTECTED_INVOICE_VALUE_KEY,
      tax_number: PROTECTED_INVOICE_VALUE_KEY,
    },
  }
}

/** Returns only lifecycle transitions accepted by the backend review contract. */
export function getInvoiceReviewActions(
  status: InvoiceApplicationStatus
): Array<'reviewing' | 'approve' | 'reject'> {
  if (status === 'submitted') return ['reviewing', 'reject']
  if (status === 'reviewing') return ['approve', 'reject']
  return []
}

/** Runs the dependent reviewing-to-approved-to-issued browser workflow. */
export async function approveAndIssueInvoice(
  api: AdminInvoiceApi,
  detail: InvoiceApplicationDetail,
  document: FormData,
  onUploadFailureApproved: (
    approved: InvoiceApplicationDetail
  ) => Promise<void> | void
): Promise<InvoiceApplicationDetail> {
  if (detail.status !== 'reviewing') {
    throw new Error('approve-and-issue requires a reviewing application')
  }
  const approved = await api.reviewApplication(detail.id, {
    action: 'approve',
    expected_status: 'reviewing',
  })
  try {
    return await api.uploadDocument(detail.id, document)
  } catch (error) {
    await onUploadFailureApproved(approved)
    throw error
  }
}

/** Validates and creates the exact invoice PDF multipart request. */
export function buildInvoiceDocumentFormData(
  detail: InvoiceApplicationDetail,
  input: InvoiceDocumentUploadInput
): InvoiceValidationResult<FormData> {
  const file = input.file
  if (!file || file.size <= 0) {
    return { ok: false, errorKey: 'Select a non-empty invoice PDF' }
  }
  if (
    file.size > MAX_INVOICE_PDF_BYTES ||
    file.type !== 'application/pdf' ||
    !file.name.toLocaleLowerCase().endsWith('.pdf')
  ) {
    return { ok: false, errorKey: 'Invoice PDF must be at most 10 MiB' }
  }
  if (!input.pdf_facts_attested) {
    return { ok: false, errorKey: 'Confirm that the PDF facts are correct' }
  }

  let expectedStatus: 'approved' | 'issued'
  let invoiceNumber = input.invoice_number.trim()
  let invoiceCode = input.invoice_code.trim()
  let invoiceDate = input.invoice_date
  let faceAmountMinor = input.face_amount_minor
  let currency = input.currency

  if (
    (detail.status === 'reviewing' || detail.status === 'approved') &&
    detail.document === null
  ) {
    expectedStatus = 'approved'
  } else if (
    detail.status === 'issued' &&
    detail.issuance !== null &&
    detail.document !== null
  ) {
    expectedStatus = 'issued'
    invoiceNumber = detail.issuance.invoice_number
    invoiceCode = detail.issuance.invoice_code
    invoiceDate = detail.issuance.invoice_date
    faceAmountMinor = detail.issuance.face_amount_minor
    currency = detail.issuance.currency
  } else {
    return { ok: false, errorKey: 'Invoice document state changed' }
  }

  if (
    invoiceNumber.length === 0 ||
    !Number.isSafeInteger(invoiceDate) ||
    invoiceDate <= 0 ||
    !Number.isSafeInteger(faceAmountMinor) ||
    faceAmountMinor <= 0 ||
    currency !== 'CNY'
  ) {
    return { ok: false, errorKey: 'Enter valid invoice issuance facts' }
  }

  const formData = new FormData()
  formData.set('file', file)
  formData.set('expected_status', expectedStatus)
  formData.set('invoice_number', invoiceNumber)
  formData.set('invoice_code', invoiceCode)
  formData.set('invoice_date', String(invoiceDate))
  formData.set('face_amount_minor', String(faceAmountMinor))
  formData.set('currency', currency)
  formData.set('pdf_facts_attested', 'true')
  return { ok: true, data: formData }
}

/** Validates invoice policy and the complete database-backed R2 configuration. */
export function validateInvoiceSetting(
  setting: UpdateInvoiceSettingRequest,
  r2SecretConfigured: boolean
): InvoiceValidationResult<UpdateInvoiceSettingRequest> {
  if (
    !Number.isSafeInteger(setting.application_window_days) ||
    setting.application_window_days <= 0
  ) {
    return { ok: false, errorKey: 'Application window must be positive' }
  }
  if (
    !Number.isSafeInteger(setting.minimum_amount_minor) ||
    setting.minimum_amount_minor < 0
  ) {
    return { ok: false, errorKey: 'Minimum invoice amount cannot be negative' }
  }
  if (
    !Number.isSafeInteger(setting.fee_percent) ||
    setting.fee_percent < 0 ||
    setting.fee_percent > 100
  ) {
    return { ok: false, errorKey: 'Invoice fee percentage is out of range' }
  }
  if (
    !Number.isSafeInteger(setting.pdf_retention_days) ||
    setting.pdf_retention_days <= 0
  ) {
    return { ok: false, errorKey: 'PDF retention must be positive' }
  }
  const r2Values = [
    setting.r2_endpoint,
    setting.r2_bucket,
    setting.r2_access_key_id,
  ].map((value) => value.trim())
  const publicR2Empty = r2Values.every((value) => value === '')
  const publicR2Complete = r2Values.every((value) => value !== '')
  const secretAvailable =
    r2SecretConfigured || setting.r2_secret_access_key.trim() !== ''
  if (
    (!publicR2Empty && !publicR2Complete) ||
    (publicR2Complete && !secretAvailable) ||
    (publicR2Empty && setting.r2_secret_access_key.trim() !== '')
  ) {
    return { ok: false, errorKey: 'Enter a complete R2 configuration' }
  }
  return { ok: true, data: setting }
}
