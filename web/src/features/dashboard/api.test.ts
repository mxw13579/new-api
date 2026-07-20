import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'

import { api } from '../../lib/api'
import { getDistributionData, getSelfQuota } from './api'

const originalGet = api.get

describe('dashboard self quota api', () => {
  afterEach(() => {
    api.get = originalGet
  })

  test('requests the dedicated self quota endpoint', async () => {
    let requestedUrl = ''

    api.get = (async (url: string) => {
      requestedUrl = url
      return {
        data: {
          success: true,
          message: '',
          data: { quota: 123456 },
        },
      }
    }) as typeof api.get

    const result = await getSelfQuota()

    assert.equal(requestedUrl, '/api/user/self/quota')
    assert.equal(result.success, true)
    assert.deepEqual(result.data, { quota: 123456 })
  })

  test('requests the independent distribution endpoint with canonical params', async () => {
    let requestedUrl = ''
    let requestedParams: unknown

    api.get = (async (url: string, config?: { params?: unknown }) => {
      requestedUrl = url
      requestedParams = config?.params
      return {
        data: {
          success: true,
          message: '',
          data: [
            {
              dimension: 'key',
              id: 'key:1',
              label: 'Default key',
              quota: 12,
              tokens: 34,
              requests: 5,
            },
          ],
        },
      }
    }) as typeof api.get

    const result = await getDistributionData({
      start_timestamp: 100,
      end_timestamp: 200,
      metric: 'quota',
      dimension: 'key',
    })

    assert.equal(requestedUrl, '/api/data/distribution')
    assert.deepEqual(requestedParams, {
      start_timestamp: 100,
      end_timestamp: 200,
      metric: 'quota',
      dimension: 'key',
    })
    assert.equal(result.success, true)
    assert.equal(result.data?.[0]?.id, 'key:1')
  })
})
