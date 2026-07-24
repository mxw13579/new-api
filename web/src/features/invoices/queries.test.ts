/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  redactInvoiceApplicationDetailForCache,
  redactInvoiceProfilesForCache,
} from './queries'
import type { InvoiceApplicationDetail, InvoiceProfile } from './types'

describe('invoice query-cache redaction', () => {
  test('removes raw tax numbers without mutating display profiles', () => {
    const profile = {
      id: 1,
      type: 'company',
      title: 'Buyer',
      tax_number: '91310000PRIVATE',
      is_default: true,
      version: 1,
      created_at: 1,
      updated_at: 1,
    } satisfies InvoiceProfile

    assert.equal(redactInvoiceProfilesForCache([profile])[0]?.tax_number, '')
    assert.equal(profile.tax_number, '91310000PRIVATE')
  })

  test('removes snapshot tax numbers without mutating display details', () => {
    const detail = {
      id: 1,
      profile_snapshot: { tax_number: '91310000PRIVATE' },
    } as InvoiceApplicationDetail

    assert.equal(
      redactInvoiceApplicationDetailForCache(detail).profile_snapshot
        .tax_number,
      ''
    )
    assert.equal(detail.profile_snapshot.tax_number, '91310000PRIVATE')
  })
})
