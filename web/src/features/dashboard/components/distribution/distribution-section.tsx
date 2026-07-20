import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { getDistributionData } from '@/features/dashboard/api'
import {
  buildDistributionAggregation,
  type DistributionDimension,
  type DistributionMetric,
  type DistributionSegment,
} from '@/features/dashboard/lib/distribution'
import { formatNumber, formatQuota } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { dateToUnixTimestamp, getRollingDateRange } from '@/lib/time'
import { useChartTheme } from '@/lib/use-chart-theme'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'
import { useAuthStore } from '@/stores/auth-store'

import {
  DISTRIBUTION_QUERY_KEY,
  METRIC_OPTIONS,
  RANGE_OPTIONS,
  TABLE_HEADERS,
  getAllowedDistributionDimensions,
} from './distribution-contract'

type Translate = (key: string, options?: Record<string, string>) => string

function formatMetricValue(metric: DistributionMetric, value: number): string {
  if (metric === 'quota') {
    return formatQuota(value)
  }

  return formatNumber(value)
}

function getDisplayLabel(segment: DistributionSegment, t: Translate) {
  if (segment.isOther) {
    return t('Other')
  }

  if (segment.dimension === 'key' && !segment.label.trim()) {
    return t('Unknown key ({{id}})', { id: segment.id })
  }

  return segment.label
}

function buildDonutSpec(
  segments: DistributionSegment[],
  metric: DistributionMetric,
  t: Translate
) {
  return {
    type: 'pie',
    innerRadius: 0.62,
    padAngle: 1,
    data: [
      {
        id: 'distribution',
        values: segments.map((segment) => ({
          id: segment.id,
          label: getDisplayLabel(segment, t),
          value: segment.value,
          percentLabel: segment.percentLabel,
          metricLabel: t(
            METRIC_OPTIONS.find((option) => option.value === metric)
              ?.labelKey ?? 'Quota'
          ),
          valueLabel: formatMetricValue(metric, segment.value),
        })),
      },
    ],
    valueField: 'value',
    categoryField: 'label',
    legends: {
      visible: true,
      orient: 'bottom',
      item: { visible: true },
    },
    tooltip: {
      mark: {
        title: (datum: Record<string, unknown>) => String(datum.label ?? ''),
        content: [
          {
            key: (datum: Record<string, unknown>) =>
              String(datum.metricLabel ?? ''),
            value: (datum: Record<string, unknown>) =>
              `${String(datum.valueLabel ?? '')} · ${String(
                datum.percentLabel ?? ''
              )}`,
          },
        ],
      },
    },
    label: {
      visible: true,
      formatMethod: (text: string, datum: Record<string, unknown>) =>
        `${text} ${String(datum.percentLabel ?? '')}`,
    },
  }
}

function distributionStatusMessage(options: {
  isError: boolean
  isFetching: boolean
  dataUpdatedAt: number
  t: Translate
}) {
  if (options.isError) {
    return options.t('Distribution refresh failed')
  }

  if (options.isFetching) {
    return options.t('Updating distribution data')
  }

  if (options.dataUpdatedAt > 0) {
    return `${options.t('Last updated')}: ${new Date(
      options.dataUpdatedAt
    ).toLocaleString()}`
  }

  return options.t('Loading distribution data')
}

export function DistributionSection() {
  const { t } = useTranslation()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const { resolvedTheme, themeReady } = useChartTheme()
  const allowedDimensions = useMemo(
    () => getAllowedDistributionDimensions(userRole),
    [userRole]
  )
  const [metric, setMetric] = useState<DistributionMetric>('quota')
  const [dimension, setDimension] = useState<DistributionDimension>(
    allowedDimensions[0]?.value ?? 'key'
  )
  const [selectedRange, setSelectedRange] = useState(7)
  const [username, setUsername] = useState('')
  const canFilterUser = Boolean(userRole && userRole >= ROLE.ADMIN)

  useEffect(() => {
    if (!allowedDimensions.some((option) => option.value === dimension)) {
      setDimension(allowedDimensions[0]?.value ?? 'group')
    }
  }, [allowedDimensions, dimension])

  const range = useMemo(
    () => getRollingDateRange(selectedRange),
    [selectedRange]
  )
  const startTimestamp = dateToUnixTimestamp(range.start)
  const endTimestamp = dateToUnixTimestamp(range.end)
  const trimmedUsername = username.trim()
  const query = useQuery({
    queryKey: [
      ...DISTRIBUTION_QUERY_KEY,
      startTimestamp,
      endTimestamp,
      metric,
      dimension,
      canFilterUser ? trimmedUsername : '',
    ],
    queryFn: async () => {
      const result = await getDistributionData({
        start_timestamp: startTimestamp,
        end_timestamp: endTimestamp,
        metric,
        dimension,
        ...(canFilterUser && trimmedUsername
          ? { username: trimmedUsername }
          : {}),
      })
      if (!result.success) {
        throw new Error(result.message || 'Distribution refresh failed')
      }
      return result.data ?? []
    },
    placeholderData: (previousData) => previousData,
  })
  const aggregation = useMemo(
    () => buildDistributionAggregation(query.data ?? [], metric),
    [query.data, metric]
  )
  const donutSpec = useMemo(
    () => buildDonutSpec(aggregation.segments, metric, t),
    [aggregation.segments, metric, t]
  )
  const selectedMetricLabel =
    METRIC_OPTIONS.find((option) => option.value === metric)?.labelKey ??
    'Quota'
  const statusMessage = distributionStatusMessage({
    isError: query.isError,
    isFetching: query.isFetching,
    dataUpdatedAt: query.dataUpdatedAt,
    t,
  })

  let distributionContent: ReactNode = (
    <div className='text-muted-foreground rounded-lg border px-4 py-10 text-center text-sm'>
      {t('No distribution data')}
    </div>
  )
  if (query.isLoading && !query.data) {
    distributionContent = <DistributionSkeleton />
  } else if (aggregation.showDonut) {
    distributionContent = (
      <DistributionChartAndTable
        segments={aggregation.segments}
        metric={metric}
        selectedMetricLabel={selectedMetricLabel}
        donutSpec={donutSpec}
        themeReady={themeReady}
        resolvedTheme={resolvedTheme}
        t={t}
      />
    )
  }

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Quota Distribution')}</CardTitle>
          <CardDescription>
            {t('Analyze consumption share by user, API key, or group.')}
          </CardDescription>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
            <div className='space-y-1.5'>
              <Label id='distribution-range-label'>{t('Time range')}</Label>
              <div
                className='flex flex-wrap gap-1.5'
                aria-labelledby='distribution-range-label'
              >
                {RANGE_OPTIONS.map((option) => (
                  <Button
                    key={option.days}
                    type='button'
                    variant={
                      selectedRange === option.days ? 'default' : 'outline'
                    }
                    size='sm'
                    onClick={() => setSelectedRange(option.days)}
                    aria-pressed={selectedRange === option.days}
                    aria-label={t(option.labelKey)}
                  >
                    {t(option.labelKey)}
                  </Button>
                ))}
              </div>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='distribution-metric'>{t('Metric')}</Label>
              <Select
                value={metric}
                onValueChange={(value) =>
                  setMetric(value as DistributionMetric)
                }
              >
                <SelectTrigger
                  id='distribution-metric'
                  className='w-full'
                  aria-label={t('Metric')}
                >
                  <SelectValue placeholder={t('Select metric')} />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {METRIC_OPTIONS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {t(option.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='distribution-dimension'>
                {t('Distribution by')}
              </Label>
              <Select
                value={dimension}
                onValueChange={(value) =>
                  setDimension(value as DistributionDimension)
                }
              >
                <SelectTrigger
                  id='distribution-dimension'
                  className='w-full'
                  aria-label={t('Distribution by')}
                >
                  <SelectValue placeholder={t('Select dimension')} />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {allowedDimensions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {t(option.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            {canFilterUser && (
              <div className='space-y-1.5'>
                <Label htmlFor='distribution-username'>
                  {t('Username filter')}
                </Label>
                <Input
                  id='distribution-username'
                  value={username}
                  onChange={(event) => setUsername(event.currentTarget.value)}
                  placeholder={t('Filter by username')}
                  aria-label={t('Username filter')}
                />
              </div>
            )}
          </div>

          <div className='grid gap-2 sm:grid-cols-3'>
            {METRIC_OPTIONS.map((option) => (
              <div
                key={option.value}
                className='bg-muted/40 rounded-lg border px-3 py-2'
              >
                <div className='text-muted-foreground text-xs'>
                  {t(option.labelKey)}
                </div>
                <div className='text-base font-semibold'>
                  {formatMetricValue(
                    option.value,
                    aggregation.summary[option.value]
                  )}
                </div>
              </div>
            ))}
          </div>

          <div
            className='text-muted-foreground text-xs'
            role='status'
            aria-live='polite'
          >
            {statusMessage}
          </div>

          {query.isError && (
            <div className='border-destructive/40 bg-destructive/5 text-destructive flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm'>
              <span>{t('Distribution refresh failed')}</span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => void query.refetch()}
                aria-label={t('Refresh distribution')}
              >
                <RefreshCw className='size-3.5' aria-hidden />
                {t('Refresh distribution')}
              </Button>
            </div>
          )}

          {distributionContent}
        </CardContent>
      </Card>
    </div>
  )
}

function DistributionChartAndTable(props: {
  segments: DistributionSegment[]
  metric: DistributionMetric
  selectedMetricLabel: string
  donutSpec: Record<string, unknown>
  themeReady: boolean
  resolvedTheme: string
  t: Translate
}) {
  return (
    <div className='grid gap-4 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:items-start'>
      <div
        className='min-w-0 rounded-lg border p-3'
        role='img'
        aria-label={props.t('Quota Distribution')}
      >
        <div className='mb-2 text-sm font-medium'>
          {props.t('Distribution by')} {props.t(props.selectedMetricLabel)}
        </div>
        <div className='h-[280px] sm:h-[340px]'>
          {props.themeReady && (
            <VChart
              spec={{
                ...props.donutSpec,
                theme: props.resolvedTheme === 'dark' ? 'dark' : 'light',
                background: 'transparent',
              }}
              option={VCHART_OPTION}
            />
          )}
        </div>
        <div className='mt-2 grid gap-1.5 text-xs'>
          {props.segments.map((segment, index) => (
            <div
              key={segment.id}
              className='flex items-center justify-between gap-2'
            >
              <span className='truncate'>
                {index + 1}. {getDisplayLabel(segment, props.t)}
              </span>
              <span className='text-muted-foreground shrink-0'>
                {segment.percentLabel} {' · '}
                {formatMetricValue(props.metric, segment.value)}
              </span>
            </div>
          ))}
        </div>
      </div>
      <DistributionRankingTable
        segments={props.segments}
        metric={props.metric}
        t={props.t}
      />
    </div>
  )
}

function DistributionSkeleton() {
  return (
    <div className='grid gap-4 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]'>
      <Skeleton className='h-[320px] rounded-lg' />
      <Skeleton className='h-[320px] rounded-lg' />
    </div>
  )
}

function DistributionRankingTable(props: {
  segments: DistributionSegment[]
  metric: DistributionMetric
  t: Translate
}) {
  return (
    <div className='min-w-0 overflow-x-auto rounded-lg border'>
      <table className='w-full min-w-[720px] text-sm'>
        <caption className='sr-only'>{props.t('Top contributors')}</caption>
        <thead className='bg-muted/50 text-muted-foreground'>
          <tr>
            {TABLE_HEADERS.map((header) => (
              <th key={header} scope='col' className='px-3 py-2 text-left'>
                {props.t(header)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {props.segments.map((segment, index) => (
            <tr key={segment.id} className='border-t'>
              <td className='px-3 py-2 font-medium'>{index + 1}</td>
              <td className='px-3 py-2'>
                <div className='flex flex-col'>
                  <span className='font-medium'>
                    {getDisplayLabel(segment, props.t)}
                  </span>
                  <span className='text-muted-foreground text-xs'>
                    {segment.id}
                  </span>
                </div>
              </td>
              <td className='px-3 py-2'>
                {formatMetricValue(props.metric, segment.value)}
              </td>
              <td className='px-3 py-2'>
                <span
                  className={cn(
                    'inline-flex rounded-full px-2 py-0.5 text-xs font-medium',
                    segment.isOther
                      ? 'bg-muted text-muted-foreground'
                      : 'bg-primary/10 text-primary'
                  )}
                >
                  {segment.percentLabel}
                </span>
              </td>
              <td className='px-3 py-2'>
                {formatQuota(segment.metrics.quota)}
              </td>
              <td className='px-3 py-2'>
                {formatNumber(segment.metrics.tokens)}
              </td>
              <td className='px-3 py-2'>
                {formatNumber(segment.metrics.requests)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
