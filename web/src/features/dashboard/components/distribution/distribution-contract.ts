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
