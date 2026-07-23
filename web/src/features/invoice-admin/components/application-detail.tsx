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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'

import { InvoiceApiError } from '../../invoices/api'
import { InvoiceStatusBadges } from '../../invoices/components/status-badges'
import {
  DOCUMENT_STATUS_CONFIG,
  getInvoiceErrorMessageKey,
} from '../../invoices/contract'
import {
  PROTECTED_INVOICE_VALUE_KEY,
  maskInvoiceSensitiveDetail,
} from '../contract'
import { adminInvoiceQueryKeys } from '../queries'
import type { AdminInvoiceApi, InvoiceApplicationDetail } from '../types'
import { DocumentUpload } from './document-upload'
import { ReviewActions, type ReviewAction } from './review-actions'

interface ApplicationDetailProps {
  applicationId: number | null
  invoiceApi: AdminInvoiceApi
  canReadSensitive: boolean
  canUploadDocument: boolean
  onClose: () => void
}

function errorCode(error: unknown) {
  return error instanceof InvoiceApiError
    ? error.code
    : ('INVOICE_INTERNAL_ERROR' as const)
}

/** Renders immutable invoice facts and permission-gated administrator actions. */
export function ApplicationDetail(props: ApplicationDetailProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const applicationId = props.applicationId
  const detailQuery = useQuery({
    queryKey: adminInvoiceQueryKeys.detail(applicationId ?? 0),
    queryFn: () => props.invoiceApi.getApplication(applicationId ?? 0),
    enabled: applicationId !== null,
  })

  async function converge(detail?: InvoiceApplicationDetail): Promise<void> {
    if (applicationId === null) return
    if (detail) {
      queryClient.setQueryData(
        adminInvoiceQueryKeys.detail(applicationId),
        detail
      )
    }
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: adminInvoiceQueryKeys.lists(),
      }),
      queryClient.invalidateQueries({
        queryKey: adminInvoiceQueryKeys.detail(applicationId),
      }),
    ])
  }

  async function mutationFailed(error: unknown): Promise<void> {
    if (errorCode(error) === 'INVOICE_STATE_CONFLICT') await converge()
    toast.error(t(getInvoiceErrorMessageKey(errorCode(error))))
  }

  const reviewMutation = useMutation({
    mutationFn: (input: {
      action: 'reviewing' | 'approve'
      detail: InvoiceApplicationDetail
    }) =>
      props.invoiceApi.reviewApplication(input.detail.id, {
        action: input.action,
        expected_status: input.detail.status as 'submitted' | 'reviewing',
      }),
    onSuccess: async (detail) => {
      await converge(detail)
      toast.success(t('Invoice review updated'))
    },
    onError: mutationFailed,
  })
  const rejectMutation = useMutation({
    mutationFn: (input: { reason: string; detail: InvoiceApplicationDetail }) =>
      props.invoiceApi.rejectApplication(input.detail.id, {
        expected_status: input.detail.status as 'submitted' | 'reviewing',
        reason: input.reason,
      }),
    onSuccess: async (detail) => {
      await converge(detail)
      toast.success(t('Invoice rejected'))
    },
    onError: mutationFailed,
  })
  const uploadMutation = useMutation({
    mutationFn: (input: { id: number; document: FormData }) =>
      props.invoiceApi.uploadDocument(input.id, input.document),
    onSuccess: async (detail) => {
      await converge(detail)
      toast.success(t('Invoice PDF saved'))
    },
    onError: mutationFailed,
  })
  let pendingAction: ReviewAction = null
  if (reviewMutation.isPending) pendingAction = reviewMutation.variables.action
  if (rejectMutation.isPending) pendingAction = 'reject'
  const busy = pendingAction !== null || uploadMutation.isPending
  const detail = detailQuery.data
    ? maskInvoiceSensitiveDetail(detailQuery.data, props.canReadSensitive)
    : undefined
  const uploadEligible =
    detail !== undefined &&
    ((detail.status === 'approved' && detail.document === null) ||
      (detail.status === 'issued' &&
        detail.issuance !== null &&
        detail.document !== null)) &&
    (detail.payment_review_status === 'none' ||
      detail.payment_review_status === 'resolved_valid')

  function protectedValue(value: string): string {
    return value === PROTECTED_INVOICE_VALUE_KEY ? t(value) : value
  }

  return (
    <Sheet
      open={applicationId !== null}
      onOpenChange={(open) => {
        if (!open && !busy) props.onClose()
      }}
    >
      <SheetContent
        className='w-full max-w-full sm:max-w-2xl'
        showCloseButton={!busy}
      >
        <SheetHeader>
          <SheetTitle>{t('Invoice application details')}</SheetTitle>
          <SheetDescription>
            {detail?.application_no ?? t('Loading invoice application')}
          </SheetDescription>
        </SheetHeader>
        <div className='min-h-0 flex-1 space-y-5 overflow-y-auto px-4 pb-6'>
          {detailQuery.isLoading ? (
            <div className='space-y-3'>
              <Skeleton className='h-20 w-full' />
              <Skeleton className='h-40 w-full' />
            </div>
          ) : null}
          {detailQuery.isError ? (
            <Alert variant='destructive'>
              <AlertTitle>{t('Unable to load invoice details')}</AlertTitle>
              <AlertDescription className='space-y-2'>
                <p>
                  {t(getInvoiceErrorMessageKey(errorCode(detailQuery.error)))}
                </p>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => detailQuery.refetch()}
                >
                  {t('Retry')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}
          {!detailQuery.isLoading && !detailQuery.isError && detail ? (
            <>
              <InvoiceStatusBadges application={detail} />
              <section aria-labelledby='invoice-profile-heading'>
                <h3 id='invoice-profile-heading' className='mb-2 font-medium'>
                  {t('Profile snapshot')}
                </h3>
                <dl className='grid min-w-0 gap-3 rounded-lg border p-3 sm:grid-cols-2'>
                  <div className='min-w-0'>
                    <dt className='text-muted-foreground'>
                      {t('Invoice title')}
                    </dt>
                    <dd className='break-words'>
                      {protectedValue(detail.profile_snapshot.title)}
                    </dd>
                  </div>
                  <div className='min-w-0'>
                    <dt className='text-muted-foreground'>
                      {t('Tax identifier')}
                    </dt>
                    <dd className='break-all'>
                      {protectedValue(detail.profile_snapshot.tax_number)}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Invoice type')}
                    </dt>
                    <dd>
                      {t(
                        detail.profile_snapshot.type === 'company'
                          ? 'Company'
                          : 'Personal'
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Snapshot version')}
                    </dt>
                    <dd>{detail.profile_snapshot.version}</dd>
                  </div>
                </dl>
              </section>

              <section aria-labelledby='invoice-orders-heading'>
                <h3 id='invoice-orders-heading' className='mb-2 font-medium'>
                  {t('Invoice items')}
                </h3>
                <ul className='space-y-2'>
                  {detail.items.map((item) => (
                    <li
                      key={item.topup_id}
                      className='min-w-0 rounded-lg border p-3'
                    >
                      <p className='font-medium break-all'>{item.order_no}</p>
                      <p className='text-muted-foreground break-words'>
                        {item.product_description}
                      </p>
                      <p>
                        {new Intl.NumberFormat(undefined, {
                          style: 'currency',
                          currency: 'CNY',
                        }).format(item.paid_amount_minor / 100)}
                      </p>
                    </li>
                  ))}
                </ul>
              </section>

              {detail.issuance ? (
                <section aria-labelledby='invoice-issuance-heading'>
                  <h3
                    id='invoice-issuance-heading'
                    className='mb-2 font-medium'
                  >
                    {t('Issuance facts')}
                  </h3>
                  <dl className='grid gap-3 rounded-lg border p-3 sm:grid-cols-2'>
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('Invoice number')}
                      </dt>
                      <dd className='break-all'>
                        {detail.issuance.invoice_number}
                      </dd>
                    </div>
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('Invoice code')}
                      </dt>
                      <dd className='break-all'>
                        {detail.issuance.invoice_code || t('Not provided')}
                      </dd>
                    </div>
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('Invoice date')}
                      </dt>
                      <dd>
                        {dayjs
                          .unix(detail.issuance.invoice_date)
                          .format('YYYY-MM-DD')}
                      </dd>
                    </div>
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('Document status')}
                      </dt>
                      <dd>
                        {t(
                          DOCUMENT_STATUS_CONFIG[detail.document_status]
                            .labelKey
                        )}
                      </dd>
                    </div>
                  </dl>
                </section>
              ) : null}

              <Separator />
              <ReviewActions
                application={detail}
                pendingAction={pendingAction}
                onReview={(action) => reviewMutation.mutate({ action, detail })}
                onReject={(reason) =>
                  rejectMutation
                    .mutateAsync({ reason, detail })
                    .then(() => undefined)
                }
              />

              {props.canUploadDocument && uploadEligible ? (
                <>
                  <Separator />
                  <DocumentUpload
                    key={`${detail.status}-${detail.document?.id ?? 'initial'}`}
                    application={detail}
                    pending={uploadMutation.isPending}
                    onUpload={(document) =>
                      uploadMutation.mutate({ id: detail.id, document })
                    }
                  />
                </>
              ) : null}
            </>
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}
