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
export type InvoiceType = 'personal' | 'company'
export type InvoiceApplicationStatus =
  | 'submitted'
  | 'reviewing'
  | 'approved'
  | 'rejected'
  | 'cancelled'
  | 'issued'
export type InvoicePaymentReviewStatus =
  | 'none'
  | 'pre_issue_hold'
  | 'post_issue_hold'
  | 'resolved_valid'
  | 'resolved_voided'
export type InvoiceFeeStatus =
  | 'not_required'
  | 'paid'
  | 'refund_pending'
  | 'refunded'
export type InvoiceDocumentStatus =
  | 'uploading'
  | 'validating'
  | 'available'
  | 'superseded'
  | 'upload_failed'
  | 'deleting'
  | 'deleted'
  | 'delete_failed'
  | 'missing'

export type InvoiceErrorCode =
  | 'INVOICE_INVALID_REQUEST'
  | 'INVOICE_FORBIDDEN'
  | 'INVOICE_QUOTA_INSUFFICIENT'
  | 'INVOICE_NOT_FOUND'
  | 'INVOICE_IDEMPOTENCY_CONFLICT'
  | 'INVOICE_STATE_CONFLICT'
  | 'INVOICE_TOPUP_INELIGIBLE'
  | 'INVOICE_PAYMENT_EVIDENCE_CONFLICT'
  | 'INVOICE_DOCUMENT_UNAVAILABLE'
  | 'INVOICE_INTERNAL_ERROR'

export interface InvoiceConfig {
  personal_enabled: boolean
  company_enabled: boolean
  application_window_days: number
  minimum_amount_minor: number
  fee_quota: number
  pdf_retention_days: number
  currency: 'CNY'
}

export interface InvoiceProfile {
  id: number
  type: InvoiceType
  title: string
  tax_number: string
  is_default: boolean
  version: number
  created_at: number
  updated_at: number
}

export interface CreateInvoiceProfileRequest {
  type: InvoiceType
  title: string
  tax_number: string
  is_default: boolean
}

export interface UpdateInvoiceProfileRequest {
  id: number
  expected_version: number
  title: string
  tax_number: string
  is_default: boolean
}

export interface DeleteInvoiceProfileRequest {
  id: number
  expected_version: number
}

export interface EligibleInvoiceOrder {
  topup_id: number
  order_no: string
  paid_amount_minor: number
  currency: 'CNY'
  product_description: string
  paid_at: number
}

export interface InvoicePage<T> {
  items: T[]
  page: number
  page_size: number
  total: number
}

export interface InvoicePageRequest {
  page: number
  page_size: number
}

export interface CreateInvoiceApplicationRequest {
  request_id: string
  profile_id: number
  profile_version: number
  topup_ids: number[]
}

export interface InvoiceApplicationSummary {
  id: number
  application_no: string
  type: InvoiceType
  status: InvoiceApplicationStatus
  payment_review_status: InvoicePaymentReviewStatus
  currency: 'CNY'
  amount_minor: number
  fee_quota: number
  fee_status: InvoiceFeeStatus
  submitted_at: number
  reviewed_at: number | null
  cancelled_at: number | null
  issued_at: number | null
  reject_reason: string
  document_status: InvoiceDocumentStatus
  document_expires_at: number | null
  document_deleted_at: number | null
  can_cancel: boolean
  can_download: boolean
}

export interface InvoiceProfileSnapshot {
  type: InvoiceType
  title: string
  tax_number: string
  version: number
}

export interface InvoicePolicySnapshot {
  application_window_days: number
  minimum_amount_minor: number
  fee_quota: number
  pdf_retention_days: number
}

export interface InvoiceApplicationItem extends EligibleInvoiceOrder {}

export interface InvoiceIssuanceMetadata {
  id: number
  invoice_number: string
  invoice_code: string
  invoice_date: number
  face_amount_minor: number
  currency: 'CNY'
}

export interface InvoiceDocumentMetadata {
  id: number
  status: InvoiceDocumentStatus
  expires_at: number | null
  deleted_at: number | null
}

export interface InvoiceApplicationDetail extends InvoiceApplicationSummary {
  profile_snapshot: InvoiceProfileSnapshot
  policy_snapshot: InvoicePolicySnapshot
  items: InvoiceApplicationItem[]
  issuance: InvoiceIssuanceMetadata | null
  document: InvoiceDocumentMetadata | null
}

export interface InvoiceApi {
  getConfig(): Promise<InvoiceConfig>
  listProfiles(): Promise<InvoiceProfile[]>
  createProfile(request: CreateInvoiceProfileRequest): Promise<InvoiceProfile>
  updateProfile(request: UpdateInvoiceProfileRequest): Promise<InvoiceProfile>
  deleteProfile(request: DeleteInvoiceProfileRequest): Promise<void>
  listEligibleOrders(
    request: InvoicePageRequest
  ): Promise<InvoicePage<EligibleInvoiceOrder>>
  createApplication(
    request: CreateInvoiceApplicationRequest
  ): Promise<InvoiceApplicationDetail>
  listApplications(
    request: InvoicePageRequest
  ): Promise<InvoicePage<InvoiceApplicationSummary>>
  getApplication(applicationId: number): Promise<InvoiceApplicationDetail>
  cancelApplication(applicationId: number): Promise<InvoiceApplicationDetail>
  getDocumentDownloadUrl(applicationId: number): string
}
