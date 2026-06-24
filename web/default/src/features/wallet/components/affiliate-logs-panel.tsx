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
import { ChevronLeft, ChevronRight, RotateCw } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { useAffiliateLogs } from '../hooks'
import type { AffiliateLogItem, AffiliateLogType } from '../types'
import { createAffiliateLogColumns } from './affiliate-logs-panel.columns'

const AFFILIATE_LOG_TYPES: AffiliateLogType[] = [
  'invite_reward',
  'topup_rebate',
]

export function AffiliateLogsPanel() {
  const { t } = useTranslation()
  const {
    type,
    items,
    total,
    page,
    pageSize,
    totalPages,
    loading,
    error,
    setType,
    setPage,
    refresh,
  } = useAffiliateLogs({ pageSize: 5 })

  const columns = useMemo(() => createAffiliateLogColumns(t, type), [t, type])

  const rangeStart = total === 0 ? 0 : (page - 1) * pageSize + 1
  const rangeEnd = Math.min(page * pageSize, total)

  return (
    <div className='border-t px-3 py-3 sm:px-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div>
          <h4 className='text-sm font-semibold'>{t('Affiliate logs')}</h4>
          <p className='text-muted-foreground text-xs'>
            {t('Recent invite rewards and wallet top-up rebates.')}
          </p>
        </div>
        <Tabs
          value={type}
          onValueChange={(value) => setType(value as AffiliateLogType)}
        >
          <TabsList className='grid w-full grid-cols-2 sm:w-fit'>
            {AFFILIATE_LOG_TYPES.map((logType) => (
              <TabsTrigger key={logType} value={logType}>
                {logType === 'invite_reward'
                  ? t('Invite logs')
                  : t('Rebate logs')}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <div className='mt-3'>
        <AffiliateLogsContent
          columns={columns}
          error={error}
          items={items}
          loading={loading}
          onRetry={refresh}
          type={type}
        />
      </div>

      {!loading && !error && total > 0 ? (
        <div className='mt-3 flex flex-col items-center gap-2 sm:flex-row sm:justify-between'>
          <div className='text-muted-foreground text-xs'>
            {t('Showing {{start}}-{{end}} of {{total}}', {
              start: rangeStart,
              end: rangeEnd,
              total,
            })}
          </div>
          <div className='flex items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setPage(Math.max(1, page - 1))}
              disabled={page <= 1}
              className='h-8 w-8 p-0'
              aria-label={t('Previous page')}
            >
              <ChevronLeft className='size-4' />
            </Button>
            <span className='text-muted-foreground min-w-12 text-center text-xs tabular-nums'>
              {page} / {totalPages}
            </span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setPage(Math.min(totalPages, page + 1))}
              disabled={page >= totalPages}
              className='h-8 w-8 p-0'
              aria-label={t('Next page')}
            >
              <ChevronRight className='size-4' />
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  )
}

interface AffiliateLogsContentProps {
  columns: StaticDataTableColumn<AffiliateLogItem>[]
  error: string | null
  items: AffiliateLogItem[]
  loading: boolean
  onRetry: () => void
  type: AffiliateLogType
}

function AffiliateLogsContent({
  columns,
  error,
  items,
  loading,
  onRetry,
  type,
}: AffiliateLogsContentProps) {
  const { t } = useTranslation()

  if (loading) {
    return <AffiliateLogsSkeleton />
  }

  if (error) {
    return (
      <div className='text-muted-foreground flex min-h-28 flex-col items-center justify-center rounded-lg border border-dashed p-4 text-center'>
        <p className='text-sm font-medium'>{error}</p>
        <Button
          type='button'
          variant='outline'
          size='sm'
          className='mt-3 h-8 gap-1.5'
          onClick={onRetry}
        >
          <RotateCw className='size-3.5' />
          {t('Reload logs')}
        </Button>
      </div>
    )
  }

  return (
    <StaticDataTable
      columns={columns}
      data={items}
      getRowKey={(log) => log.id}
      tableClassName='text-xs'
      emptyContent={
        <div className='text-muted-foreground py-4 text-center text-sm'>
          {type === 'invite_reward'
            ? t('No invite logs yet')
            : t('No rebate logs yet')}
        </div>
      }
    />
  )
}

function AffiliateLogsSkeleton() {
  const rows = ['first', 'second', 'third', 'fourth']

  return (
    <div className='space-y-2 rounded-lg border p-3'>
      {rows.map((row) => (
        <div
          key={row}
          className='grid grid-cols-[0.75fr_1fr_0.8fr] items-center gap-3'
        >
          <Skeleton className='h-4' />
          <Skeleton className='h-4' />
          <Skeleton className='h-4' />
        </div>
      ))}
    </div>
  )
}
