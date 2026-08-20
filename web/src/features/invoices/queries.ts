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
import type { InvoiceApplicationDetail, InvoiceProfile } from './types'

/** Provides hierarchical query keys for invoice-domain cache entries. */
export const invoiceQueryKeys = {
  all: ['invoices'] as const,
  config: () => [...invoiceQueryKeys.all, 'config'] as const,
  profiles: () => [...invoiceQueryKeys.all, 'profiles'] as const,
  eligibleOrdersList: () =>
    [...invoiceQueryKeys.all, 'eligible-orders'] as const,
  eligibleOrders: (page: number, pageSize: number) =>
    [...invoiceQueryKeys.eligibleOrdersList(), page, pageSize] as const,
  applicationsList: () => [...invoiceQueryKeys.all, 'applications'] as const,
  applications: (page: number, pageSize: number) =>
    [...invoiceQueryKeys.applicationsList(), page, pageSize] as const,
  application: (applicationId: number) =>
    [...invoiceQueryKeys.all, 'application', applicationId] as const,
}

/** Removes raw tax identifiers before profile data enters React Query state. */
export function redactInvoiceProfilesForCache(
  profiles: InvoiceProfile[]
): InvoiceProfile[] {
  return profiles.map((profile) => ({
    ...profile,
    title: '',
    tax_number: '',
    identity_card_number: '',
  }))
}

/** Removes raw tax identifiers before application details enter React Query state. */
export function redactInvoiceApplicationDetailForCache(
  detail: InvoiceApplicationDetail
): InvoiceApplicationDetail {
  return {
    ...detail,
    profile_snapshot: {
      ...detail.profile_snapshot,
      title: '',
      tax_number: '',
      identity_card_number: '',
    },
  }
}
