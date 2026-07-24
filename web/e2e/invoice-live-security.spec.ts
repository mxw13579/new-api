/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { expect, test } from '@playwright/test'

const applicationId = 1
const tokens = {
  owner: 'invoice-live-owner-token-00000001',
  other: 'invoice-live-other-token-00000002',
  reviewer: 'invoice-live-review-token-0000003',
  restricted: 'invoice-live-denied-token-0000004',
} as const
const forbiddenSentinels = [
  'X-Amz-Signature=',
  'invoices/live/private-object.pdf',
  '91310000PRIVATE',
]

test('owner binary download uses bearer auth while cross-owner stays masked', async ({
  request,
}) => {
  const owner = await request.get(
    `/api/user/invoices/${applicationId}/document`,
    {
      headers: { Authorization: `Bearer ${tokens.owner}` },
    }
  )
  expect(owner.status()).toBe(200)
  expect(owner.headers()['content-type']).toContain('application/pdf')
  expect(owner.headers()['cache-control']).toContain('no-store')
  expect((await owner.body()).subarray(0, 8).toString()).toBe('%PDF-1.7')

  const crossOwner = await request.get(
    `/api/user/invoices/${applicationId}/document`,
    {
      headers: { Authorization: `Bearer ${tokens.other}` },
    }
  )
  const missing = await request.get('/api/user/invoices/999999/document', {
    headers: { Authorization: `Bearer ${tokens.other}` },
  })
  expect(crossOwner.status()).toBe(404)
  expect(await crossOwner.text()).toBe(await missing.text())
  expect(crossOwner.headers()['cache-control']).toContain('no-store')
})

test('restricted admin is no-store 403 and ordinary reviewer sees masked identity', async ({
  request,
}) => {
  const restricted = await request.get(`/api/admin/invoices/${applicationId}`, {
    headers: { Authorization: `Bearer ${tokens.restricted}` },
  })
  expect(restricted.status()).toBe(403)
  expect(restricted.headers()['cache-control']).toContain('no-store')

  const reviewer = await request.get(`/api/admin/invoices/${applicationId}`, {
    headers: { Authorization: `Bearer ${tokens.reviewer}` },
  })
  expect(reviewer.status()).toBe(200)
  const body = await reviewer.text()
  expect(body).not.toContain('91310000PRIVATE')
  for (const sentinel of forbiddenSentinels) {
    expect(body).not.toContain(sentinel)
  }
})

test('real routes preserve application, conflict, review, replacement, and owner download contracts', async ({
  request,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== 'desktop-chromium',
    'the stateful real-route chain runs once against the shared live server'
  )
  const ownerHeaders = { Authorization: `Bearer ${tokens.owner}` }
  const adminHeaders = { Authorization: `Bearer ${tokens.reviewer}` }
  const profilesResponse = await request.get('/api/user/invoice/profiles', {
    headers: ownerHeaders,
  })
  expect(profilesResponse.status()).toBe(200)
  const profiles = await profilesResponse.json()
  const profile = profiles.data[0] as { id: number; version: number }

  const applicationResponse = await request.post('/api/user/invoices', {
    headers: ownerHeaders,
    data: {
      request_id: 'invoice-live-chain',
      profile_id: profile.id,
      profile_version: profile.version,
      topup_ids: [7001],
    },
  })
  expect(applicationResponse.status()).toBe(201)
  const application = await applicationResponse.json()
  const chainedId = application.data.id as number
  expect(application.data.status).toBe('submitted')
  expect(application.data.items).toHaveLength(1)
  expect(application.data.fee_status).toBe('paid')

  const conflict = await request.post('/api/user/invoices', {
    headers: ownerHeaders,
    data: {
      request_id: 'invoice-live-chain',
      profile_id: profile.id,
      profile_version: profile.version + 1,
      topup_ids: [7001],
    },
  })
  expect(conflict.status()).toBe(409)
  expect((await conflict.json()).data.code).toBe('INVOICE_IDEMPOTENCY_CONFLICT')

  for (const transition of [
    { action: 'reviewing', expected_status: 'submitted' },
    { action: 'approve', expected_status: 'reviewing' },
  ]) {
    const response = await request.post(
      `/api/admin/invoices/${chainedId}/review`,
      { headers: adminHeaders, data: transition }
    )
    expect(response.status()).toBe(200)
  }

  const facts = {
    invoice_number: 'INV-LIVE-CHAIN',
    invoice_code: 'LIVE',
    invoice_date: '1900000000',
    face_amount_minor: '12345',
    currency: 'CNY',
    pdf_facts_attested: 'true',
  }
  const upload = async (expectedStatus: 'approved' | 'issued') =>
    request.post(`/api/admin/invoices/${chainedId}/document`, {
      headers: adminHeaders,
      multipart: {
        ...facts,
        expected_status: expectedStatus,
        file: {
          name: 'invoice.pdf',
          mimeType: 'application/pdf',
          buffer: Buffer.from(buildInvoicePDF()),
        },
      },
    })
  const initial = await upload('approved')
  expect(initial.status()).toBe(200)
  const initialBody = await initial.json()
  expect(initialBody.data.status).toBe('issued')
  const initialDocumentId = initialBody.data.document.id

  const replacement = await upload('issued')
  expect(replacement.status()).toBe(200)
  const replacementBody = await replacement.json()
  expect(replacementBody.data.document.id).not.toBe(initialDocumentId)
  expect(replacementBody.data.issuance.invoice_number).toBe(
    facts.invoice_number
  )

  const ownerDownload = await request.get(
    `/api/user/invoices/${chainedId}/document`,
    { headers: ownerHeaders }
  )
  expect(ownerDownload.status()).toBe(200)
  expect(ownerDownload.headers()['cache-control']).toContain('no-store')
  expect((await ownerDownload.body()).subarray(0, 8).toString()).toBe(
    '%PDF-1.7'
  )
  const ownerDetail = await request.get(`/api/user/invoices/${chainedId}`, {
    headers: ownerHeaders,
  })
  const detailText = await ownerDetail.text()
  expect(detailText).not.toContain('download_url')
  for (const sentinel of forbiddenSentinels.slice(0, 2))
    expect(detailText).not.toContain(sentinel)

  const audit = await request.get(`/__invoice-live/audit/${chainedId}`)
  expect(await audit.json()).toEqual({
    items: 1,
    issuances: 1,
    documents: 2,
    available_documents: 1,
    superseded_documents: 1,
    fee_charges: 1,
  })
})

test('invoice page has keyboard focus visibility, no horizontal overflow, and no sensitive browser residue', async ({
  page,
  context,
}) => {
  const consoleText: string[] = []
  page.on('console', (message) => consoleText.push(message.text()))
  await context.setExtraHTTPHeaders({ Authorization: `Bearer ${tokens.owner}` })
  await page.goto('/invoices')
  await page.keyboard.press('Tab')
  const focused = page.locator(':focus')
  await expect(focused).toBeVisible()
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth <=
        document.documentElement.clientWidth
    )
  ).toBe(true)

  const residue = await page.evaluate(() => ({
    dom: document.documentElement.innerHTML,
    history: location.href,
    local: JSON.stringify(localStorage),
    session: JSON.stringify(sessionStorage),
    queryCache: (
      window as typeof window & {
        __invoiceLiveQueryCacheSnapshot?: () => string
      }
    ).__invoiceLiveQueryCacheSnapshot?.(),
  }))
  expect(residue.queryCache).toBeTruthy()
  for (const sentinel of forbiddenSentinels.slice(0, 2)) {
    expect(residue.queryCache).not.toContain(sentinel)
  }
  const surfaces = [
    residue.dom,
    residue.history,
    residue.local,
    residue.session,
    ...consoleText,
  ]
  for (const surface of surfaces) {
    for (const sentinel of forbiddenSentinels) {
      expect(surface).not.toContain(sentinel)
    }
  }
})

function buildInvoicePDF(): string {
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /Contents 4 0 R >>',
    '<< /Length 0 >>\nstream\n\nendstream',
  ]
  let output = '%PDF-1.7\n'
  const offsets = [0]
  objects.forEach((object, index) => {
    offsets.push(Buffer.byteLength(output))
    output += `${index + 1} 0 obj\n${object}\nendobj\n`
  })
  const xref = Buffer.byteLength(output)
  output += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`
  for (const offset of offsets.slice(1)) {
    output += `${String(offset).padStart(10, '0')} 00000 n \n`
  }
  return `${output}trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
}
