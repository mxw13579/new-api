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
import { describe, test } from 'node:test'

import { QueryClient } from '@tanstack/react-query'

import { SELF_QUOTA_QUERY_KEY } from '../../dashboard/hooks/use-self-quota'
import {
  refreshWalletAfterConfirmedQuotaChange,
  refreshWalletAfterOrderCreation,
} from './quota-refresh'

describe('wallet quota refresh boundaries', () => {
  test('confirmed quota changes invalidate self quota and then refresh wallet data', async () => {
    const queryClient = new QueryClient()
    const calls: string[] = []

    queryClient.invalidateQueries = ((filters: unknown) => {
      calls.push(JSON.stringify(filters))
      return Promise.resolve()
    }) as typeof queryClient.invalidateQueries

    await refreshWalletAfterConfirmedQuotaChange(queryClient, async () => {
      calls.push('fetchUser')
    })

    assert.deepEqual(calls, [
      JSON.stringify({ queryKey: SELF_QUOTA_QUERY_KEY, exact: true }),
      'fetchUser',
    ])
  })

  test('order creation refreshes wallet data without invalidating self quota', async () => {
    const calls: string[] = []

    await refreshWalletAfterOrderCreation(async () => {
      calls.push('fetchUser')
    })

    assert.deepEqual(calls, ['fetchUser'])
  })
})
