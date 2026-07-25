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
import { describe, expect, it } from 'bun:test'

import { formatQuota } from '@/lib/format'

import type { InvoiceFeeLedgerEntry } from '../../invoices/types'
import { createInvoiceFeeEntryPresentation } from './invoice-fee-history'

const t = (key: string) => key

describe('invoice fee history presentation', () => {
  it('shows an applied charge with wallet-currency balances', () => {
    const entry: InvoiceFeeLedgerEntry = {
      id: 1,
      application_id: 7,
      application_no: 'INV-2026-7',
      entry_type: 'charge',
      fee_percent: 5,
      quota: 500_000,
      balance_before: 2_000_000,
      balance_after: 1_500_000,
      status: 'applied',
      applied_at: 1_760_000_000,
    }

    const display = createInvoiceFeeEntryPresentation(entry, t)
    expect(display.applicationNo).toBe('INV-2026-7')
    expect(display.entryType).toBe('Invoice fee charge')
    expect(display.feePercent).toBe('5%')
    expect(display.quota).toBe(`-${formatQuota(500_000)}`)
    expect(display.balanceBefore).toBe(formatQuota(2_000_000))
    expect(display.balanceAfter).toBe(formatQuota(1_500_000))
    expect(display.status).toBe('Invoice fee applied')
  })

  it('shows a pending refund without inventing unavailable balances or time', () => {
    const entry: InvoiceFeeLedgerEntry = {
      id: 2,
      application_id: 7,
      application_no: 'INV-2026-7',
      entry_type: 'refund',
      fee_percent: 5,
      quota: 500_000,
      balance_before: null,
      balance_after: null,
      status: 'pending',
      applied_at: null,
    }

    expect(createInvoiceFeeEntryPresentation(entry, t)).toEqual({
      applicationNo: 'INV-2026-7',
      entryType: 'Invoice fee refund',
      feePercent: '5%',
      quota: `+${formatQuota(500_000)}`,
      balanceBefore: 'Not available',
      balanceAfter: 'Not available',
      status: 'Invoice fee pending',
      appliedAt: 'Not available',
    })
  })
})
