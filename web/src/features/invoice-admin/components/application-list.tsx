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
import dayjs from 'dayjs'
import { FileText } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { InvoiceApiError } from '../../invoices/api'
import { InvoiceStatusBadges } from '../../invoices/components/status-badges'
import { getInvoiceErrorMessageKey } from '../../invoices/contract'
import { adminInvoiceQueryKeys } from '../queries'
import type { AdminInvoiceApi } from '../types'

const PAGE_SIZE = 20

function formatCny(minor: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'CNY',
  }).format(minor / 100)
}

interface ApplicationListProps {
  invoiceApi: AdminInvoiceApi
  onSelect: (applicationId: number) => void
}

/** Renders an administrator-visible, server-paginated invoice application list. */
export function ApplicationList(props: ApplicationListProps) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const applicationsQuery = useQuery({
    queryKey: adminInvoiceQueryKeys.list(page, PAGE_SIZE),
    queryFn: () =>
      props.invoiceApi.listApplications({ page, page_size: PAGE_SIZE }),
  })
  const totalPages = Math.max(
    1,
    Math.ceil((applicationsQuery.data?.total ?? 0) / PAGE_SIZE)
  )
  let content: React.ReactNode
  if (applicationsQuery.isLoading) {
    content = (
      <div className='space-y-3' aria-label={t('Loading invoice applications')}>
        <Skeleton className='h-12 w-full' />
        <Skeleton className='h-12 w-full' />
        <Skeleton className='h-12 w-full' />
      </div>
    )
  } else if (applicationsQuery.isError) {
    const code =
      applicationsQuery.error instanceof InvoiceApiError
        ? applicationsQuery.error.code
        : 'INVOICE_INTERNAL_ERROR'
    content = (
      <Alert variant='destructive'>
        <AlertTitle>{t('Unable to load invoice applications')}</AlertTitle>
        <AlertDescription className='space-y-2'>
          <p>{t(getInvoiceErrorMessageKey(code))}</p>
          <Button
            variant='outline'
            size='sm'
            onClick={() => applicationsQuery.refetch()}
          >
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  } else if (applicationsQuery.data?.items.length === 0) {
    content = (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <FileText aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('No invoice applications')}</EmptyTitle>
          <EmptyDescription>
            {t('New invoice applications will appear here.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    content = (
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Application number')}</TableHead>
            <TableHead>{t('Submitted')}</TableHead>
            <TableHead>{t('Amount')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead className='text-right'>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {applicationsQuery.data?.items.map((application) => (
            <TableRow key={application.id}>
              <TableCell className='font-medium'>
                {application.application_no}
              </TableCell>
              <TableCell>
                {dayjs
                  .unix(application.submitted_at)
                  .format('YYYY-MM-DD HH:mm')}
              </TableCell>
              <TableCell>{formatCny(application.amount_minor)}</TableCell>
              <TableCell>
                <InvoiceStatusBadges application={application} />
              </TableCell>
              <TableCell className='text-right'>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => props.onSelect(application.id)}
                >
                  {t('View details')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Invoice review queue')}</CardTitle>
        <CardDescription>
          {t('Review invoice applications and manage issued PDF documents.')}
        </CardDescription>
      </CardHeader>
      <CardContent>{content}</CardContent>
      <CardFooter className='flex flex-wrap justify-between gap-2'>
        <span className='text-muted-foreground' aria-live='polite'>
          {t('Page {{page}} of {{totalPages}}', { page, totalPages })}
        </span>
        <div className='flex gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1 || applicationsQuery.isFetching}
            onClick={() => setPage((current) => Math.max(1, current - 1))}
          >
            {t('Previous')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= totalPages || applicationsQuery.isFetching}
            onClick={() => setPage((current) => current + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </CardFooter>
    </Card>
  )
}
