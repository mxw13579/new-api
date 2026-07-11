import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  DISTRIBUTION_TOP_N,
  buildDistributionAggregation,
  type DistributionEndpointRow,
} from './distribution'

const row = (
  id: string,
  label: string,
  quota: number,
  tokens = 0,
  requests = 0
): DistributionEndpointRow => ({
  dimension: 'key',
  id,
  label,
  quota,
  tokens,
  requests,
})

describe('dashboard distribution aggregation', () => {
  test('returns an empty canonical segment list when the selected total is zero', () => {
    const result = buildDistributionAggregation(
      [row('key:1', 'still has tokens', 0, 30, 2)],
      'quota'
    )

    assert.equal(result.showDonut, false)
    assert.deepEqual(result.segments, [])
    assert.deepEqual(result.rankingRows, [])
    assert.deepEqual(result.summary, { quota: 0, tokens: 30, requests: 2 })
    assert.equal(result.hasNegativeValues, false)
  })

  test('aggregates duplicate stable identities and falls back empty labels to the id', () => {
    const result = buildDistributionAggregation(
      [
        row('key:1', '', 10, 20, 1),
        row('key:1', 'Renamed key', 15, 25, 2),
        row('key:2', '', 5, 10, 3),
      ],
      'quota'
    )

    assert.deepEqual(
      result.segments.map((segment) => ({
        id: segment.id,
        label: segment.label,
        metrics: segment.metrics,
      })),
      [
        {
          id: 'key:1',
          label: 'Renamed key',
          metrics: { quota: 25, tokens: 45, requests: 3 },
        },
        {
          id: 'key:2',
          label: 'key:2',
          metrics: { quota: 5, tokens: 10, requests: 3 },
        },
      ]
    )
  })

  test('keeps same-name token ids separate and sorts ties by label then id', () => {
    const result = buildDistributionAggregation(
      [
        row('key:2', 'Shared', 10),
        row('key:1', 'Shared', 10),
        row('key:3', 'Alpha', 10),
      ],
      'quota'
    )

    assert.deepEqual(
      result.segments.map((segment) => segment.id),
      ['key:3', 'key:1', 'key:2']
    )
  })

  test('uses the selected metric for ordering, percentages, and donut visibility', () => {
    const tokens = buildDistributionAggregation(
      [
        row('key:1', 'small quota', 100, 1, 30),
        row('key:2', 'large tokens', 1, 90, 10),
        row('key:3', 'medium tokens', 1, 9, 60),
      ],
      'tokens'
    )
    const requests = buildDistributionAggregation(
      [
        row('key:1', 'small quota', 100, 1, 30),
        row('key:2', 'large tokens', 1, 90, 10),
        row('key:3', 'medium tokens', 1, 9, 60),
      ],
      'requests'
    )

    assert.equal(tokens.showDonut, true)
    assert.deepEqual(
      tokens.segments.map((segment) => segment.id),
      ['key:2', 'key:3', 'key:1']
    )
    assert.deepEqual(
      requests.segments.map((segment) => segment.id),
      ['key:3', 'key:1', 'key:2']
    )
  })

  test('normalizes negative metrics to zero and exposes a validation flag', () => {
    const result = buildDistributionAggregation(
      [row('key:1', 'bad source', -10, -20, 4), row('key:2', 'valid', 5)],
      'quota'
    )

    assert.equal(result.hasNegativeValues, true)
    assert.deepEqual(result.summary, { quota: 5, tokens: 0, requests: 4 })
    assert.deepEqual(
      result.segments.map((segment) => ({
        id: segment.id,
        metrics: segment.metrics,
      })),
      [
        { id: 'key:2', metrics: { quota: 5, tokens: 0, requests: 0 } },
        { id: 'key:1', metrics: { quota: 0, tokens: 0, requests: 4 } },
      ]
    )
  })

  test('builds Top 8 plus Other without losing totals or canonical sharing', () => {
    const rows = Array.from({ length: DISTRIBUTION_TOP_N + 1 }, (_, index) =>
      row(`key:${index + 1}`, `Key ${index + 1}`, 1, index + 1, 1)
    )

    const result = buildDistributionAggregation(rows, 'quota')
    const totalPercent = result.segments.reduce(
      (sum, segment) => sum + segment.percent,
      0
    )

    assert.equal(result.segments.length, DISTRIBUTION_TOP_N + 1)
    assert.equal(result.segments.at(-1)?.id, 'other')
    assert.equal(result.segments.at(-1)?.isOther, true)
    assert.deepEqual(result.summary, { quota: 9, tokens: 45, requests: 9 })
    assert.deepEqual(
      result.segments.reduce(
        (sum, segment) => ({
          quota: sum.quota + segment.metrics.quota,
          tokens: sum.tokens + segment.metrics.tokens,
          requests: sum.requests + segment.metrics.requests,
        }),
        { quota: 0, tokens: 0, requests: 0 }
      ),
      result.summary
    )
    assert.equal(Number(totalPercent.toFixed(1)), 100)
    assert.equal(result.segments[DISTRIBUTION_TOP_N - 1]?.percentLabel, '11.2%')
    assert.equal(result.segments.at(-1)?.percentLabel, '11.1%')
    assert.equal(result.rankingRows.length, result.segments.length)
    assert.equal(result.rankingRows[0]?.segment, result.segments[0])
  })
})
