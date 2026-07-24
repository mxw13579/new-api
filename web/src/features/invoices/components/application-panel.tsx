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
import { Skeleton } from '@/components/ui/skeleton'

import { InvoiceApiError } from '../api'
import { getInvoiceErrorMessageKey } from '../contract'
import type {
  EligibleInvoiceOrder,
  InvoiceApi,
  InvoiceConfig,
  InvoicePage,
  InvoiceProfile,
} from '../types'
import {
  createInvoiceDraftIdentity,
  createInvoiceOrderSelection,
  getInvoiceOrderSelectionSummary,
  invalidateUserInvoiceMutationQueries,
  isInvoiceProfileEnabled,
  updateInvoiceOrderSelection,
} from '../user-workspace'
import { ApplicationConfirmation } from './application-confirmation'
import { ApplicationPolicy } from './application-policy'
import { ApplicationProfileSelection } from './application-profile-selection'
import { EligibleOrderSelection } from './eligible-order-selection'

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
  const [orderSelection, setOrderSelection] = useState(
    createInvoiceOrderSelection
  )
  const [selectedProfileId, setSelectedProfileId] = useState<number>(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [liveMessageKey, setLiveMessageKey] = useState('')
  const reviewButtonRef = useRef<HTMLButtonElement>(null)
  const draftIdentityRef = useRef(createInvoiceDraftIdentity())
  const profiles = props.profiles || []
  const selectedProfile = profiles.find(
    (profile) => profile.id === selectedProfileId
  )
  const selectionSummary = getInvoiceOrderSelectionSummary(
    orderSelection,
    props.config?.minimum_amount_minor ?? 0
  )
  const minimumReached = !!props.config && selectionSummary.minimumReached
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
      const topupIds = selectionSummary.topupIds
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
      setOrderSelection(createInvoiceOrderSelection())
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
          <ApplicationPolicy
            config={props.config}
            loading={props.configLoading}
            error={props.configError}
            retry={props.retryConfig}
            formatAmount={formatCny}
          />

          <ApplicationProfileSelection
            config={props.config}
            profiles={profiles}
            selectedProfileId={selectedProfileId}
            onSelectedProfileIdChange={setSelectedProfileId}
            loading={props.profilesLoading}
            error={props.profilesError}
            retry={props.retryProfiles}
          />

          <EligibleOrderSelection
            ordersPage={props.ordersPage}
            loading={props.ordersLoading}
            error={props.ordersError}
            retry={props.retryOrders}
            selection={orderSelection}
            onSelectionChange={(order, selected) =>
              setOrderSelection((current) =>
                updateInvoiceOrderSelection(current, order, selected)
              )
            }
            onPageChange={props.onOrdersPageChange}
            formatAmount={formatCny}
          />

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
              {t('Selected amount')}: {formatCny(selectionSummary.amountMinor)}
            </p>
            <p className='text-muted-foreground text-xs'>
              {t('{{count}} complete orders selected', {
                count: orderSelection.size,
              })}
            </p>
          </div>
          <Button
            ref={reviewButtonRef}
            onClick={() => setConfirmOpen(true)}
            disabled={
              !selectedProfile ||
              !selectedProfileEnabled ||
              orderSelection.size === 0 ||
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

      <ApplicationConfirmation
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        reviewButtonRef={reviewButtonRef}
        profileTitle={selectedProfile?.title}
        profileVersion={selectedProfile?.version}
        selectedOrderCount={orderSelection.size}
        formattedAmount={formatCny(selectionSummary.amountMinor)}
        feeQuota={props.config?.fee_quota ?? 0}
        pending={createMutation.isPending}
        onSubmit={() => createMutation.mutate()}
      />
    </>
  )
}
