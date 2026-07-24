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

import type { QueryClient } from '@tanstack/react-query'

import {
  createInvoiceDraftIdentity,
  createInvoiceOrderSelection,
  getInvoiceOrderSelectionSummary,
  invalidateInvoiceApplicationConflictQueries,
  invalidateUserInvoiceMutationQueries,
  invoicePageCount,
  isInvoiceProfileEnabled,
  reconcileInvoiceOrderSelectionAfterError,
  updateInvoiceOrderSelection,
  shouldPollInvoiceApplication,
} from './user-workspace'

describe('invoice user workspace behavior', () => {
  it('keeps one request id for retries until the draft changes or succeeds', () => {
    let sequence = 0
    const identity = createInvoiceDraftIdentity(() => `request-${++sequence}`)

    expect(identity.forDraft('profile:1:orders:10')).toBe('request-1')
    expect(identity.forDraft('profile:1:orders:10')).toBe('request-1')
    expect(identity.forDraft('profile:1:orders:11')).toBe('request-2')
    expect(identity.forDraft('profile:1:orders:11')).toBe('request-2')

    identity.reset()
    expect(identity.forDraft('profile:1:orders:11')).toBe('request-3')
  })

  it('keeps cross-page amount validation and submitted ids on one selection', () => {
    let selection = createInvoiceOrderSelection()
    selection = updateInvoiceOrderSelection(
      selection,
      { topup_id: 11, paid_amount_minor: 600 },
      true
    )
    selection = updateInvoiceOrderSelection(
      selection,
      { topup_id: 22, paid_amount_minor: 500 },
      true
    )

    expect(getInvoiceOrderSelectionSummary(selection, 1_000)).toEqual({
      topupIds: [11, 22],
      amountMinor: 1_100,
      minimumReached: true,
    })

    selection = updateInvoiceOrderSelection(
      selection,
      { topup_id: 11, paid_amount_minor: 600 },
      false
    )
    expect(getInvoiceOrderSelectionSummary(selection, 1_000)).toEqual({
      topupIds: [22],
      amountMinor: 500,
      minimumReached: false,
    })
  })

  it('recovers after a rejected hidden order disappears from eligibility', () => {
    for (const code of [
      'INVOICE_TOPUP_INELIGIBLE',
      'INVOICE_PAYMENT_EVIDENCE_CONFLICT',
    ] as const) {
      let sequence = 0
      const identity = createInvoiceDraftIdentity(() => `request-${++sequence}`)
      let selection = createInvoiceOrderSelection()
      selection = updateInvoiceOrderSelection(
        selection,
        { topup_id: 11, paid_amount_minor: 600 },
        true
      )
      selection = updateInvoiceOrderSelection(
        selection,
        { topup_id: 22, paid_amount_minor: 500 },
        true
      )
      expect(identity.forDraft('orders:11,22')).toBe('request-1')

      selection = reconcileInvoiceOrderSelectionAfterError(selection, code)
      expect(getInvoiceOrderSelectionSummary(selection, 500)).toEqual({
        topupIds: [],
        amountMinor: 0,
        minimumReached: false,
      })

      selection = updateInvoiceOrderSelection(
        selection,
        { topup_id: 22, paid_amount_minor: 500 },
        true
      )
      expect(getInvoiceOrderSelectionSummary(selection, 500).topupIds).toEqual([
        22,
      ])
      expect(identity.forDraft('orders:22')).toBe('request-2')
      expect(identity.forDraft('orders:22')).toBe('request-2')
    }

    const unchangedSelection = createInvoiceOrderSelection()
    expect(
      reconcileInvoiceOrderSelectionAfterError(
        unchangedSelection,
        'INVOICE_STATE_CONFLICT'
      )
    ).toBe(unchangedSelection)
  })

  it('refreshes a conflicted profile and rotates identity only for its new version', async () => {
    const invalidations: unknown[][] = []
    const queryClient = {
      invalidateQueries: async (filters: { queryKey?: readonly unknown[] }) => {
        invalidations.push(filters.queryKey as unknown[])
      },
    } as QueryClient
    let sequence = 0
    const identity = createInvoiceDraftIdentity(() => `request-${++sequence}`)
    const fingerprint = (profileVersion: number) =>
      JSON.stringify({
        profileId: 7,
        profileVersion,
        topupIds: [22],
        feeQuota: 3,
      })

    expect(identity.forDraft(fingerprint(1))).toBe('request-1')
    expect(identity.forDraft(fingerprint(1))).toBe('request-1')

    await invalidateInvoiceApplicationConflictQueries(
      queryClient,
      'INVOICE_STATE_CONFLICT'
    )
    expect(invalidations).toEqual([
      ['invoices', 'eligible-orders'],
      ['invoices', 'applications'],
      ['user', 'self', 'quota'],
      ['invoices', 'profiles'],
    ])

    expect(identity.forDraft(fingerprint(2))).toBe('request-2')
    expect(identity.forDraft(fingerprint(2))).toBe('request-2')
    identity.reset()
    expect(identity.forDraft(fingerprint(2))).toBe('request-3')

    for (const code of [
      'INVOICE_TOPUP_INELIGIBLE',
      'INVOICE_PAYMENT_EVIDENCE_CONFLICT',
    ] as const) {
      invalidations.length = 0
      await invalidateInvoiceApplicationConflictQueries(queryClient, code)
      expect(invalidations).toEqual([
        ['invoices', 'eligible-orders'],
        ['invoices', 'applications'],
        ['user', 'self', 'quota'],
      ])
    }
  })

  it('invalidates all user lists, the affected detail, and self quota', async () => {
    const invalidations: readonly unknown[][] = []
    const queryClient = {
      invalidateQueries: async (filters: { queryKey?: readonly unknown[] }) => {
        ;(invalidations as unknown[][]).push(filters.queryKey as unknown[])
      },
    } as QueryClient

    await invalidateUserInvoiceMutationQueries(queryClient, 42)

    expect(invalidations).toEqual([
      ['invoices', 'eligible-orders'],
      ['invoices', 'applications'],
      ['invoices', 'application', 42],
      ['user', 'self', 'quota'],
    ])
  })

  it('calculates pagination and enforces per-type configuration', () => {
    expect(invoicePageCount(0, 10)).toBe(1)
    expect(invoicePageCount(21, 10)).toBe(3)
    expect(
      isInvoiceProfileEnabled(
        { personal_enabled: true, company_enabled: false },
        'personal'
      )
    ).toBe(true)
    expect(
      isInvoiceProfileEnabled(
        { personal_enabled: true, company_enabled: false },
        'company'
      )
    ).toBe(false)
  })

  it('polls nonterminal applications and active expiring documents only', () => {
    expect(
      shouldPollInvoiceApplication({
        status: 'reviewing',
        document_status: 'validating',
        document_expires_at: null,
      })
    ).toBe(true)
    expect(
      shouldPollInvoiceApplication(
        {
          status: 'issued',
          document_status: 'available',
          document_expires_at: 2_000_000_000,
        },
        1_900_000_000
      )
    ).toBe(true)
    expect(
      shouldPollInvoiceApplication({
        status: 'cancelled',
        document_status: 'deleted',
        document_expires_at: null,
      })
    ).toBe(false)
    expect(
      shouldPollInvoiceApplication(
        {
          status: 'issued',
          document_status: 'available',
          document_expires_at: 1_800_000_000,
        },
        1_900_000_000
      )
    ).toBe(false)
  })
})
