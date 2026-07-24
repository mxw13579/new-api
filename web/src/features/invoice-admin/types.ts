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
  AdminInvoiceApi,
  InvoiceApplicationDetail,
  InvoiceSetting,
  InvoiceSettingsApi,
} from '../invoices/types'

/** Re-exports shared invoice contracts through the administrator feature. */
export type {
  AdminInvoiceApi,
  InvoiceApplicationDetail,
  InvoiceSetting,
  InvoiceSettingsApi,
}

/** Describes the four independent administrator invoice capabilities. */
export interface InvoiceAdminCapabilities {
  canReview: boolean
  canUploadDocument: boolean
  canReadSensitive: boolean
  canManageSettings: boolean
}

/** Captures the client-side PDF facts validated before multipart upload. */
export interface InvoiceDocumentUploadInput {
  file: File | null
  invoice_number: string
  invoice_code: string
  invoice_date: number
  face_amount_minor: number
  currency: 'CNY'
  pdf_facts_attested: boolean
}

/** Represents a validated invoice value or its localized error key. */
export type InvoiceValidationResult<T> =
  | { ok: true; data: T }
  | { ok: false; errorKey: string }
