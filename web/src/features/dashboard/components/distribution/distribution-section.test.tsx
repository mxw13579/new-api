import { describe, expect, it } from 'bun:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React, { type ReactNode } from 'react'
import { renderToReadableStream } from 'react-dom/server.browser'

import { ROLE } from '@/lib/roles'

import {
  DISTRIBUTION_A11Y_CONTRACT,
  DISTRIBUTION_QUERY_KEY,
  getAllowedDistributionDimensions,
  getDistributionLayoutOrder,
} from './distribution-contract'

const START_TIMESTAMP = 1_800_000_000
const END_TIMESTAMP = 1_800_604_800
let currentRole: number | undefined
type MockModule = (
  specifier: string,
  factory: () => Record<string, unknown>
) => void
const mockModule = (
  (await import('bun:test')) as unknown as { mock: { module: MockModule } }
).mock.module

mockModule('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: Record<string, string>) => {
      let text = key
      for (const [name, value] of Object.entries(options ?? {})) {
        text = text.replace(`{{${name}}}`, value)
      }
      return text
    },
  }),
}))

mockModule('@/lib/time', () => ({
  dateToUnixTimestamp: (date: Date) => Math.floor(date.getTime() / 1000),
  getRollingDateRange: () => ({
    start: new Date(START_TIMESTAMP * 1000),
    end: new Date(END_TIMESTAMP * 1000),
  }),
}))

mockModule('@/lib/use-chart-theme', () => ({
  useChartTheme: () => ({ resolvedTheme: 'light', themeReady: true }),
}))

mockModule('@/stores/auth-store', () => ({
  useAuthStore: (
    selector: (state: {
      auth: { user: { id: number; username: string; role: number } | null }
    }) => unknown
  ) =>
    selector({
      auth: {
        user:
          currentRole == null
            ? null
            : { id: 1, username: 'tester', role: currentRole },
      },
    }),
}))

mockModule('@visactor/react-vchart', () => ({
  VChart: () => React.createElement('div', { 'data-testid': 'vchart' }),
}))

mockModule('@/features/dashboard/api', () => ({
  getDistributionData: async () => ({
    success: true,
    data: distributionRowsByDimension.user,
  }),
}))

const distributionRowsByDimension = {
  key: [
    {
      dimension: 'key',
      id: '101',
      label: 'Primary key',
      quota: 500_000,
      tokens: 1_200,
      requests: 12,
    },
    {
      dimension: 'key',
      id: '102',
      label: 'Batch key',
      quota: 250_000,
      tokens: 700,
      requests: 7,
    },
  ],
  user: [
    {
      dimension: 'user',
      id: '1',
      label: 'alice',
      quota: 700_000,
      tokens: 2_000,
      requests: 20,
    },
    {
      dimension: 'user',
      id: '2',
      label: 'bob',
      quota: 300_000,
      tokens: 900,
      requests: 9,
    },
  ],
} as const

type RenderRole = typeof ROLE.USER | typeof ROLE.ADMIN | typeof ROLE.SUPER_ADMIN

async function renderHtml(children: ReactNode) {
  const stream = await renderToReadableStream(children)
  await stream.allReady
  return new Response(stream).text()
}

function queryKeyFor(role: RenderRole) {
  const dimension = role === ROLE.USER ? 'key' : 'user'

  return [
    ...DISTRIBUTION_QUERY_KEY,
    START_TIMESTAMP,
    END_TIMESTAMP,
    'quota',
    dimension,
    '',
  ] as const
}

async function renderDistributionSection(role: RenderRole) {
  const { DistributionSection } = await import('./distribution-section')
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const dimension = role === ROLE.USER ? 'key' : 'user'
  queryClient.setQueryData(
    queryKeyFor(role),
    distributionRowsByDimension[dimension],
    { updatedAt: 1 }
  )
  currentRole = role

  try {
    return await renderHtml(
      <QueryClientProvider client={queryClient}>
        <DistributionSection />
      </QueryClientProvider>
    )
  } finally {
    queryClient.clear()
    currentRole = undefined
  }
}

describe('distribution dashboard section contract', () => {
  it('uses an independent query key for the distribution endpoint', () => {
    expect(DISTRIBUTION_QUERY_KEY).toEqual(['dashboard', 'distribution'])
  })

  it('filters dimension selectors by role permission matrix', () => {
    expect(
      getAllowedDistributionDimensions(ROLE.USER).map((option) => option.value)
    ).toEqual(['key', 'group'])
    expect(
      getAllowedDistributionDimensions(ROLE.ADMIN).map((option) => option.value)
    ).toEqual(['user', 'group'])
    expect(
      getAllowedDistributionDimensions(ROLE.SUPER_ADMIN).map(
        (option) => option.value
      )
    ).toEqual(['user', 'key', 'group'])
  })

  it('documents a11y and mobile layout invariants for the section', () => {
    expect(DISTRIBUTION_A11Y_CONTRACT.tableCaptionKey).toBe('Top contributors')
    expect(DISTRIBUTION_A11Y_CONTRACT.liveRegion).toBe('polite')
    expect(DISTRIBUTION_A11Y_CONTRACT.tableHeaders).toContain('Share')
    expect(getDistributionLayoutOrder('mobile')).toEqual([
      'controls',
      'summary',
      'chart',
      'table',
    ])
    expect(getDistributionLayoutOrder('desktop')).toEqual([
      'controls',
      'summary',
      'chart-table',
    ])
  })

  it('renders real controls, chart alternative text, table caption, headers, and live region', async () => {
    const html = await renderDistributionSection(ROLE.ADMIN)

    expect(html).toContain('Quota Distribution')
    expect(html).toContain(
      'Analyze consumption share by user, API key, or group.'
    )
    expect(html).toContain('>Time range</')
    expect(html).toContain('aria-label="Last 24 hours"')
    expect(html).toContain('aria-label="Metric"')
    expect(html).toContain('aria-label="Distribution by"')
    expect(html).toContain('aria-label="Username filter"')
    expect(html).toContain('role="img"')
    expect(html).toContain('aria-label="Quota Distribution"')
    expect(html).toContain('role="status"')
    expect(html).toContain('aria-live="polite"')
    expect(html).toContain(
      '<caption class="sr-only">Top contributors</caption>'
    )
    for (const header of [
      'Rank',
      'Contributor',
      'Value',
      'Share',
      'Quota',
      'Tokens',
      'Requests',
    ]) {
      expect(html).toContain(`<th scope="col"`)
      expect(html).toContain(`>${header}</th>`)
    }
    expect(html).toContain('overflow-x-auto')
    expect(html).toContain('alice')
    expect(html).toContain('bob')
  })

  it('renders user role with key-scoped distribution and no username filter', async () => {
    const html = await renderDistributionSection(ROLE.USER)

    expect(html).toContain('Primary key')
    expect(html).toContain('Batch key')
    expect(html.includes('Username filter')).toBe(false)
  })

  it('renders admin role with user distribution and username filtering', async () => {
    const html = await renderDistributionSection(ROLE.ADMIN)

    expect(html).toContain('alice')
    expect(html).toContain('bob')
    expect(html).toContain('Username filter')
  })

  it('renders root role with user distribution and keeps root-only key dimension available', async () => {
    const html = await renderDistributionSection(ROLE.SUPER_ADMIN)

    expect(html).toContain('alice')
    expect(html).toContain('bob')
    expect(html).toContain('Username filter')
    expect(
      getAllowedDistributionDimensions(ROLE.SUPER_ADMIN).map(
        (option) => option.value
      )
    ).toContain('key')
  })
})
