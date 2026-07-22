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
import { Download01Icon, InvoiceIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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

import { InvoiceApiError } from '../api'
import { canDownloadInvoice, getInvoiceErrorMessageKey } from '../contract'
import { invoiceQueryKeys } from '../queries'
import type { InvoiceApi, InvoiceApplicationSummary } from '../types'
import { InvoiceStatusBadges } from './status-badges'

interface HistoryPanelProps {
  invoiceApi: InvoiceApi
  applications: InvoiceApplicationSummary[] | undefined
  loading: boolean
  error: boolean
}

function formatCny(minor: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'CNY',
  }).format(minor / 100)
}

/**
 * Renders invoice application history and its available actions.
 *
 * @param props - Application records, loading state, and API dependency.
 * @returns Status cards with cancellation and guarded download actions.
 */
export function HistoryPanel(props: HistoryPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const cancelMutation = useMutation({
    mutationFn: (applicationId: number) =>
      props.invoiceApi.cancelApplication(applicationId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: invoiceQueryKeys.applications(1, 50),
        }),
        queryClient.invalidateQueries({
          queryKey: invoiceQueryKeys.eligibleOrders(1, 100),
        }),
      ])
      toast.success(t('Invoice application cancelled'))
    },
    onError: (error) => {
      if (error instanceof InvoiceApiError) {
        toast.error(t(getInvoiceErrorMessageKey(error.code)))
        return
      }
      toast.error(t('Invoice error: service unavailable'))
    },
  })

  if (props.loading) {
    return (
      <div className='space-y-3'>
        <Skeleton className='h-36 w-full' />
        <Skeleton className='h-36 w-full' />
      </div>
    )
  }

  if (props.error) {
    return (
      <Alert variant='destructive'>
        <AlertTitle>{t('Invoice history failed to load')}</AlertTitle>
        <AlertDescription>{t('Please refresh and try again')}</AlertDescription>
      </Alert>
    )
  }

  const applications = props.applications || []
  if (applications.length === 0) {
    return (
      <Empty className='border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={InvoiceIcon} strokeWidth={2} />
          </EmptyMedia>
          <EmptyTitle>{t('No invoice applications')}</EmptyTitle>
          <EmptyDescription>
            {t('Submitted invoice applications will appear here.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='grid gap-4'>
      {applications.map((application) => {
        const downloadable = canDownloadInvoice(
          application,
          Math.floor(Date.now() / 1000)
        )
        return (
          <Card key={application.id}>
            <CardHeader>
              <CardTitle>{application.application_no}</CardTitle>
              <CardDescription>
                {t('Submitted at')}{' '}
                {new Date(application.submitted_at * 1000).toLocaleString()}
              </CardDescription>
            </CardHeader>
            <CardContent className='space-y-3'>
              <InvoiceStatusBadges application={application} />
              <dl className='grid gap-2 text-sm sm:grid-cols-3'>
                <div>
                  <dt className='text-muted-foreground'>
                    {t('Invoice amount')}
                  </dt>
                  <dd className='font-medium'>
                    {formatCny(application.amount_minor)}
                  </dd>
                </div>
                <div>
                  <dt className='text-muted-foreground'>
                    {t('Wallet quota fee')}
                  </dt>
                  <dd>{application.fee_quota}</dd>
                </div>
                <div>
                  <dt className='text-muted-foreground'>
                    {t('Document expires at')}
                  </dt>
                  <dd>
                    {application.document_expires_at
                      ? new Date(
                          application.document_expires_at * 1000
                        ).toLocaleString()
                      : t('Not available')}
                  </dd>
                </div>
              </dl>
              {application.reject_reason ? (
                <Alert variant='destructive'>
                  <AlertTitle>{t('Rejection reason')}</AlertTitle>
                  <AlertDescription>
                    {application.reject_reason}
                  </AlertDescription>
                </Alert>
              ) : null}
              {!downloadable && application.status === 'issued' ? (
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Download is unavailable while payment review, document status, or expiry blocks access.'
                  )}
                </p>
              ) : null}
            </CardContent>
            <CardFooter className='justify-end gap-2'>
              {application.can_cancel ? (
                <Button
                  variant='outline'
                  disabled={cancelMutation.isPending}
                  onClick={() => cancelMutation.mutate(application.id)}
                >
                  {t('Cancel application')}
                </Button>
              ) : null}
              {downloadable ? (
                <Button
                  render={
                    <a
                      href={props.invoiceApi.getDocumentDownloadUrl(
                        application.id
                      )}
                    />
                  }
                  nativeButton={false}
                >
                  <HugeiconsIcon
                    icon={Download01Icon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                  {t('Download PDF')}
                </Button>
              ) : null}
            </CardFooter>
          </Card>
        )
      })}
    </div>
  )
}
