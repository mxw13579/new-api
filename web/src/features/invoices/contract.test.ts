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
import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { describe, test } from 'node:test'
import { pathToFileURL } from 'node:url'

import {
  APPLICATION_STATUS_CONFIG,
  DOCUMENT_STATUS_CONFIG,
  FEE_STATUS_CONFIG,
  PAYMENT_REVIEW_STATUS_CONFIG,
  canDownloadInvoice,
  formatInvoiceAmount,
  getInvoiceErrorMessageKey,
} from './contract'
import { createInvoiceDemoApi } from './demo-api'
import { invoiceQueryKeys } from './queries'
import type {
  InvoiceApi,
  InvoiceApplicationSummary,
  InvoiceErrorCode,
} from './types'

describe('invoice frontend contract', () => {
  test('all four independent status unions have stable labelKey mappings', () => {
    assert.deepEqual(Object.keys(APPLICATION_STATUS_CONFIG).sort(), [
      'approved',
      'cancelled',
      'issued',
      'rejected',
      'reviewing',
      'submitted',
    ])
    assert.deepEqual(Object.keys(FEE_STATUS_CONFIG).sort(), [
      'not_required',
      'paid',
      'refund_pending',
      'refunded',
    ])
    assert.deepEqual(Object.keys(PAYMENT_REVIEW_STATUS_CONFIG).sort(), [
      'none',
      'post_issue_hold',
      'pre_issue_hold',
      'resolved_valid',
      'resolved_voided',
    ])
    assert.deepEqual(Object.keys(DOCUMENT_STATUS_CONFIG).sort(), [
      'available',
      'delete_failed',
      'deleted',
      'deleting',
      'missing',
      'superseded',
      'upload_failed',
      'uploading',
      'validating',
    ])

    for (const config of [
      ...Object.values(APPLICATION_STATUS_CONFIG),
      ...Object.values(FEE_STATUS_CONFIG),
      ...Object.values(PAYMENT_REVIEW_STATUS_CONFIG),
      ...Object.values(DOCUMENT_STATUS_CONFIG),
    ]) {
      assert.match(config.labelKey, /^Invoice /)
      assert.deepEqual(Object.keys(config), ['labelKey', 'variant'])
    }
  })

  test('maps every stable server code without using free-form messages', () => {
    const codes: InvoiceErrorCode[] = [
      'INVOICE_INVALID_REQUEST',
      'INVOICE_FORBIDDEN',
      'INVOICE_QUOTA_INSUFFICIENT',
      'INVOICE_NOT_FOUND',
      'INVOICE_IDEMPOTENCY_CONFLICT',
      'INVOICE_STATE_CONFLICT',
      'INVOICE_TOPUP_INELIGIBLE',
      'INVOICE_PAYMENT_EVIDENCE_CONFLICT',
      'INVOICE_DOCUMENT_UNAVAILABLE',
      'INVOICE_INTERNAL_ERROR',
    ]

    for (const code of codes) {
      assert.match(getInvoiceErrorMessageKey(code), /^Invoice error:/)
    }
    assert.equal(
      getInvoiceErrorMessageKey('INVOICE_STATE_CONFLICT'),
      'Invoice error: data changed, refresh and try again'
    )
  })

  test('formats invoice minor units with the shared CNY presentation', () => {
    assert.equal(
      formatInvoiceAmount(12_345),
      new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency: 'CNY',
      }).format(123.45)
    )
  })

  test('suppresses download for payment holds, unavailable documents, and expiry', () => {
    const base: InvoiceApplicationSummary = {
      id: 1,
      application_no: 'INV-1',
      type: 'personal',
      status: 'issued',
      payment_review_status: 'none',
      currency: 'CNY',
      amount_minor: 1200,
      fee_quota: 0,
      fee_status: 'not_required',
      submitted_at: 1,
      reviewed_at: 2,
      cancelled_at: null,
      issued_at: 3,
      reject_reason: '',
      document_status: 'available',
      document_expires_at: 2_000,
      document_deleted_at: null,
      can_cancel: false,
      can_download: true,
    }

    assert.equal(canDownloadInvoice(base, 1_000), true)
    assert.equal(
      canDownloadInvoice(
        { ...base, payment_review_status: 'pre_issue_hold' },
        1_000
      ),
      false
    )
    assert.equal(
      canDownloadInvoice(
        { ...base, payment_review_status: 'post_issue_hold' },
        1_000
      ),
      false
    )
    assert.equal(
      canDownloadInvoice(
        { ...base, payment_review_status: 'resolved_voided' },
        1_000
      ),
      false
    )
    assert.equal(
      canDownloadInvoice({ ...base, document_status: 'delete_failed' }, 1_000),
      false
    )
    assert.equal(canDownloadInvoice(base, 2_000), false)
  })

  test('typed demo fixture satisfies the same InvoiceApi boundary', async () => {
    const invoiceApi: InvoiceApi = createInvoiceDemoApi()
    const [config, profiles, orders, applications] = await Promise.all([
      invoiceApi.getConfig(),
      invoiceApi.listProfiles(),
      invoiceApi.listEligibleOrders({ page: 1, page_size: 20 }),
      invoiceApi.listApplications({ page: 1, page_size: 20 }),
    ])

    assert.equal(config.currency, 'CNY')
    assert.ok(profiles.length >= 2)
    assert.ok(orders.items.length >= 2)
    assert.ok(applications.items.length >= 4)
  })

  test('all seven locales cover status labelKey and stable error render matrices', async () => {
    const statusKeys = [
      ...Object.values(APPLICATION_STATUS_CONFIG),
      ...Object.values(FEE_STATUS_CONFIG),
      ...Object.values(PAYMENT_REVIEW_STATUS_CONFIG),
      ...Object.values(DOCUMENT_STATUS_CONFIG),
    ].map((config) => config.labelKey)
    const errorKeys = [
      'INVOICE_INVALID_REQUEST',
      'INVOICE_FORBIDDEN',
      'INVOICE_QUOTA_INSUFFICIENT',
      'INVOICE_NOT_FOUND',
      'INVOICE_IDEMPOTENCY_CONFLICT',
      'INVOICE_STATE_CONFLICT',
      'INVOICE_TOPUP_INELIGIBLE',
      'INVOICE_PAYMENT_EVIDENCE_CONFLICT',
      'INVOICE_DOCUMENT_UNAVAILABLE',
      'INVOICE_INTERNAL_ERROR',
    ].map((code) => getInvoiceErrorMessageKey(code as InvoiceErrorCode))

    for (const locale of ['en', 'zh', 'zh-TW', 'fr', 'ja', 'ru', 'vi']) {
      const raw = await readFile(
        new URL(`../../i18n/locales/${locale}.json`, import.meta.url),
        'utf8'
      )
      const translation = JSON.parse(raw).translation as Record<string, string>
      for (const key of [...statusKeys, ...errorKeys]) {
        assert.ok(translation[key], `${locale} missing ${key}`)
        if (locale !== 'en') assert.notEqual(translation[key], key)
      }
    }
  })

  test('all seven locales cover every user and admin invoice literal', async () => {
    const sourceDirectories = [
      new URL('./', import.meta.url),
      new URL('../invoice-admin/', import.meta.url),
    ]
    const sourceFiles: URL[] = [
      new URL(
        '../../routes/_authenticated/invoices/index.tsx',
        import.meta.url
      ),
      new URL(
        '../../routes/_authenticated/admin-invoices/index.tsx',
        import.meta.url
      ),
      new URL(
        '../../routes/_authenticated/invoice-settings/index.tsx',
        import.meta.url
      ),
      new URL('../../hooks/use-sidebar-data.ts', import.meta.url),
    ]
    for (const sourceDirectory of sourceDirectories) {
      for (const entry of await readdir(sourceDirectory, {
        recursive: true,
        withFileTypes: true,
      })) {
        if (!entry.isFile() || !/\.tsx?$/.test(entry.name)) continue
        sourceFiles.push(pathToFileURL(path.join(entry.parentPath, entry.name)))
      }
    }
    const pageKeys = new Set<string>()
    for (const dynamicKey of [
      'Protected invoice value',
      'Select a non-empty invoice PDF',
      'Invoice PDF must be at most 10 MiB',
      'Confirm that the PDF facts are correct',
      'Invoice document state changed',
      'Enter valid invoice issuance facts',
      'Application window must be positive',
      'Minimum invoice amount cannot be negative',
      'Invoice fee quota is out of range',
      'PDF retention must be positive',
    ]) {
      pageKeys.add(dynamicKey)
    }
    for (const sourceFile of sourceFiles) {
      const source = await readFile(sourceFile, 'utf8')
      for (const match of source.matchAll(/\bt\(\s*['"]([^'"]+)['"]/gs)) {
        pageKeys.add(match[1])
      }
    }

    for (const locale of ['en', 'zh', 'zh-TW', 'fr', 'ja', 'ru', 'vi']) {
      const raw = await readFile(
        new URL(`../../i18n/locales/${locale}.json`, import.meta.url),
        'utf8'
      )
      const translation = JSON.parse(raw).translation as Record<string, string>
      const missingKeys = [...pageKeys].filter((key) => !translation[key])
      assert.deepEqual(missingKeys, [], `${locale} missing invoice page keys`)
    }
  })

  test('production composition imports HTTP only and never fixture fallback', async () => {
    const source = await readFile(
      new URL('./production-api.ts', import.meta.url),
      'utf8'
    )
    assert.match(source, /createHttpInvoiceApi/)
    assert.doesNotMatch(source, /demo-api|fixture|fallback/i)
  })

  test('query keys expose precise user list prefixes', () => {
    assert.deepEqual(invoiceQueryKeys.eligibleOrdersList(), [
      'invoices',
      'eligible-orders',
    ])
    assert.deepEqual(invoiceQueryKeys.applicationsList(), [
      'invoices',
      'applications',
    ])
  })
})
