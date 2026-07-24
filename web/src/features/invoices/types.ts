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
/** Identifies the supported invoice profile categories. */
export type InvoiceType = 'personal' | 'company'

/** Enumerates the lifecycle states of an invoice application. */
export type InvoiceApplicationStatus =
  | 'submitted'
  | 'reviewing'
  | 'approved'
  | 'rejected'
  | 'cancelled'
  | 'issued'
/** Describes the independent payment-evidence review state. */
export type InvoicePaymentReviewStatus =
  | 'none'
  | 'pre_issue_hold'
  | 'post_issue_hold'
  | 'resolved_valid'
  | 'resolved_voided'
/** Describes invoice fee collection and refund progress. */
export type InvoiceFeeStatus =
  | 'not_required'
  | 'paid'
  | 'refund_pending'
  | 'refunded'
/** Enumerates the persisted PDF document lifecycle states. */
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

/** Lists stable invoice API error codes used by UI error handling. */
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

/** Defines the invoice policy exposed to the current user. */
export interface InvoiceConfig {
  personal_enabled: boolean
  company_enabled: boolean
  application_window_days: number
  minimum_amount_minor: number
  fee_quota: number
  pdf_retention_days: number
  currency: 'CNY'
}

/** Represents a versioned personal or company invoice identity. */
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

/** Defines the fields accepted when creating an invoice profile. */
export interface CreateInvoiceProfileRequest {
  type: InvoiceType
  title: string
  tax_number: string
  is_default: boolean
}

/** Defines an optimistic-concurrency update for an invoice profile. */
export interface UpdateInvoiceProfileRequest {
  id: number
  expected_version: number
  title: string
  tax_number: string
  is_default: boolean
}

/** Identifies the profile version to delete. */
export interface DeleteInvoiceProfileRequest {
  id: number
  expected_version: number
}

/** Represents a complete paid order eligible for invoice selection. */
export interface EligibleInvoiceOrder {
  topup_id: number
  order_no: string
  paid_amount_minor: number
  currency: 'CNY'
  product_description: string
  paid_at: number
}

/** Wraps a page of invoice-domain records. */
export interface InvoicePage<T> {
  items: T[]
  page: number
  page_size: number
  total: number
}

/** Defines one invoice list pagination request. */
export interface InvoicePageRequest {
  page: number
  page_size: number
}

/** Defines an idempotent invoice application submission. */
export interface CreateInvoiceApplicationRequest {
  request_id: string
  profile_id: number
  profile_version: number
  topup_ids: number[]
}

/** Summarizes application, fee, payment-review, and document state. */
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

/** Captures the immutable profile facts used by an application. */
export interface InvoiceProfileSnapshot {
  type: InvoiceType
  title: string
  tax_number: string
  version: number
}

/** Captures the invoice policy applied at submission time. */
export interface InvoicePolicySnapshot {
  application_window_days: number
  minimum_amount_minor: number
  fee_quota: number
  pdf_retention_days: number
}

/** Represents an immutable eligible order attached to an application. */
export interface InvoiceApplicationItem extends EligibleInvoiceOrder {}

/** Describes immutable issuance metadata for an issued invoice. */
export interface InvoiceIssuanceMetadata {
  id: number
  invoice_number: string
  invoice_code: string
  invoice_date: number
  face_amount_minor: number
  currency: 'CNY'
}

/** Describes the active invoice document lifecycle metadata. */
export interface InvoiceDocumentMetadata {
  id: number
  status: InvoiceDocumentStatus
  expires_at: number | null
  deleted_at: number | null
}

/** Extends an application summary with snapshots and document details. */
export interface InvoiceApplicationDetail extends InvoiceApplicationSummary {
  profile_snapshot: InvoiceProfileSnapshot
  policy_snapshot: InvoicePolicySnapshot
  items: InvoiceApplicationItem[]
  issuance: InvoiceIssuanceMetadata | null
  document: InvoiceDocumentMetadata | null
}

/** Defines the frontend boundary for personal invoice operations. */
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

/** Identifies the production invoice API boundary. */
export type AuthenticatedInvoiceApi = InvoiceApi

/** Defines an administrator review transition. */
export interface ReviewInvoiceApplicationRequest {
  action: 'reviewing' | 'approve'
  expected_status: 'submitted' | 'reviewing'
}

/** Defines an administrator rejection transition. */
export interface RejectInvoiceApplicationRequest {
  expected_status: 'submitted' | 'reviewing'
  reason: string
}

/** Defines the administrator invoice operations without user-cache overlap. */
export interface AdminInvoiceApi {
  listApplications(
    request: InvoicePageRequest
  ): Promise<InvoicePage<InvoiceApplicationSummary>>
  getApplication(applicationId: number): Promise<InvoiceApplicationDetail>
  reviewApplication(
    applicationId: number,
    request: ReviewInvoiceApplicationRequest
  ): Promise<InvoiceApplicationDetail>
  rejectApplication(
    applicationId: number,
    request: RejectInvoiceApplicationRequest
  ): Promise<InvoiceApplicationDetail>
  uploadDocument(
    applicationId: number,
    document: FormData
  ): Promise<InvoiceApplicationDetail>
}

/** Defines the complete editable invoice policy object. */
export interface InvoiceSetting {
  personal_enabled: boolean
  company_enabled: boolean
  application_window_days: number
  minimum_amount_minor: number
  fee_quota: number
  pdf_retention_days: number
}

/** Defines the independently permissioned invoice-settings API. */
export interface InvoiceSettingsApi {
  getSetting(): Promise<InvoiceSetting>
  updateSetting(setting: InvoiceSetting): Promise<InvoiceSetting>
}
