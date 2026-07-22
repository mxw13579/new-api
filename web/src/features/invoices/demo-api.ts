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
  InvoiceApi,
  InvoiceApplicationDetail,
  InvoiceApplicationSummary,
  InvoiceProfile,
} from './types'

const now = 1_900_000_000

function application(
  id: number,
  overrides: Partial<InvoiceApplicationSummary>
): InvoiceApplicationSummary {
  return {
    id,
    application_no: `INV-DEMO-${id}`,
    type: 'personal',
    status: 'submitted',
    payment_review_status: 'none',
    currency: 'CNY',
    amount_minor: 12_800,
    fee_quota: 500,
    fee_status: 'paid',
    submitted_at: now - id * 3600,
    reviewed_at: null,
    cancelled_at: null,
    issued_at: null,
    reject_reason: '',
    document_status: 'uploading',
    document_expires_at: null,
    document_deleted_at: null,
    can_cancel: true,
    can_download: false,
    ...overrides,
  }
}

function detailFromSummary(
  summary: InvoiceApplicationSummary
): InvoiceApplicationDetail {
  return {
    ...summary,
    profile_snapshot: {
      type: summary.type,
      title: summary.type === 'personal' ? 'Demo User' : 'Demo Company',
      tax_number: summary.type === 'company' ? '91310000DEMO' : '',
      version: 1,
    },
    policy_snapshot: {
      application_window_days: 90,
      minimum_amount_minor: 1000,
      fee_quota: summary.fee_quota,
      pdf_retention_days: 365,
    },
    items: [],
    issuance: null,
    document: null,
  }
}

export function createInvoiceDemoApi(): InvoiceApi {
  let profiles: InvoiceProfile[] = [
    {
      id: 1,
      type: 'personal',
      title: 'Demo User',
      tax_number: '',
      is_default: true,
      version: 3,
      created_at: now - 80_000,
      updated_at: now - 200,
    },
    {
      id: 2,
      type: 'company',
      title: 'Demo Company',
      tax_number: '91310000DEMO',
      is_default: true,
      version: 2,
      created_at: now - 70_000,
      updated_at: now - 100,
    },
  ]
  const orders = [
    {
      topup_id: 101,
      order_no: 'TOPUP-101',
      paid_amount_minor: 6800,
      currency: 'CNY' as const,
      product_description: 'API credit top-up',
      paid_at: now - 7200,
    },
    {
      topup_id: 102,
      order_no: 'TOPUP-102',
      paid_amount_minor: 6000,
      currency: 'CNY' as const,
      product_description: 'API credit top-up',
      paid_at: now - 3600,
    },
  ]
  let applications = [
    application(1, {}),
    application(2, {
      status: 'reviewing',
      payment_review_status: 'pre_issue_hold',
      document_status: 'validating',
    }),
    application(3, {
      status: 'issued',
      payment_review_status: 'none',
      document_status: 'available',
      document_expires_at: now + 86400,
      can_cancel: false,
      can_download: true,
    }),
    application(4, {
      status: 'issued',
      payment_review_status: 'post_issue_hold',
      document_status: 'available',
      document_expires_at: now + 86400,
      can_cancel: false,
      can_download: false,
    }),
  ]

  return {
    async getConfig() {
      return {
        personal_enabled: true,
        company_enabled: true,
        application_window_days: 90,
        minimum_amount_minor: 1000,
        fee_quota: 500,
        pdf_retention_days: 365,
        currency: 'CNY',
      }
    },
    async listProfiles() {
      return profiles
    },
    async createProfile(request) {
      const created: InvoiceProfile = {
        id: Math.max(0, ...profiles.map((profile) => profile.id)) + 1,
        ...request,
        version: 1,
        created_at: now,
        updated_at: now,
      }
      profiles = [...profiles, created]
      return created
    },
    async updateProfile(request) {
      const current = profiles.find((profile) => profile.id === request.id)
      if (!current || current.version !== request.expected_version) {
        throw new Error('INVOICE_STATE_CONFLICT')
      }
      const updated: InvoiceProfile = {
        ...current,
        title: request.title,
        tax_number: request.tax_number,
        is_default: request.is_default,
        version: current.version + 1,
        updated_at: now,
      }
      profiles = profiles.map((profile) =>
        profile.id === updated.id ? updated : profile
      )
      return updated
    },
    async deleteProfile(request) {
      profiles = profiles.filter(
        (profile) =>
          profile.id !== request.id ||
          profile.version !== request.expected_version
      )
    },
    async listEligibleOrders(request) {
      return {
        items: orders,
        page: request.page,
        page_size: request.page_size,
        total: orders.length,
      }
    },
    async createApplication(request: CreateInvoiceApplicationRequest) {
      const created = application(applications.length + 1, {
        amount_minor: orders
          .filter((order) => request.topup_ids.includes(order.topup_id))
          .reduce((total, order) => total + order.paid_amount_minor, 0),
      })
      applications = [created, ...applications]
      return detailFromSummary(created)
    },
    async listApplications(request) {
      return {
        items: applications,
        page: request.page,
        page_size: request.page_size,
        total: applications.length,
      }
    },
    async getApplication(applicationId) {
      const found = applications.find((item) => item.id === applicationId)
      if (!found) throw new Error('INVOICE_NOT_FOUND')
      return detailFromSummary(found)
    },
    async cancelApplication(applicationId) {
      const found = applications.find((item) => item.id === applicationId)
      if (!found) throw new Error('INVOICE_NOT_FOUND')
      const cancelled = {
        ...found,
        status: 'cancelled' as const,
        cancelled_at: now,
        can_cancel: false,
      }
      applications = applications.map((item) =>
        item.id === applicationId ? cancelled : item
      )
      return detailFromSummary(cancelled)
    },
    getDocumentDownloadUrl(applicationId) {
      return `/demo/invoices/${applicationId}.pdf`
    },
  }
}
