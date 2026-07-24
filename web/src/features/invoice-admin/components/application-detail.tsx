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
import { getInvoiceErrorMessageKey } from '../../invoices/contract'
import { maskInvoiceSensitiveDetail } from '../contract'
import { adminInvoiceQueryKeys } from '../queries'
import type { AdminInvoiceApi, InvoiceApplicationDetail } from '../types'
import {
  ApplicationIssuanceSection,
  ApplicationItemsSection,
  ApplicationProfileSection,
} from './application-detail-sections'
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
              <ApplicationProfileSection application={detail} />
              <ApplicationItemsSection application={detail} />
              <ApplicationIssuanceSection application={detail} />

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
