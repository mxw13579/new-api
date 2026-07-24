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

import { renderToStaticMarkup } from 'react-dom/server'

import type { InvoiceApplicationDetail } from '../types'

type MockModule = (
  specifier: string,
  factory: () => Record<string, unknown>
) => void
const mockModule = (
  (await import('bun:test')) as unknown as { mock: { module: MockModule } }
).mock.module

mockModule('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

const { DocumentUpload } = await import('./document-upload')
const { SettingsForm } = await import('./settings-form')

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
    version: 1,
  },
  policy_snapshot: {
    application_window_days: 30,
    minimum_amount_minor: 0,
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

describe('invoice administrator pending field behavior', () => {
  it('marks settings numeric Fields and controls disabled', () => {
    const html = render(
      <SettingsForm
        setting={{
          personal_enabled: true,
          company_enabled: true,
          application_window_days: 30,
          minimum_amount_minor: 0,
          fee_quota: 0,
          pdf_retention_days: 90,
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

  it('marks the upload file Field and control disabled', () => {
    const html = render(
      <DocumentUpload
        application={application}
        pending
        onUpload={() => undefined}
      />
    )

    assert.match(
      html,
      /<div role="group"[^>]*data-slot="field"[^>]*data-disabled="true"[^>]*>[\s\S]*?<input[^>]*id="invoice-pdf-file"[^>]*disabled=""/
    )
    assert.match(html, /aria-describedby="invoice-pdf-help"/)
  })
})
