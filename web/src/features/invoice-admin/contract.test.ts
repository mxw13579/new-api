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
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { describe, test } from 'node:test'

import type { AuthUser } from '@/stores/auth-store'

import type {
  AdminInvoiceApi,
  InvoiceApplicationDetail,
  UpdateInvoiceSettingRequest,
} from '../invoices/types'
import {
  INVOICE_ADMIN_PERMISSIONS,
  MAX_INVOICE_PDF_BYTES,
  PROTECTED_INVOICE_VALUE_KEY,
  approveAndIssueInvoice,
  buildInvoiceDocumentFormData,
  getInvoiceReviewActions,
  getInvoiceAdminCapabilities,
  getInvoiceOwnerLabel,
  maskInvoiceSensitiveDetail,
  validateInvoiceSetting,
} from './contract'
import { adminInvoiceQueryKeys, invoiceSettingsQueryKeys } from './queries'

const baseDetail: InvoiceApplicationDetail = {
  id: 7,
  application_no: 'INV-7',
  type: 'company',
  status: 'approved',
  payment_review_status: 'none',
  currency: 'CNY',
  amount_minor: 1234,
  fee_quota: 0,
  fee_status: 'not_required',
  submitted_at: 1,
  reviewed_at: 2,
  cancelled_at: null,
  issued_at: null,
  reject_reason: '',
  document_status: 'missing',
  document_expires_at: null,
  document_deleted_at: null,
  can_cancel: false,
  can_download: false,
  profile_snapshot: {
    type: 'company',
    title: 'Secret Company',
    tax_number: '91310000SECRET',
    version: 3,
  },
  policy_snapshot: {
    application_window_days: 30,
    minimum_amount_minor: 100,
    fee_percent: 0,
    fee_quota: 0,
    pdf_retention_days: 90,
  },
  items: [],
  issuance: null,
  document: null,
}

function issuanceApi(
  review: AdminInvoiceApi['reviewApplication'],
  upload: AdminInvoiceApi['uploadDocument']
): AdminInvoiceApi {
  return {
    async listApplications() {
      throw new Error('unused')
    },
    async getApplication() {
      throw new Error('unused')
    },
    reviewApplication: review,
    async rejectApplication() {
      throw new Error('unused')
    },
    uploadDocument: upload,
  }
}

function adminWithPermissions(actions: Record<string, boolean>): AuthUser {
  return {
    id: 1,
    username: 'admin',
    role: 10,
    permissions: { admin_permissions: { invoice: actions } },
  }
}

describe('invoice admin contracts', () => {
  test('uses display name then username while always preserving immutable user ID', () => {
    assert.deepEqual(
      getInvoiceOwnerLabel({
        user_id: 42,
        username: 'alice',
        display_name: 'Alice A',
      }),
      { name: 'Alice A', userId: 42 }
    )
    assert.deepEqual(
      getInvoiceOwnerLabel({
        user_id: 42,
        username: 'alice',
        display_name: '',
      }),
      { name: 'alice', userId: 42 }
    )
  })
  test('enforces review, upload, sensitive-read, and settings independently', () => {
    assert.deepEqual(INVOICE_ADMIN_PERMISSIONS, {
      resource: 'invoice',
      review: 'review',
      documentUpload: 'document.upload',
      sensitiveRead: 'sensitive.read',
      settings: 'settings',
    })
    assert.deepEqual(
      getInvoiceAdminCapabilities(
        adminWithPermissions({
          review: true,
          'document.upload': false,
          'sensitive.read': true,
          settings: false,
        })
      ),
      {
        canReview: true,
        canUploadDocument: false,
        canReadSensitive: true,
        canManageSettings: false,
      }
    )
  })

  test('exposes only backend-valid review transitions for each expected status', () => {
    assert.deepEqual(getInvoiceReviewActions('submitted'), [
      'reviewing',
      'reject',
    ])
    assert.deepEqual(getInvoiceReviewActions('reviewing'), [
      'approve',
      'reject',
    ])
    assert.deepEqual(getInvoiceReviewActions('approved'), [])
    assert.deepEqual(getInvoiceReviewActions('issued'), [])
  })

  test('removes sensitive values instead of merely hiding them with CSS', () => {
    const masked = maskInvoiceSensitiveDetail(baseDetail, false)

    assert.equal(masked.profile_snapshot.title, PROTECTED_INVOICE_VALUE_KEY)
    assert.equal(
      masked.profile_snapshot.tax_number,
      PROTECTED_INVOICE_VALUE_KEY
    )
    assert.doesNotMatch(JSON.stringify(masked), /Secret Company|91310000SECRET/)
    assert.equal(
      maskInvoiceSensitiveDetail(baseDetail, true).profile_snapshot.tax_number,
      '91310000SECRET'
    )
  })

  test('builds the exact initial PDF multipart contract after strict validation', () => {
    const form = buildInvoiceDocumentFormData(baseDetail, {
      file: new File(['%PDF-1.7'], 'invoice.pdf', { type: 'application/pdf' }),
      invoice_number: 'N-7',
      invoice_code: 'C-7',
      invoice_date: 1_752_000_000,
      face_amount_minor: 1234,
      currency: 'CNY',
      pdf_facts_attested: true,
    })

    assert.equal(form.ok, true)
    if (!form.ok) return
    assert.deepEqual([...form.data.keys()].sort(), [
      'currency',
      'expected_status',
      'face_amount_minor',
      'file',
      'invoice_code',
      'invoice_date',
      'invoice_number',
      'pdf_facts_attested',
    ])
    assert.equal(form.data.get('expected_status'), 'approved')
    assert.equal(form.data.get('face_amount_minor'), '1234')
  })

  test('builds reviewing issuance facts for the approved upload transition', () => {
    const form = buildInvoiceDocumentFormData(
      { ...baseDetail, status: 'reviewing' },
      {
        file: new File(['%PDF-1.7'], 'invoice.pdf', {
          type: 'application/pdf',
        }),
        invoice_number: 'N-REVIEWING',
        invoice_code: 'C-REVIEWING',
        invoice_date: 1_752_000_000,
        face_amount_minor: 1234,
        currency: 'CNY',
        pdf_facts_attested: true,
      }
    )

    assert.equal(form.ok, true)
    if (!form.ok) return
    assert.equal(form.data.get('expected_status'), 'approved')
  })

  test('approves then uploads in strict order', async () => {
    const calls: string[] = []
    const reviewing = { ...baseDetail, status: 'reviewing' as const }
    const approved = { ...baseDetail, status: 'approved' as const }
    const issued = { ...baseDetail, status: 'issued' as const }
    const document = new FormData()
    document.set('expected_status', 'approved')
    const api = issuanceApi(
      async (_id, request) => {
        calls.push(`review:${request.action}:${request.expected_status}`)
        return approved
      },
      async (_id, document) => {
        calls.push(`upload:${document.get('expected_status')}`)
        return issued
      }
    )

    const result = await approveAndIssueInvoice(api, reviewing, document, () =>
      assert.fail('successful upload does not need retry convergence')
    )

    assert.equal(result, issued)
    assert.deepEqual(calls, ['review:approve:reviewing', 'upload:approved'])
  })

  test('never uploads after approval failure', async () => {
    let uploadCalls = 0
    const api = issuanceApi(
      async () => {
        throw new Error('approval failed')
      },
      async () => {
        uploadCalls += 1
        return baseDetail
      }
    )

    await assert.rejects(
      approveAndIssueInvoice(
        api,
        { ...baseDetail, status: 'reviewing' },
        new FormData(),
        () => assert.fail('approval state must not publish')
      ),
      /approval failed/
    )
    assert.equal(uploadCalls, 0)
  })

  test('keeps the returned approved detail when upload fails', async () => {
    const approved = { ...baseDetail, status: 'approved' as const }
    let converged: InvoiceApplicationDetail | null = null
    let releaseConvergence: (() => void) | undefined
    let rejected = false
    const api = issuanceApi(
      async () => approved,
      async () => {
        throw new Error('upload failed')
      }
    )

    const result = approveAndIssueInvoice(
      api,
      { ...baseDetail, status: 'reviewing' },
      new FormData(),
      async (detail) => {
        converged = detail
        await new Promise<void>((resolve) => {
          releaseConvergence = resolve
        })
      }
    )
    void result.catch(() => {
      rejected = true
    })
    await new Promise<void>((resolve) => setTimeout(resolve, 0))

    assert.equal(rejected, false)
    assert.ok(releaseConvergence)
    releaseConvergence()
    await assert.rejects(result, /upload failed/)
    assert.deepEqual(converged, approved)
  })

  test('locks replacement issuance facts to the existing issued invoice', () => {
    const issued: InvoiceApplicationDetail = {
      ...baseDetail,
      status: 'issued',
      document_status: 'available',
      issued_at: 10,
      issuance: {
        id: 2,
        invoice_number: 'LOCKED-NUMBER',
        invoice_code: 'LOCKED-CODE',
        invoice_date: 1_700_000_000,
        face_amount_minor: 1234,
        currency: 'CNY',
      },
      document: {
        id: 3,
        status: 'available',
        expires_at: 99,
        deleted_at: null,
      },
    }
    const form = buildInvoiceDocumentFormData(issued, {
      file: new File(['%PDF-1.7'], 'replacement.pdf', {
        type: 'application/pdf',
      }),
      invoice_number: 'ATTACKER-CHANGE',
      invoice_code: 'ATTACKER-CHANGE',
      invoice_date: 99,
      face_amount_minor: 99,
      currency: 'CNY',
      pdf_facts_attested: true,
    })

    assert.equal(form.ok, true)
    if (!form.ok) return
    assert.equal(form.data.get('expected_status'), 'issued')
    assert.equal(form.data.get('invoice_number'), 'LOCKED-NUMBER')
    assert.equal(form.data.get('invoice_code'), 'LOCKED-CODE')
    assert.equal(form.data.get('invoice_date'), '1700000000')
    assert.equal(form.data.get('face_amount_minor'), '1234')
  })

  test('rejects unsafe PDF and attestation inputs before upload', () => {
    const inputs = {
      invoice_number: 'N-7',
      invoice_code: '',
      invoice_date: 1,
      face_amount_minor: 1234,
      currency: 'CNY' as const,
      pdf_facts_attested: true,
    }
    const invalidFiles = [
      new File([], 'empty.pdf', { type: 'application/pdf' }),
      new File(['text'], 'invoice.txt', { type: 'text/plain' }),
      new File([new Uint8Array(MAX_INVOICE_PDF_BYTES + 1)], 'large.pdf', {
        type: 'application/pdf',
      }),
    ]

    for (const file of invalidFiles) {
      assert.equal(
        buildInvoiceDocumentFormData(baseDetail, { ...inputs, file }).ok,
        false
      )
    }
    assert.equal(
      buildInvoiceDocumentFormData(baseDetail, {
        ...inputs,
        file: new File(['%PDF'], 'invoice.pdf', { type: 'application/pdf' }),
        pdf_facts_attested: false,
      }).ok,
      false
    )
  })

  test('validates percentage fees and complete database-backed R2 settings', () => {
    const valid: UpdateInvoiceSettingRequest = {
      personal_enabled: true,
      company_enabled: false,
      application_window_days: 30,
      minimum_amount_minor: 0,
      fee_percent: 5,
      pdf_retention_days: 90,
      r2_endpoint:
        'https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com',
      r2_bucket: 'private-invoices',
      r2_access_key_id: 'access-id',
      r2_secret_access_key: '',
    }
    assert.deepEqual(validateInvoiceSetting(valid, true), {
      ok: true,
      data: valid,
    })

    for (const invalid of [
      { ...valid, application_window_days: 0 },
      { ...valid, minimum_amount_minor: -1 },
      { ...valid, fee_percent: 101 },
      { ...valid, pdf_retention_days: 0 },
      { ...valid, r2_bucket: '' },
    ]) {
      assert.equal(validateInvoiceSetting(invalid, true).ok, false)
    }
  })

  test('keeps admin lists, detail, and settings in independent query namespaces', () => {
    assert.deepEqual(adminInvoiceQueryKeys.list(2, 20), [
      'invoices',
      'admin',
      'applications',
      2,
      20,
    ])
    assert.deepEqual(adminInvoiceQueryKeys.detail(7), [
      'invoices',
      'admin',
      'application',
      7,
    ])
    assert.deepEqual(invoiceSettingsQueryKeys.detail(), [
      'invoices',
      'settings',
    ])
  })

  test('hand-authored routes expose literal IDs to the TanStack generator', async () => {
    const routes = [
      {
        file: '../../routes/_authenticated/admin-invoices/index.tsx',
        id: '/_authenticated/admin-invoices/',
      },
      {
        file: '../../routes/_authenticated/invoice-settings/index.tsx',
        id: '/_authenticated/invoice-settings/',
      },
    ]

    for (const route of routes) {
      const source = await readFile(
        new URL(route.file, import.meta.url),
        'utf8'
      )
      assert.match(source, new RegExp(`createFileRoute\\('${route.id}'\\)`))
      assert.doesNotMatch(source, /createFileRoute\([^)]*as never/)
    }
  })
})
