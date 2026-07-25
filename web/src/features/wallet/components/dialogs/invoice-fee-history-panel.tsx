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
import { useQuery } from '@tanstack/react-query'
import type { ColumnDef, PaginationState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { invoiceFeeLedgerApi } from '../../../invoices/production-api'
import type { InvoiceFeeLedgerEntry } from '../../../invoices/types'
import { createInvoiceFeeEntryPresentation } from '../../lib/invoice-fee-history'

const PAGE_SIZE = 10

interface InvoiceFeeHistoryPanelProps {
  enabled: boolean
  paginationInFooter?: boolean
}

/** Shows owner-scoped invoice fee transitions only after its tab is selected. */
export function InvoiceFeeHistoryPanel(props: InvoiceFeeHistoryPanelProps) {
  const { t } = useTranslation()
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: PAGE_SIZE,
  })
  const feeQuery = useQuery({
    queryKey: [
      'wallet',
      'invoice-fees',
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: () =>
      invoiceFeeLedgerApi.list({
        page: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      }),
    enabled: props.enabled,
    placeholderData: (previousData) => previousData,
  })
  const entries = feeQuery.data?.items ?? []
  const total = feeQuery.data?.total ?? 0
  const columns = useMemo<ColumnDef<InvoiceFeeLedgerEntry>[]>(
    () => [
      {
        accessorKey: 'applied_at',
        header: t('Time'),
        cell: ({ row }) => (
          <span className='font-mono text-xs tabular-nums'>
            {createInvoiceFeeEntryPresentation(row.original, t).appliedAt}
          </span>
        ),
        size: 160,
        meta: { mobileOrder: 1 },
      },
      {
        accessorKey: 'application_no',
        header: t('Application number'),
        cell: ({ row }) => (
          <span className='font-mono text-xs font-medium'>
            {row.original.application_no}
          </span>
        ),
        size: 180,
        meta: { mobileTitle: true },
      },
      {
        accessorKey: 'entry_type',
        header: t('Type'),
        cell: ({ row }) => {
          const display = createInvoiceFeeEntryPresentation(row.original, t)
          return (
            <StatusBadge
              label={display.entryType}
              variant={
                row.original.entry_type === 'charge' ? 'warning' : 'success'
              }
              copyable={false}
            />
          )
        },
        size: 130,
        meta: { mobileBadge: true },
      },
      {
        accessorKey: 'fee_percent',
        header: t('Fee percentage'),
        cell: ({ row }) =>
          createInvoiceFeeEntryPresentation(row.original, t).feePercent,
        size: 110,
        meta: { mobileOrder: 2 },
      },
      {
        accessorKey: 'quota',
        header: t('Quota amount'),
        cell: ({ row }) => (
          <span
            className={cn(
              'font-mono font-medium tabular-nums',
              row.original.entry_type === 'charge' && 'text-destructive'
            )}
          >
            {createInvoiceFeeEntryPresentation(row.original, t).quota}
          </span>
        ),
        size: 120,
        meta: { mobileOrder: 3 },
      },
      {
        accessorKey: 'balance_before',
        header: t('Balance before'),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {createInvoiceFeeEntryPresentation(row.original, t).balanceBefore}
          </span>
        ),
        size: 130,
        meta: { mobileOrder: 4 },
      },
      {
        accessorKey: 'balance_after',
        header: t('Balance after'),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {createInvoiceFeeEntryPresentation(row.original, t).balanceAfter}
          </span>
        ),
        size: 130,
        meta: { mobileOrder: 5 },
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        cell: ({ row }) => {
          const display = createInvoiceFeeEntryPresentation(row.original, t)
          return (
            <StatusBadge
              label={display.status}
              variant={
                row.original.status === 'applied' ? 'success' : 'warning'
              }
              copyable={false}
            />
          )
        },
        size: 110,
        meta: { mobileOrder: 6 },
      },
    ],
    [t]
  )
  const { table } = useDataTable({
    data: entries,
    columns,
    pagination,
    onPaginationChange: setPagination,
    manualPagination: true,
    totalCount: total,
    enableRowSelection: false,
  })
  const isError = feeQuery.isError

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={feeQuery.isLoading}
      isFetching={feeQuery.isFetching}
      emptyTitle={
        isError
          ? t('Unable to load invoice fees')
          : t('No invoice fee records found')
      }
      emptyDescription={
        isError
          ? t('Invoice fee history could not be loaded. Try again.')
          : undefined
      }
      emptyAction={
        isError ? (
          <Button
            variant='outline'
            size='sm'
            onClick={() => feeQuery.refetch()}
          >
            {t('Retry')}
          </Button>
        ) : undefined
      }
      toolbarProps={null}
      paginationInFooter={props.paginationInFooter ?? false}
      fixedHeight={props.paginationInFooter ?? false}
      applyHeaderSize
      skeletonKeyPrefix='invoice-fee-log-skeleton'
      tableClassName='[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_th]:text-[13px]'
      getColumnClassName={(_, kind) => (kind === 'cell' ? 'py-2' : undefined)}
      getRowClassName={(row) =>
        row.original.entry_type === 'charge' ? 'bg-destructive/5' : undefined
      }
      mobileProps={{ getRowKey: (row) => row.original.id }}
    />
  )
}
