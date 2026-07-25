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
/* oxlint-disable eslint/no-nested-ternary */
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

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
import { formatQuota } from '@/lib/format'

import { formatInvoiceAmount } from '../contract'
import {
  invoiceQueryKeys,
  redactInvoiceApplicationDetailForCache,
} from '../queries'
import type { InvoiceApi, InvoiceApplicationDetail } from '../types'
import { shouldPollInvoiceApplication } from '../user-workspace'
import { InvoiceStatusBadges } from './status-badges'

interface ApplicationDetailProps {
  invoiceApi: InvoiceApi
  applicationId: number | null
  onOpenChange: (open: boolean) => void
}

/** Displays the immutable invoice application record in a titled sheet. */
export function ApplicationDetail(props: ApplicationDetailProps) {
  const { t } = useTranslation()
  const applicationId = props.applicationId
  const [displayDetail, setDisplayDetail] = useState<
    InvoiceApplicationDetail | undefined
  >()
  const detailQuery = useQuery({
    queryKey: invoiceQueryKeys.application(applicationId ?? 0),
    queryFn: async () => {
      if (applicationId === null) throw new Error('application-id-required')
      const detail = await props.invoiceApi.getApplication(applicationId)
      setDisplayDetail(detail)
      return redactInvoiceApplicationDetailForCache(detail)
    },
    enabled: applicationId !== null,
    refetchInterval: (query) =>
      query.state.data && shouldPollInvoiceApplication(query.state.data)
        ? 30_000
        : false,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: 'always',
    staleTime: 0,
  })
  const detail = displayDetail?.id === applicationId ? displayDetail : undefined
  const detailLoading = detail === undefined && detailQuery.isFetching

  return (
    <Sheet open={applicationId !== null} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full overflow-y-auto sm:max-w-2xl'>
        <SheetHeader>
          <SheetTitle>{t('Invoice application details')}</SheetTitle>
          <SheetDescription>
            {detail?.application_no ?? t('Loading invoice application')}
          </SheetDescription>
          <Button
            variant='outline'
            size='sm'
            className='mt-2 w-fit'
            onClick={() => detailQuery.refetch()}
          >
            {t('Refresh')}
          </Button>
        </SheetHeader>
        <div className='space-y-5 px-4 pb-6'>
          {detailLoading ? (
            <>
              <Skeleton className='h-24 w-full' />
              <Skeleton className='h-48 w-full' />
            </>
          ) : detailQuery.isError ? (
            <Alert variant='destructive'>
              <AlertTitle>{t('Invoice details failed to load')}</AlertTitle>
              <AlertDescription>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => detailQuery.refetch()}
                >
                  {t('Retry')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : detail ? (
            <>
              <InvoiceStatusBadges application={detail} />
              <section>
                <h3 className='mb-2 font-medium'>{t('Profile snapshot')}</h3>
                <dl className='grid gap-2 text-sm sm:grid-cols-2'>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Profile type')}
                    </dt>
                    <dd>
                      {t(
                        detail.profile_snapshot.type === 'personal'
                          ? 'Personal'
                          : 'Company'
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Profile version')}
                    </dt>
                    <dd>v{detail.profile_snapshot.version}</dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Invoice profile')}
                    </dt>
                    <dd>{detail.profile_snapshot.title}</dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>{t('Tax number')}</dt>
                    <dd>
                      {detail.profile_snapshot.tax_number || t('Not available')}
                    </dd>
                  </div>
                </dl>
              </section>
              <Separator />
              <section>
                <h3 className='mb-2 font-medium'>{t('Policy snapshot')}</h3>
                <dl className='grid gap-2 text-sm sm:grid-cols-2'>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Application window')}
                    </dt>
                    <dd>
                      {detail.policy_snapshot.application_window_days}{' '}
                      {t('days')}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Minimum amount')}
                    </dt>
                    <dd>
                      {formatInvoiceAmount(
                        detail.policy_snapshot.minimum_amount_minor
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>{t('Wallet fee')}</dt>
                    <dd>{formatQuota(detail.policy_snapshot.fee_quota)}</dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('PDF retention')}
                    </dt>
                    <dd>
                      {detail.policy_snapshot.pdf_retention_days} {t('days')}
                    </dd>
                  </div>
                </dl>
              </section>
              <Separator />
              <section>
                <h3 className='mb-2 font-medium'>{t('Invoice items')}</h3>
                <div className='space-y-2'>
                  {detail.items.map((item) => (
                    <div
                      key={item.topup_id}
                      className='flex justify-between gap-3 rounded-md border p-3 text-sm'
                    >
                      <span>
                        {item.order_no} · {item.product_description}
                      </span>
                      <span className='shrink-0'>
                        {formatInvoiceAmount(item.paid_amount_minor)}
                      </span>
                    </div>
                  ))}
                </div>
              </section>
              <Separator />
              <section>
                <h3 className='mb-2 font-medium'>
                  {t('Issuance and document')}
                </h3>
                <dl className='grid gap-2 text-sm sm:grid-cols-2'>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Invoice number')}
                    </dt>
                    <dd>
                      {detail.issuance?.invoice_number ?? t('Not available')}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Invoice code')}
                    </dt>
                    <dd>
                      {detail.issuance?.invoice_code ?? t('Not available')}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Invoice date')}
                    </dt>
                    <dd>
                      {detail.issuance
                        ? new Date(
                            detail.issuance.invoice_date * 1000
                          ).toLocaleDateString()
                        : t('Not available')}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Document expires at')}
                    </dt>
                    <dd>
                      {detail.document?.expires_at
                        ? new Date(
                            detail.document.expires_at * 1000
                          ).toLocaleString()
                        : t('Not available')}
                    </dd>
                  </div>
                </dl>
              </section>
            </>
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}
