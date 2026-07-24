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
  mobileOwner: 'invoice-live-mobile-token-00000005',
  other: 'invoice-live-other-token-00000002',
  reviewer: 'invoice-live-review-token-0000003',
  restricted: 'invoice-live-denied-token-0000004',
} as const
const forbiddenSentinels = [
  'X-Amz-Signature=',
  'invoices/live/private-object.pdf',
  '91310000PRIVATE',
  'Live Fixture Co',
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

test('real pages preserve application, conflict, review, replacement, and owner download contracts', async ({
  browser,
  request,
}, testInfo) => {
  const mobile = testInfo.project.name === 'mobile-chromium'
  const ownerToken = mobile ? tokens.mobileOwner : tokens.owner
  const scenario = mobile ? 'mobile' : 'desktop'
  const viewport = mobile
    ? { width: 390, height: 844 }
    : { width: 1280, height: 720 }
  const ownerContext = await browser.newContext({
    viewport,
    extraHTTPHeaders: { Authorization: `Bearer ${ownerToken}` },
  })
  await ownerContext.addInitScript(() =>
    window.localStorage.setItem('setup_status_checked', 'true')
  )
  const ownerPage = await ownerContext.newPage()
  await ownerPage.goto('/invoices')
  expect(await ownerPage.evaluate(() => window.innerWidth)).toBe(viewport.width)
  await expect(
    ownerPage.getByRole('heading', { name: 'Invoices', exact: true })
  ).toBeVisible()
  await ownerPage.getByLabel('Invoice profile').selectOption({ index: 1 })
  await ownerPage
    .getByRole('checkbox', {
      name: `invoice-live-topup-${scenario}`,
    })
    .check()

  const editorContext = await browser.newContext({
    viewport,
    extraHTTPHeaders: { Authorization: `Bearer ${ownerToken}` },
  })
  await editorContext.addInitScript(() =>
    window.localStorage.setItem('setup_status_checked', 'true')
  )
  const editorPage = await editorContext.newPage()
  await editorPage.goto('/invoices')
  await editorPage.getByRole('tab', { name: 'Profiles' }).click()
  await editorPage.getByRole('button', { name: 'Edit' }).click()
  const editorDialog = editorPage.getByRole('dialog')
  await expect(editorDialog.getByLabel('Company name')).toHaveValue(
    'Live Fixture Co'
  )
  await expect(editorDialog.getByLabel('Tax number')).toHaveValue(
    '91310000PRIVATE'
  )
  await editorDialog.getByRole('button', { name: 'Save' }).click()
  await expect(editorPage.getByText('Invoice profile saved')).toBeVisible()
  await editorContext.close()

  await ownerPage
    .getByRole('button', { name: 'Review invoice application' })
    .click()
  const confirmation = ownerPage.getByRole('dialog')
  await confirmation
    .getByRole('button', { name: 'Submit invoice application' })
    .click()
  await expect(
    ownerPage.getByText('Invoice error: data changed, refresh and try again')
  ).toBeVisible()
  await expect(ownerPage.getByLabel('Invoice profile')).toContainText('v2')
  await confirmation
    .getByRole('button', { name: 'Submit invoice application' })
    .click()
  await expect(
    ownerPage.getByText('Invoice application submitted successfully')
  ).toBeVisible()

  const scenarioResponse = await request.get(
    `/__invoice-live/scenario/${scenario}`
  )
  expect(scenarioResponse.status()).toBe(200)
  const scenarioAudit = await scenarioResponse.json()
  const chainedId = scenarioAudit.application_id as number
  const applicationNo = scenarioAudit.application_no as string

  const adminContext = await browser.newContext({
    viewport,
    extraHTTPHeaders: { Authorization: `Bearer ${tokens.reviewer}` },
  })
  await adminContext.addInitScript(() =>
    window.localStorage.setItem('setup_status_checked', 'true')
  )
  const adminPage = await adminContext.newPage()
  await adminPage.goto('/admin-invoices')
  expect(await adminPage.evaluate(() => window.innerWidth)).toBe(viewport.width)
  const applicationRow = adminPage
    .getByRole('row')
    .filter({ hasText: applicationNo })
  await applicationRow.getByRole('button', { name: 'View details' }).click()
  await adminPage.getByRole('button', { name: 'Start review' }).click()
  await expect(adminPage.getByText('Invoice review updated')).toBeVisible()
  await adminPage.getByRole('button', { name: 'Approve invoice' }).click()
  await expect(adminPage.getByText('Invoice review updated')).toBeVisible()

  const invoiceNumber = `INV-LIVE-CHAIN-${scenario.toUpperCase()}`
  const uploadPDF = async (replacement: boolean) => {
    await adminPage.getByLabel('PDF document').setInputFiles({
      name: 'invoice.pdf',
      mimeType: 'application/pdf',
      buffer: Buffer.from(buildInvoicePDF()),
    })
    if (!replacement) {
      await adminPage.getByLabel('Invoice number').fill(invoiceNumber)
      await adminPage.getByLabel('Invoice code').fill('LIVE')
      await adminPage.getByLabel('Invoice date').fill('2030-03-17')
    }
    await adminPage
      .getByRole('checkbox', {
        name: 'I confirm the PDF matches these issuance facts.',
      })
      .check()
    await adminPage
      .getByRole('button', { name: replacement ? 'Replace PDF' : 'Upload PDF' })
      .click()
    await expect(adminPage.getByText('Invoice PDF saved')).toBeVisible()
  }
  await uploadPDF(false)
  await expect(adminPage.getByLabel('Invoice number')).toHaveValue(
    invoiceNumber
  )
  await uploadPDF(true)
  await adminContext.close()

  await ownerPage.reload()
  await ownerPage.getByRole('tab', { name: 'History' }).click()
  const applicationCard = ownerPage
    .locator('[data-slot="card"]')
    .filter({ hasText: applicationNo })
  await expect(applicationCard).toBeVisible()
  await applicationCard.getByRole('button', { name: 'Details' }).click()
  const ownerDetail = ownerPage.getByRole('dialog')
  await expect(ownerDetail.getByText(invoiceNumber)).toBeVisible()
  await expect(ownerDetail.getByText('Live Fixture Co')).toBeVisible()
  await ownerPage.keyboard.press('Escape')
  const downloadEvent = ownerPage.waitForEvent('download')
  await applicationCard.getByRole('button', { name: 'Download PDF' }).click()
  const download = await downloadEvent
  expect(download.suggestedFilename()).toBe(`invoice-${chainedId}.pdf`)

  const convergence = await request.post(
    `/__invoice-live/converge/${chainedId}`
  )
  expect(convergence.status()).toBe(200)
  expect(await convergence.json()).toEqual({
    reconciled: 1,
    cleanup_processed: 2,
    cleanup_deleted: 2,
    cleanup_failed: 0,
  })
  const audit = await request.get(`/__invoice-live/audit/${chainedId}`)
  expect(await audit.json()).toEqual({
    applications: 1,
    items: 1,
    issuances: 1,
    documents: 3,
    available_documents: 0,
    superseded_documents: 0,
    deleted_documents: 2,
    upload_failed_documents: 1,
    fee_charges: 1,
    fee_charge_quota: 10,
    fee_charge_status: 'applied',
    fee_charge_balance_before: 100,
    fee_charge_balance_after: 90,
  })
  await ownerContext.close()
})

test('invoice page has keyboard focus visibility, no horizontal overflow, and no sensitive browser residue', async ({
  page,
  context,
}) => {
  const consoleText: string[] = []
  page.on('console', (message) => consoleText.push(message.text()))
  await page.addInitScript(() =>
    window.localStorage.setItem('setup_status_checked', 'true')
  )
  await context.setExtraHTTPHeaders({ Authorization: `Bearer ${tokens.owner}` })
  await page.goto('/invoices')
  await page.getByRole('tab', { name: 'Profiles' }).click()
  await expect(page.getByText('Live Fixture Co')).toBeVisible()
  await expect(page.getByText('Tax number: 91310000PRIVATE')).toBeVisible()
  await page.getByRole('button', { name: 'Edit' }).click()
  const profileDialog = page.getByRole('dialog')
  await expect(profileDialog.getByLabel('Company name')).toHaveValue(
    'Live Fixture Co'
  )
  await expect(profileDialog.getByLabel('Tax number')).toHaveValue(
    '91310000PRIVATE'
  )
  await profileDialog.getByRole('button', { name: 'Cancel' }).click()
  await page.getByRole('tab', { name: 'History' }).click()
  await page.getByRole('button', { name: 'Details' }).first().click()
  const detailDialog = page.getByRole('dialog')
  await expect(detailDialog.getByText('Live Fixture Co')).toBeVisible()
  await expect(detailDialog.getByText('91310000PRIVATE')).toBeVisible()
  await page.keyboard.press('Escape')
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
  expect(residue.queryCache).toContain('profiles')
  for (const sentinel of forbiddenSentinels) {
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
    for (const sentinel of forbiddenSentinels.slice(0, 2)) {
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
