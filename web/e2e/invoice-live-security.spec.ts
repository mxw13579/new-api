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
  }))
  const surfaces = [...Object.values(residue), ...consoleText]
  for (const surface of surfaces) {
    for (const sentinel of forbiddenSentinels) {
      expect(surface).not.toContain(sentinel)
    }
  }
})
