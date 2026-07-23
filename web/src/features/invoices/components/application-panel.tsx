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
import { InvoiceIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
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
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldDescription,
  FieldLabel,
  FieldSet,
  FieldLegend,
} from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { useIsMobile } from '@/hooks/use-mobile'

import { InvoiceApiError } from '../api'
import {
  calculateSelectedAmountMinor,
  getInvoiceErrorMessageKey,
} from '../contract'
import type {
  EligibleInvoiceOrder,
  InvoiceApi,
  InvoiceConfig,
  InvoicePage,
  InvoiceProfile,
} from '../types'
import {
  createInvoiceDraftIdentity,
  invalidateUserInvoiceMutationQueries,
  invoicePageCount,
  isInvoiceProfileEnabled,
} from '../user-workspace'

interface ApplicationPanelProps {
  invoiceApi: InvoiceApi
  config: InvoiceConfig | undefined
  profiles: InvoiceProfile[] | undefined
  profilesLoading: boolean
  profilesError: boolean
  retryProfiles: () => void
  ordersPage: InvoicePage<EligibleInvoiceOrder> | undefined
  ordersLoading: boolean
  ordersError: boolean
  retryOrders: () => void
  onOrdersPageChange: (page: number) => void
  configLoading: boolean
  configError: boolean
  retryConfig: () => void
  walletQuota: number | undefined
  quotaLoading: boolean
  quotaError: boolean
  retryQuota: () => void
}

function formatCny(minor: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'CNY',
  }).format(minor / 100)
}

/**
 * Renders order selection and invoice application submission.
 *
 * @param props - Invoice policy, selectable records, and API dependencies.
 * @returns The application workflow for desktop and mobile layouts.
 */
export function ApplicationPanel(props: ApplicationPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isMobile = useIsMobile()
  const [selectedTopUpIds, setSelectedTopUpIds] = useState<Set<number>>(
    () => new Set()
  )
  const [selectedProfileId, setSelectedProfileId] = useState<number>(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [liveMessageKey, setLiveMessageKey] = useState('')
  const reviewButtonRef = useRef<HTMLButtonElement>(null)
  const draftIdentityRef = useRef(createInvoiceDraftIdentity())
  const orders = props.ordersPage?.items || []
  const ordersPage = props.ordersPage
  const profiles = props.profiles || []
  const selectedProfile = profiles.find(
    (profile) => profile.id === selectedProfileId
  )
  const selectedAmountMinor = calculateSelectedAmountMinor(
    orders,
    selectedTopUpIds
  )
  const minimumReached =
    !!props.config && selectedAmountMinor >= props.config.minimum_amount_minor
  const quotaAvailable =
    !!props.config &&
    props.walletQuota !== undefined &&
    props.walletQuota >= props.config.fee_quota
  const selectedProfileEnabled =
    !!props.config &&
    !!selectedProfile &&
    isInvoiceProfileEnabled(props.config, selectedProfile.type)

  const createMutation = useMutation({
    mutationFn: () => {
      if (!selectedProfile) throw new Error('profile-required')
      const topupIds = [...selectedTopUpIds].sort((left, right) => left - right)
      const fingerprint = JSON.stringify({
        profileId: selectedProfile.id,
        profileVersion: selectedProfile.version,
        topupIds,
        feeQuota: props.config?.fee_quota,
      })
      return props.invoiceApi.createApplication({
        request_id: draftIdentityRef.current.forDraft(fingerprint),
        profile_id: selectedProfile.id,
        profile_version: selectedProfile.version,
        topup_ids: topupIds,
      })
    },
    onSuccess: async (application) => {
      await invalidateUserInvoiceMutationQueries(queryClient, application.id)
      draftIdentityRef.current.reset()
      setSelectedTopUpIds(new Set())
      setConfirmOpen(false)
      setLiveMessageKey('Invoice application submitted successfully')
    },
    onError: async (error) => {
      if (error instanceof InvoiceApiError) {
        if (
          error.code === 'INVOICE_STATE_CONFLICT' ||
          error.code === 'INVOICE_TOPUP_INELIGIBLE' ||
          error.code === 'INVOICE_PAYMENT_EVIDENCE_CONFLICT'
        ) {
          await invalidateUserInvoiceMutationQueries(queryClient)
        }
        setLiveMessageKey(getInvoiceErrorMessageKey(error.code))
        return
      }
      setLiveMessageKey('Invoice error: service unavailable')
    },
  })

  const confirmation = (
    <div className='space-y-3 px-4 pb-4 sm:px-0 sm:pb-0'>
      <dl className='grid gap-2 text-sm'>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Invoice profile')}</dt>
          <dd className='text-right font-medium'>{selectedProfile?.title}</dd>
        </div>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Profile version')}</dt>
          <dd>{selectedProfile?.version}</dd>
        </div>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Selected orders')}</dt>
          <dd>{selectedTopUpIds.size}</dd>
        </div>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Invoice amount')}</dt>
          <dd className='font-medium'>{formatCny(selectedAmountMinor)}</dd>
        </div>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Wallet quota fee')}</dt>
          <dd>{props.config?.fee_quota ?? 0}</dd>
        </div>
      </dl>
    </div>
  )

  if (props.configLoading && props.profilesLoading && props.ordersLoading) {
    return (
      <Card>
        <CardHeader>
          <Skeleton className='h-5 w-44' />
          <Skeleton className='h-4 w-72 max-w-full' />
        </CardHeader>
        <CardContent className='space-y-3'>
          <Skeleton className='h-9 w-full' />
          <Skeleton className='h-28 w-full' />
        </CardContent>
      </Card>
    )
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>{t('Apply for an invoice')}</CardTitle>
          <CardDescription>
            {t(
              'Select complete paid orders. Partial order amounts are not supported.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className='space-y-5'>
          {props.configError ? (
            <Alert variant='destructive'>
              <AlertTitle>{t('Invoice policy failed to load')}</AlertTitle>
              <AlertDescription>
                <Button variant='outline' size='sm' onClick={props.retryConfig}>
                  {t('Retry')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : props.configLoading ? (
            <Skeleton className='h-20 w-full' />
          ) : (
            <Alert>
              <AlertTitle>{t('Invoice policy')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Orders from the last {{days}} days are eligible. Minimum {{minimum}}, fee {{fee}} wallet quota, PDF retention {{retention}} days.',
                  {
                    days: props.config?.application_window_days,
                    minimum: formatCny(props.config?.minimum_amount_minor ?? 0),
                    fee: props.config?.fee_quota,
                    retention: props.config?.pdf_retention_days,
                  }
                )}
              </AlertDescription>
            </Alert>
          )}

          <Field>
            <FieldLabel htmlFor='invoice-profile'>
              {t('Invoice profile')}
            </FieldLabel>
            <NativeSelect
              id='invoice-profile'
              className='w-full'
              value={selectedProfileId || ''}
              onChange={(event) =>
                setSelectedProfileId(Number(event.target.value))
              }
            >
              <NativeSelectOption value=''>
                {t('Select an invoice profile')}
              </NativeSelectOption>
              {profiles.map((profile) => {
                const enabled =
                  !props.config ||
                  isInvoiceProfileEnabled(props.config, profile.type)
                return (
                  <NativeSelectOption
                    key={profile.id}
                    value={profile.id}
                    disabled={!enabled}
                  >
                    {profile.title} · v{profile.version}
                    {!enabled ? ` (${t('Disabled for new applications')})` : ''}
                  </NativeSelectOption>
                )
              })}
            </NativeSelect>
            <FieldDescription>
              {t(
                'The selected profile version is submitted with the application.'
              )}
            </FieldDescription>
            {props.profilesLoading ? <Skeleton className='h-8 w-full' /> : null}
            {props.profilesError ? (
              <Alert variant='destructive'>
                <AlertTitle>{t('Invoice profiles failed to load')}</AlertTitle>
                <AlertDescription>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={props.retryProfiles}
                  >
                    {t('Retry')}
                  </Button>
                </AlertDescription>
              </Alert>
            ) : null}
          </Field>

          <FieldSet>
            <FieldLegend>{t('Eligible paid orders')}</FieldLegend>
            {props.ordersLoading ? (
              <Skeleton className='h-28 w-full' />
            ) : props.ordersError ? (
              <Alert variant='destructive'>
                <AlertTitle>{t('Eligible orders failed to load')}</AlertTitle>
                <AlertDescription>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={props.retryOrders}
                  >
                    {t('Retry')}
                  </Button>
                </AlertDescription>
              </Alert>
            ) : orders.length === 0 ? (
              <Empty className='border'>
                <EmptyHeader>
                  <EmptyMedia variant='icon'>
                    <HugeiconsIcon icon={InvoiceIcon} strokeWidth={2} />
                  </EmptyMedia>
                  <EmptyTitle>{t('No eligible orders')}</EmptyTitle>
                  <EmptyDescription>
                    {t('Eligible paid orders will appear here.')}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className='overflow-x-auto rounded-lg border'>
                <table className='w-full min-w-[640px] text-left text-sm'>
                  <thead className='bg-muted/50'>
                    <tr>
                      <th className='p-3'>{t('Select')}</th>
                      <th className='p-3'>{t('Order number')}</th>
                      <th className='p-3'>{t('Product')}</th>
                      <th className='p-3'>{t('Paid at')}</th>
                      <th className='p-3 text-right'>{t('Paid amount')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {orders.map((order) => {
                      const checkboxId = `invoice-order-${order.topup_id}`
                      return (
                        <tr key={order.topup_id} className='border-t'>
                          <td className='p-3'>
                            <Checkbox
                              id={checkboxId}
                              checked={selectedTopUpIds.has(order.topup_id)}
                              onCheckedChange={(checked) => {
                                setSelectedTopUpIds((current) => {
                                  const next = new Set(current)
                                  if (checked) next.add(order.topup_id)
                                  else next.delete(order.topup_id)
                                  return next
                                })
                              }}
                              aria-label={t('Select order {{order}}', {
                                order: order.order_no,
                              })}
                            />
                          </td>
                          <td className='p-3 font-medium'>
                            <label htmlFor={checkboxId}>{order.order_no}</label>
                          </td>
                          <td className='p-3'>{order.product_description}</td>
                          <td className='p-3'>
                            {new Date(order.paid_at * 1000).toLocaleString()}
                          </td>
                          <td className='p-3 text-right'>
                            {formatCny(order.paid_amount_minor)}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
            {ordersPage ? (
              <div className='flex items-center justify-end gap-2'>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={ordersPage.page <= 1}
                  onClick={() => props.onOrdersPageChange(ordersPage.page - 1)}
                >
                  {t('Previous')}
                </Button>
                <span className='text-muted-foreground text-sm'>
                  {t('Page {{page}} of {{pages}}', {
                    page: ordersPage.page,
                    pages: invoicePageCount(
                      ordersPage.total,
                      ordersPage.page_size
                    ),
                  })}
                </span>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={
                    ordersPage.page >=
                    invoicePageCount(ordersPage.total, ordersPage.page_size)
                  }
                  onClick={() => props.onOrdersPageChange(ordersPage.page + 1)}
                >
                  {t('Next')}
                </Button>
              </div>
            ) : null}
          </FieldSet>

          {props.quotaError ? (
            <Alert variant='destructive'>
              <AlertTitle>{t('Wallet quota failed to load')}</AlertTitle>
              <AlertDescription>
                <Button variant='outline' size='sm' onClick={props.retryQuota}>
                  {t('Retry')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : props.quotaLoading ? (
            <Skeleton className='h-16 w-full' />
          ) : !quotaAvailable ? (
            <Alert variant='destructive'>
              <AlertTitle>
                {t('Insufficient wallet quota for invoice fee')}
              </AlertTitle>
              <AlertDescription>
                {t('Top up wallet quota before submitting this application.')}
              </AlertDescription>
            </Alert>
          ) : null}
          <div aria-live='polite' className='text-muted-foreground text-sm'>
            {liveMessageKey ? t(liveMessageKey) : null}
          </div>
        </CardContent>
        <CardFooter className='flex flex-wrap justify-between gap-3'>
          <div>
            <p className='font-medium'>
              {t('Selected amount')}: {formatCny(selectedAmountMinor)}
            </p>
            <p className='text-muted-foreground text-xs'>
              {t('{{count}} complete orders selected', {
                count: selectedTopUpIds.size,
              })}
            </p>
          </div>
          <Button
            ref={reviewButtonRef}
            onClick={() => setConfirmOpen(true)}
            disabled={
              !selectedProfile ||
              !selectedProfileEnabled ||
              selectedTopUpIds.size === 0 ||
              !minimumReached ||
              !quotaAvailable ||
              props.configError ||
              props.ordersError ||
              props.profilesError
            }
          >
            <HugeiconsIcon
              icon={InvoiceIcon}
              strokeWidth={2}
              data-icon='inline-start'
            />
            {t('Review invoice application')}
          </Button>
        </CardFooter>
      </Card>

      {isMobile ? (
        <Drawer
          open={confirmOpen}
          onOpenChange={(open) => {
            setConfirmOpen(open)
            if (!open) {
              requestAnimationFrame(() => reviewButtonRef.current?.focus())
            }
          }}
        >
          <DrawerContent>
            <DrawerHeader>
              <DrawerTitle>{t('Confirm invoice application')}</DrawerTitle>
              <DrawerDescription>
                {t(
                  'Confirm the profile version, complete orders, amount, and fee.'
                )}
              </DrawerDescription>
            </DrawerHeader>
            {confirmation}
            <DrawerFooter>
              <Button
                autoFocus
                onClick={() => createMutation.mutate()}
                disabled={createMutation.isPending}
              >
                {createMutation.isPending ? (
                  <Spinner data-icon='inline-start' />
                ) : (
                  <HugeiconsIcon
                    icon={InvoiceIcon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                )}
                {t('Submit invoice application')}
              </Button>
              <Button variant='outline' onClick={() => setConfirmOpen(false)}>
                {t('Cancel')}
              </Button>
            </DrawerFooter>
          </DrawerContent>
        </Drawer>
      ) : (
        <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('Confirm invoice application')}</DialogTitle>
              <DialogDescription>
                {t(
                  'Confirm the profile version, complete orders, amount, and fee.'
                )}
              </DialogDescription>
            </DialogHeader>
            {confirmation}
            <DialogFooter>
              <Button variant='outline' onClick={() => setConfirmOpen(false)}>
                {t('Cancel')}
              </Button>
              <Button
                onClick={() => createMutation.mutate()}
                disabled={createMutation.isPending}
              >
                {createMutation.isPending ? (
                  <Spinner data-icon='inline-start' />
                ) : (
                  <HugeiconsIcon
                    icon={InvoiceIcon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                )}
                {t('Submit invoice application')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}
