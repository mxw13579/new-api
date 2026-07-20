import type {
  DistributionDimension,
  DistributionMetric,
} from '@/features/dashboard/lib/distribution'
import { ROLE } from '@/lib/roles'

export const DISTRIBUTION_QUERY_KEY = ['dashboard', 'distribution'] as const

export const RANGE_OPTIONS = [
  { days: 1, labelKey: 'Last 24 hours' },
  { days: 7, labelKey: 'Last 7 days' },
  { days: 30, labelKey: 'Last 30 days' },
] as const

export const METRIC_OPTIONS: {
  value: DistributionMetric
  labelKey: string
}[] = [
  { value: 'quota', labelKey: 'Quota' },
  { value: 'tokens', labelKey: 'Tokens' },
  { value: 'requests', labelKey: 'Requests' },
]

const DIMENSION_OPTIONS: {
  value: DistributionDimension
  labelKey: string
}[] = [
  { value: 'user', labelKey: 'User' },
  { value: 'key', labelKey: 'API Keys' },
  { value: 'group', labelKey: 'Group' },
]

export const TABLE_HEADERS = [
  'Rank',
  'Contributor',
  'Value',
  'Share',
  'Quota',
  'Tokens',
  'Requests',
] as const

export const DISTRIBUTION_A11Y_CONTRACT = {
  tableCaptionKey: 'Top contributors',
  tableHeaders: TABLE_HEADERS,
  liveRegion: 'polite',
} as const

type DistributionLayout = 'mobile' | 'desktop'

export function getDistributionLayoutOrder(layout: DistributionLayout) {
  if (layout === 'mobile') {
    return ['controls', 'summary', 'chart', 'table'] as const
  }

  return ['controls', 'summary', 'chart-table'] as const
}

export function getAllowedDistributionDimensions(role?: number) {
  if (role != null && role >= ROLE.SUPER_ADMIN) {
    return DIMENSION_OPTIONS
  }

  if (role != null && role >= ROLE.ADMIN) {
    return DIMENSION_OPTIONS.filter((option) => option.value !== 'key')
  }

  return DIMENSION_OPTIONS.filter((option) => option.value !== 'user')
}
