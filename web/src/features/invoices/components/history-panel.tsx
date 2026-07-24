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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
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
import { Spinner } from '@/components/ui/spinner'

import { InvoiceApiError } from '../api'
import {
  canDownloadInvoice,
  formatInvoiceAmount,
  getInvoiceErrorMessageKey,
} from '../contract'
import type {
  InvoiceApi,
  InvoiceApplicationSummary,
  InvoicePage,
} from '../types'
import {
  invalidateUserInvoiceMutationQueries,
  invoicePageCount,
} from '../user-workspace'
import { ApplicationDetail } from './application-detail'
import { InvoiceStatusBadges } from './status-badges'

interface HistoryPanelProps {
  invoiceApi: InvoiceApi
  applicationsPage: InvoicePage<InvoiceApplicationSummary> | undefined
  loading: boolean
  error: boolean
  retry: () => void
  onPageChange: (page: number) => void
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
  const [nowSeconds, setNowSeconds] = useState(() =>
    Math.floor(Date.now() / 1000)
  )
  const [cancelTarget, setCancelTarget] =
    useState<InvoiceApplicationSummary | null>(null)
  const [detailId, setDetailId] = useState<number | null>(null)
  const [downloadPendingId, setDownloadPendingId] = useState<number | null>(
    null
  )
  useEffect(() => {
    const interval = window.setInterval(
      () => setNowSeconds(Math.floor(Date.now() / 1000)),
      30_000
    )
    return () => window.clearInterval(interval)
  }, [])
  const cancelMutation = useMutation({
    mutationFn: (applicationId: number) =>
      props.invoiceApi.cancelApplication(applicationId),
    onSuccess: async (application) => {
      await invalidateUserInvoiceMutationQueries(queryClient, application.id)
      setCancelTarget(null)
      toast.success(t('Invoice application cancelled'))
    },
    onError: async (error, applicationId) => {
      if (error instanceof InvoiceApiError) {
        if (error.code === 'INVOICE_STATE_CONFLICT') {
          await invalidateUserInvoiceMutationQueries(queryClient, applicationId)
        }
        toast.error(t(getInvoiceErrorMessageKey(error.code)))
        return
      }
      toast.error(t('Invoice error: service unavailable'))
    },
  })
  async function downloadDocument(applicationId: number): Promise<void> {
    setDownloadPendingId(applicationId)
    try {
      if (!props.invoiceApi.requestDocumentDownloadUrl) {
        throw new Error('authenticated-download-unavailable')
      }
      const downloadUrl =
        await props.invoiceApi.requestDocumentDownloadUrl(applicationId)
      window.location.assign(downloadUrl)
    } catch (error) {
      if (error instanceof InvoiceApiError) {
        toast.error(t(getInvoiceErrorMessageKey(error.code)))
      } else {
        toast.error(t('Invoice error: service unavailable'))
      }
    } finally {
      setDownloadPendingId(null)
    }
  }

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
        <AlertDescription>
          <Button variant='outline' size='sm' onClick={props.retry}>
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  }

  const applications = props.applicationsPage?.items || []
  const applicationsPage = props.applicationsPage
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
      <div className='flex items-center justify-between gap-3'>
        <h2 className='text-lg font-semibold'>{t('Invoice history')}</h2>
        <Button variant='outline' size='sm' onClick={props.retry}>
          {t('Refresh')}
        </Button>
      </div>
      {applications.map((application) => {
        const downloadable = canDownloadInvoice(application, nowSeconds)
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
                    {formatInvoiceAmount(application.amount_minor)}
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
              <Button
                variant='outline'
                onClick={() => setDetailId(application.id)}
              >
                {t('Details')}
              </Button>
              {application.can_cancel ? (
                <Button
                  variant='outline'
                  disabled={
                    cancelMutation.isPending &&
                    cancelMutation.variables === application.id
                  }
                  onClick={() => setCancelTarget(application)}
                >
                  {t('Cancel application')}
                </Button>
              ) : null}
              {downloadable ? (
                <Button
                  disabled={downloadPendingId === application.id}
                  onClick={() => downloadDocument(application.id)}
                >
                  {downloadPendingId === application.id ? (
                    <Spinner data-icon='inline-start' />
                  ) : (
                    <HugeiconsIcon
                      icon={Download01Icon}
                      strokeWidth={2}
                      data-icon='inline-start'
                    />
                  )}
                  {t('Download PDF')}
                </Button>
              ) : null}
            </CardFooter>
          </Card>
        )
      })}
      {applicationsPage ? (
        <div className='flex items-center justify-end gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={applicationsPage.page <= 1}
            onClick={() => props.onPageChange(applicationsPage.page - 1)}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground text-sm'>
            {t('Page {{page}} of {{pages}}', {
              page: applicationsPage.page,
              pages: invoicePageCount(
                applicationsPage.total,
                applicationsPage.page_size
              ),
            })}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={
              applicationsPage.page >=
              invoicePageCount(
                applicationsPage.total,
                applicationsPage.page_size
              )
            }
            onClick={() => props.onPageChange(applicationsPage.page + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      ) : null}
      <ApplicationDetail
        invoiceApi={props.invoiceApi}
        applicationId={detailId}
        onOpenChange={(open) => {
          if (!open) setDetailId(null)
        }}
      />
      <AlertDialog
        open={cancelTarget !== null}
        onOpenChange={(open) => {
          if (!open && !cancelMutation.isPending) setCancelTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Cancel invoice application?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'The selected paid orders will become eligible again after cancellation.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cancelMutation.isPending}>
              {t('Keep application')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={cancelMutation.isPending}
              onClick={() => {
                if (cancelTarget) cancelMutation.mutate(cancelTarget.id)
              }}
            >
              {cancelMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : null}
              {t('Cancel application')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
