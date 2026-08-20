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
import { describe, it } from 'bun:test'
import assert from 'node:assert/strict'

import dayjs from 'dayjs'
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

import {
  PROTECTED_INVOICE_VALUE_KEY,
  maskInvoiceSensitiveDetail,
} from '../contract'
import type { InvoiceApplicationDetail } from '../types'

type MockModule = (
  specifier: string,
  factory: () => Record<string, unknown>
) => void
const mockModule = (
  (await import('bun:test')) as unknown as { mock: { module: MockModule } }
).mock.module

mockModule('react-i18next', () => ({
  I18nextProvider: (props: { children?: ReactNode }) => props.children,
  initReactI18next: {
    type: '3rdParty',
    init: () => undefined,
  },
  useTranslation: () => ({ t: (key: string) => key }),
}))

const { DocumentUpload } = await import('./document-upload')
const { SettingsForm } = await import('./settings-form')
const { ApplicationProfileSection } =
  await import('./application-detail-sections')

const application: InvoiceApplicationDetail = {
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
    title: 'Company',
    tax_number: 'Tax',
    identity_card_number: '',
    version: 1,
  },
  policy_snapshot: {
    application_window_days: 30,
    minimum_amount_minor: 0,
    fee_percent: 0,
    fee_quota: 0,
    pdf_retention_days: 90,
  },
  items: [],
  issuance: null,
  document: null,
}

function render(component: React.ReactNode): string {
  return renderToStaticMarkup(component)
}

function assertFieldControlState(
  html: string,
  controlId: string,
  disabled: boolean
): void {
  const field = html
    .match(/<div role="group"[\s\S]*?<\/div>/g)
    ?.find((group) => group.includes(`for="${controlId}"`))
  assert.ok(field, `Field for ${controlId} must render`)
  assert.equal(field.includes('data-disabled="true"'), disabled)

  const control = html.match(new RegExp(`<[^>]+id="${controlId}"[^>]*>`))?.[0]
  assert.ok(control, `Control ${controlId} must render`)
  assert.equal(control.includes('disabled=""'), disabled)
}

describe('invoice administrator pending field behavior', () => {
  it('marks settings numeric Fields and controls disabled', () => {
    const html = render(
      <SettingsForm
        setting={{
          personal_enabled: true,
          company_enabled: true,
          application_window_days: 30,
          minimum_amount_minor: 0,
          fee_percent: 0,
          pdf_retention_days: 90,
          r2_endpoint: '',
          r2_bucket: '',
          r2_access_key_id: '',
          r2_secret_configured: false,
        }}
        pending
        onSave={() => undefined}
      />
    )

    assert.match(
      html,
      /<div role="group"[^>]*data-slot="field"[^>]*data-disabled="true"[^>]*>[\s\S]*?<input[^>]*id="invoice-application-window"[^>]*disabled=""/
    )
    assert.match(
      html,
      /<div role="group"[^>]*data-slot="field"[^>]*data-disabled="true"[^>]*>[\s\S]*?<input[^>]*id="invoice-pdf-retention"[^>]*disabled=""/
    )
  })

  it('mirrors pending state on every upload Field and control', () => {
    const html = render(
      <DocumentUpload
        application={application}
        pending
        onUpload={() => undefined}
      />
    )

    for (const controlId of [
      'invoice-pdf-file',
      'invoice-number',
      'invoice-code',
      'invoice-date',
      'invoice-face-amount',
      'invoice-currency',
      'invoice-pdf-attestation',
    ]) {
      assertFieldControlState(html, controlId, true)
    }
    assert.match(html, /aria-describedby="invoice-pdf-help"/)
  })

  it('mirrors replacement state only on locked issuance facts', () => {
    const replacement: InvoiceApplicationDetail = {
      ...application,
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
    const html = render(
      <DocumentUpload
        application={replacement}
        pending={false}
        onUpload={() => undefined}
      />
    )

    for (const controlId of [
      'invoice-number',
      'invoice-code',
      'invoice-date',
      'invoice-face-amount',
      'invoice-currency',
    ]) {
      assertFieldControlState(html, controlId, true)
    }
    assertFieldControlState(html, 'invoice-pdf-file', false)
    assertFieldControlState(html, 'invoice-pdf-attestation', false)
  })

  it('renders accessible enabled upload Fields before interaction', () => {
    const html = render(
      <DocumentUpload
        application={application}
        pending={false}
        onUpload={() => undefined}
      />
    )

    for (const controlId of [
      'invoice-pdf-file',
      'invoice-number',
      'invoice-code',
      'invoice-date',
      'invoice-pdf-attestation',
    ]) {
      assertFieldControlState(html, controlId, false)
    }
    assertFieldControlState(html, 'invoice-face-amount', true)
    assertFieldControlState(html, 'invoice-currency', true)
    assert.match(html, /id="invoice-pdf-help"/)
    assert.match(html, /aria-describedby="invoice-pdf-help"/)
    assert.match(
      html,
      new RegExp(
        `id="invoice-date"[^>]*value="${dayjs().format('YYYY-MM-DD')}"`
      )
    )
    assert.match(html, /id="invoice-face-amount"[^>]*value="[^"]*12\.34"/)
    assert.doesNotMatch(html, /id="invoice-face-amount"[^>]*value="1234"/)
  })

  it('renders masked profile details without sensitive values', () => {
    const sensitive: InvoiceApplicationDetail = {
      ...application,
      profile_snapshot: {
        ...application.profile_snapshot,
        title: 'Secret Company',
        tax_number: '91310000SECRET',
      },
    }
    const html = render(
      <ApplicationProfileSection
        application={maskInvoiceSensitiveDetail(sensitive, false)}
      />
    )

    assert.doesNotMatch(html, /Secret Company|91310000SECRET/)
    assert.equal(
      html.match(new RegExp(PROTECTED_INVOICE_VALUE_KEY, 'g'))?.length,
      2
    )
    assert.match(html, /aria-labelledby="invoice-profile-heading"/)
  })
})
