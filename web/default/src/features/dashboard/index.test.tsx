import { describe, expect, it } from 'bun:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React, { type ReactNode } from 'react'
import { renderToReadableStream } from 'react-dom/server.browser'

import { ROLE } from '@/lib/roles'

import { DISTRIBUTION_QUERY_KEY } from './components/distribution/distribution-contract'

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

mockModule('@tanstack/react-router', () => ({
  Link: (props: { children?: ReactNode }) => React.createElement('a', props),
  Outlet: () => null,
  createFileRoute: () => () => ({}),
  getRouteApi: () => ({
    useParams: () => ({ section: 'distribution' }),
  }),
  redirect: () => ({}),
  useBlocker: () => undefined,
  useLocation: () => ({ pathname: '/dashboard/distribution' }),
  useNavigate: () => () => undefined,
  useParams: () => ({ section: 'distribution' }),
  useRouter: () => ({ history: { go: () => undefined } }),
  useRouterState: () => ({ location: { pathname: '/dashboard/distribution' } }),
  useSearch: () => ({}),
}))

mockModule('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

mockModule('@/lib/time', () => ({
  addTimeToDate: () => new Date(END_TIMESTAMP * 1000),
  computeTimeRange: () => ({
    start_timestamp: START_TIMESTAMP,
    end_timestamp: END_TIMESTAMP,
  }),
  dateToUnixTimestamp: (date: Date) => Math.floor(date.getTime() / 1000),
  formatChartTime: (timestamp: number) => String(timestamp),
  formatDateTimeObject: (date: Date) => date.toISOString(),
  getRollingDateRange: () => ({
    start: new Date(START_TIMESTAMP * 1000),
    end: new Date(END_TIMESTAMP * 1000),
  }),
}))

mockModule('@/lib/use-chart-theme', () => ({
  useChartTheme: () => ({ resolvedTheme: 'light', themeReady: true }),
}))

mockModule('@/components/layout', () => {
  function SectionPageLayout(props: { children?: ReactNode }) {
    return React.createElement('main', null, props.children)
  }
  SectionPageLayout.Title = (props: { children?: ReactNode }) =>
    React.createElement('title', null, props.children)
  SectionPageLayout.Content = (props: { children?: ReactNode }) =>
    React.createElement('section', null, props.children)

  return { SectionPageLayout }
})

mockModule('@/components/page-transition', () => ({
  AnimatedOutlet: () => null,
  CardStaggerContainer: (props: { children?: ReactNode }) =>
    React.createElement('div', null, props.children),
  CardStaggerItem: (props: { children?: ReactNode }) =>
    React.createElement('div', null, props.children),
  FadeIn: (props: { children?: ReactNode }) =>
    React.createElement('div', null, props.children),
  PageTransition: (props: { children?: ReactNode }) =>
    React.createElement('div', null, props.children),
  StaggerContainer: (props: { children?: ReactNode }) =>
    React.createElement('div', null, props.children),
  StaggerItem: (props: { children?: ReactNode }) =>
    React.createElement('div', null, props.children),
  TableStaggerContainer: (props: { children?: ReactNode }) =>
    React.createElement('div', null, props.children),
  TableStaggerRow: (props: { children?: ReactNode }) =>
    React.createElement('div', null, props.children),
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
            : { id: 1, username: 'admin', role: currentRole },
      },
    }),
}))

mockModule('@visactor/react-vchart', () => ({
  VChart: () => React.createElement('div', { 'data-testid': 'vchart' }),
}))

mockModule('@/features/dashboard/api', () => ({
  getDistributionData: async () => ({
    success: true,
    data: [
      {
        dimension: 'user',
        id: '1',
        label: 'dashboard-route-user',
        quota: 500_000,
        tokens: 1_000,
        requests: 10,
      },
    ],
  }),
  getFlowQuotaDates: async () => ({ success: true, data: [] }),
  getSelfQuota: async () => ({ success: true, data: { quota: 0 } }),
  getUptimeStatus: async () => ({ success: true, data: [] }),
  getUserQuotaDataByUsers: async () => ({ success: true, data: [] }),
  getUserQuotaDates: async () => ({ success: true, data: [] }),
}))

async function renderHtml(children: ReactNode) {
  const stream = await renderToReadableStream(children)
  await stream.allReady
  return new Response(stream).text()
}

describe('Dashboard distribution route rendering', () => {
  it('renders DistributionSection for the distribution section id', async () => {
    const { Dashboard } = await import('./index')
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(
      [
        ...DISTRIBUTION_QUERY_KEY,
        START_TIMESTAMP,
        END_TIMESTAMP,
        'quota',
        'user',
        '',
      ],
      [
        {
          dimension: 'user',
          id: '1',
          label: 'dashboard-route-user',
          quota: 500_000,
          tokens: 1_000,
          requests: 10,
        },
      ],
      { updatedAt: 1 }
    )
    currentRole = ROLE.ADMIN

    try {
      const html = await renderHtml(
        <QueryClientProvider client={queryClient}>
          <Dashboard />
        </QueryClientProvider>
      )

      expect(html).toContain('<title>Distribution</title>')
      expect(html).toContain('Quota Distribution')
      expect(html).toContain('dashboard-route-user')
    } finally {
      queryClient.clear()
      currentRole = undefined
    }
  })
})
