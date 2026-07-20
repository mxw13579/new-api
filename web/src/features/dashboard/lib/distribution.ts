import type {
  DistributionDimension,
  DistributionEndpointRow,
  DistributionMetric,
} from '../types'

export type {
  DistributionDimension,
  DistributionEndpointRow,
  DistributionMetric,
} from '../types'

export const DISTRIBUTION_TOP_N = 8

export type DistributionMetrics = Record<DistributionMetric, number>

export type DistributionSegment = {
  dimension: DistributionDimension | 'other'
  id: string
  label: string
  metrics: DistributionMetrics
  value: number
  percent: number
  percentLabel: string
  isOther: boolean
  sourceIds: string[]
}

export type DistributionRankingRow = {
  rank: number
  segment: DistributionSegment
  label: string
  value: number
  percent: number
  percentLabel: string
  metrics: DistributionMetrics
}

export type DistributionAggregationResult = {
  selectedMetric: DistributionMetric
  segments: DistributionSegment[]
  rankingRows: DistributionRankingRow[]
  summary: DistributionMetrics
  showDonut: boolean
  hasNegativeValues: boolean
}

type MutableDistributionBucket = {
  dimension: DistributionDimension
  id: string
  label: string
  metrics: DistributionMetrics
}

const metricNames: DistributionMetric[] = ['quota', 'tokens', 'requests']

const emptyMetrics = (): DistributionMetrics => ({
  quota: 0,
  tokens: 0,
  requests: 0,
})

const normalizeMetricValue = (value: number): number => {
  if (!Number.isFinite(value) || value <= 0) {
    return 0
  }

  return value
}

const roundToOneDecimal = (value: number): number =>
  Number((Math.round(value * 10) / 10).toFixed(1))

const compareText = (left: string, right: string): number => {
  if (left < right) {
    return -1
  }

  if (left > right) {
    return 1
  }

  return 0
}

const buildSegment = (
  bucket: MutableDistributionBucket,
  selectedMetric: DistributionMetric
): DistributionSegment => ({
  dimension: bucket.dimension,
  id: bucket.id,
  label: bucket.label,
  metrics: { ...bucket.metrics },
  value: bucket.metrics[selectedMetric],
  percent: 0,
  percentLabel: '—',
  isOther: false,
  sourceIds: [bucket.id],
})

const buildOtherSegment = (
  buckets: MutableDistributionBucket[],
  selectedMetric: DistributionMetric
): DistributionSegment => {
  const metrics = buckets.reduce<DistributionMetrics>((sum, bucket) => {
    for (const metricName of metricNames) {
      sum[metricName] += bucket.metrics[metricName]
    }

    return sum
  }, emptyMetrics())

  return {
    dimension: 'other',
    id: 'other',
    label: 'Other',
    metrics,
    value: metrics[selectedMetric],
    percent: 0,
    percentLabel: '—',
    isOther: true,
    sourceIds: buckets.map((bucket) => bucket.id),
  }
}

const applyPercentages = (
  segments: DistributionSegment[],
  selectedTotal: number
): void => {
  if (selectedTotal <= 0 || segments.length === 0) {
    return
  }

  let tailIndex = -1
  for (let index = segments.length - 1; index >= 0; index -= 1) {
    if (!segments[index]?.isOther) {
      tailIndex = index
      break
    }
  }
  if (tailIndex === -1) {
    tailIndex = segments.length - 1
  }

  let assignedPercent = 0
  for (const [index, segment] of segments.entries()) {
    if (index === tailIndex) {
      continue
    }

    segment.percent = roundToOneDecimal((segment.value / selectedTotal) * 100)
    segment.percentLabel = `${segment.percent.toFixed(1)}%`
    assignedPercent += segment.percent
  }

  const tailSegment = segments[tailIndex]
  if (tailSegment) {
    tailSegment.percent = Number((100 - assignedPercent).toFixed(1))
    tailSegment.percentLabel = `${tailSegment.percent.toFixed(1)}%`
  }
}

export const buildDistributionAggregation = (
  rows: DistributionEndpointRow[],
  selectedMetric: DistributionMetric
): DistributionAggregationResult => {
  const bucketsById = new Map<string, MutableDistributionBucket>()
  let hasNegativeValues = false

  for (const sourceRow of rows) {
    const stableId = sourceRow.id.trim() || sourceRow.label.trim() || 'unknown'
    const label = sourceRow.label.trim() || stableId
    const metrics = emptyMetrics()

    for (const metricName of metricNames) {
      const rawValue = sourceRow[metricName]
      if (rawValue < 0) {
        hasNegativeValues = true
      }
      metrics[metricName] = normalizeMetricValue(rawValue)
    }

    const existingBucket = bucketsById.get(stableId)
    if (existingBucket) {
      if (existingBucket.label === existingBucket.id && label !== stableId) {
        existingBucket.label = label
      }
      for (const metricName of metricNames) {
        existingBucket.metrics[metricName] += metrics[metricName]
      }
      continue
    }

    bucketsById.set(stableId, {
      dimension: sourceRow.dimension,
      id: stableId,
      label,
      metrics,
    })
  }

  const summary = [...bucketsById.values()].reduce<DistributionMetrics>(
    (sum, bucket) => {
      for (const metricName of metricNames) {
        sum[metricName] += bucket.metrics[metricName]
      }

      return sum
    },
    emptyMetrics()
  )

  const selectedTotal = summary[selectedMetric]
  if (selectedTotal <= 0) {
    return {
      selectedMetric,
      segments: [],
      rankingRows: [],
      summary,
      showDonut: false,
      hasNegativeValues,
    }
  }

  const sortedBuckets = [...bucketsById.values()].sort((left, right) => {
    const metricDifference =
      right.metrics[selectedMetric] - left.metrics[selectedMetric]
    if (metricDifference !== 0) {
      return metricDifference
    }

    const labelDifference = compareText(left.label, right.label)
    if (labelDifference !== 0) {
      return labelDifference
    }

    return compareText(left.id, right.id)
  })

  const topBuckets = sortedBuckets.slice(0, DISTRIBUTION_TOP_N)
  const hiddenBuckets = sortedBuckets.slice(DISTRIBUTION_TOP_N)
  const segments = topBuckets.map((bucket) =>
    buildSegment(bucket, selectedMetric)
  )
  const hiddenMetrics = hiddenBuckets.reduce<DistributionMetrics>(
    (sum, bucket) => {
      for (const metricName of metricNames) {
        sum[metricName] += bucket.metrics[metricName]
      }

      return sum
    },
    emptyMetrics()
  )
  const shouldShowOther = metricNames.some(
    (metricName) => hiddenMetrics[metricName] > 0
  )

  if (shouldShowOther) {
    segments.push(buildOtherSegment(hiddenBuckets, selectedMetric))
  }

  applyPercentages(segments, selectedTotal)

  return {
    selectedMetric,
    segments,
    rankingRows: segments.map((segment, index) => ({
      rank: index + 1,
      segment,
      label: segment.label,
      value: segment.value,
      percent: segment.percent,
      percentLabel: segment.percentLabel,
      metrics: segment.metrics,
    })),
    summary,
    showDonut: true,
    hasNegativeValues,
  }
}
