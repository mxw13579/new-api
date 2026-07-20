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
