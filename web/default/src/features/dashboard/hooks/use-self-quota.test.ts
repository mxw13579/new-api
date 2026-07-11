import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { QueryClient } from '@tanstack/react-query'

import {
  SELF_QUOTA_QUERY_KEY,
  SELF_QUOTA_REFETCH_INTERVAL,
  SELF_QUOTA_STALE_TIME,
  getSelfQuotaQueryOptions,
  invalidateSelfQuotaQuery,
} from './use-self-quota'

describe('self quota query options', () => {
  test('uses the dedicated query key and refresh policy', () => {
    const options = getSelfQuotaQueryOptions()

    assert.deepEqual(options.queryKey, ['user', 'self', 'quota'])
    assert.equal(options.queryKey, SELF_QUOTA_QUERY_KEY)
    assert.equal(options.refetchInterval, SELF_QUOTA_REFETCH_INTERVAL)
    assert.equal(options.refetchInterval, 300000)
    assert.equal(options.refetchIntervalInBackground, false)
    assert.equal(options.refetchOnWindowFocus, true)
    assert.equal(options.staleTime, SELF_QUOTA_STALE_TIME)
    assert.equal(options.staleTime, 60000)
  })

  test('keeps cached quota after a failed same-key refetch', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    queryClient.setQueryData(
      SELF_QUOTA_QUERY_KEY,
      { quota: 9000 },
      { updatedAt: 0 }
    )

    await assert.rejects(
      queryClient.fetchQuery({
        ...getSelfQuotaQueryOptions(),
        queryFn: async () => {
          throw new Error('network down')
        },
      })
    )

    assert.deepEqual(queryClient.getQueryData(SELF_QUOTA_QUERY_KEY), {
      quota: 9000,
    })
  })

  test('invalidates only the self quota key for confirmed quota changes', async () => {
    const queryClient = new QueryClient()
    const invalidations: unknown[] = []
    const originalInvalidate = queryClient.invalidateQueries.bind(queryClient)

    queryClient.invalidateQueries = ((filters: unknown) => {
      invalidations.push(filters)
      return Promise.resolve()
    }) as typeof queryClient.invalidateQueries

    try {
      await invalidateSelfQuotaQuery(queryClient)
    } finally {
      queryClient.invalidateQueries = originalInvalidate
    }

    assert.deepEqual(invalidations, [
      { queryKey: ['user', 'self', 'quota'], exact: true },
    ])
  })
})
