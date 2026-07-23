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
import { InvoiceIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Settings } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Main } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { ForbiddenError } from '@/features/errors/forbidden'
import { useAuthStore } from '@/stores/auth-store'

import { InvoiceApiError } from '../invoices/api'
import { getInvoiceErrorMessageKey } from '../invoices/contract'
import { adminInvoiceApi, invoiceSettingsApi } from './api'
import { ApplicationDetail } from './components/application-detail'
import { ApplicationList } from './components/application-list'
import { SettingsForm } from './components/settings-form'
import { getInvoiceAdminCapabilities } from './contract'
import { invoiceSettingsQueryKeys } from './queries'

/** Renders the independently permissioned administrator review workspace. */
export function AdminInvoices() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const capabilities = getInvoiceAdminCapabilities(user)
  const [selectedApplicationId, setSelectedApplicationId] = useState<
    number | null
  >(null)

  if (!capabilities.canReview) return <ForbiddenError />

  return (
    <Main>
      <div className='min-h-0 flex-1 overflow-auto px-3 py-3 sm:px-4 sm:py-6'>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-5'>
          <header className='flex min-w-0 items-start gap-3'>
            <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-lg'>
              <HugeiconsIcon
                icon={InvoiceIcon}
                strokeWidth={2}
                aria-hidden='true'
              />
            </div>
            <div className='min-w-0'>
              <h1 className='text-2xl font-semibold tracking-tight'>
                {t('Invoice management')}
              </h1>
              <p className='text-muted-foreground break-words'>
                {t('Review applications and manage invoice documents.')}
              </p>
            </div>
          </header>
          <ApplicationList
            invoiceApi={adminInvoiceApi}
            onSelect={setSelectedApplicationId}
          />
        </div>
      </div>
      <ApplicationDetail
        applicationId={selectedApplicationId}
        invoiceApi={adminInvoiceApi}
        canReadSensitive={capabilities.canReadSensitive}
        canUploadDocument={capabilities.canUploadDocument}
        onClose={() => setSelectedApplicationId(null)}
      />
    </Main>
  )
}

/** Renders the complete-object invoice policy editor on its own route. */
export function InvoiceSettings() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const capabilities = getInvoiceAdminCapabilities(user)
  const queryClient = useQueryClient()
  const settingQuery = useQuery({
    queryKey: invoiceSettingsQueryKeys.detail(),
    queryFn: () => invoiceSettingsApi.getSetting(),
    enabled: capabilities.canManageSettings,
  })
  const updateMutation = useMutation({
    mutationFn: invoiceSettingsApi.updateSetting,
    onSuccess: async (setting) => {
      queryClient.setQueryData(invoiceSettingsQueryKeys.detail(), setting)
      await queryClient.invalidateQueries({
        queryKey: invoiceSettingsQueryKeys.detail(),
      })
      toast.success(t('Invoice settings saved'))
    },
    onError: (error) => {
      const code =
        error instanceof InvoiceApiError ? error.code : 'INVOICE_INTERNAL_ERROR'
      toast.error(t(getInvoiceErrorMessageKey(code)))
    },
  })

  if (!capabilities.canManageSettings) return <ForbiddenError />

  return (
    <Main>
      <div className='min-h-0 flex-1 overflow-auto px-3 py-3 sm:px-4 sm:py-6'>
        <div className='mx-auto flex w-full max-w-3xl flex-col gap-5'>
          <header className='flex min-w-0 items-start gap-3'>
            <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-lg'>
              <Settings aria-hidden='true' />
            </div>
            <div className='min-w-0'>
              <h1 className='text-2xl font-semibold tracking-tight'>
                {t('Invoice settings')}
              </h1>
              <p className='text-muted-foreground break-words'>
                {t(
                  'Configure application eligibility, fees, and PDF retention.'
                )}
              </p>
            </div>
          </header>

          <Card>
            <CardHeader>
              <CardTitle>{t('Invoice policy')}</CardTitle>
            </CardHeader>
            <CardContent>
              {settingQuery.isLoading ? (
                <div
                  className='space-y-3'
                  aria-label={t('Loading invoice settings')}
                >
                  <Skeleton className='h-10 w-full' />
                  <Skeleton className='h-40 w-full' />
                </div>
              ) : null}
              {settingQuery.isError ? (
                <Alert variant='destructive'>
                  <AlertTitle>
                    {t('Unable to load invoice settings')}
                  </AlertTitle>
                  <AlertDescription className='space-y-2'>
                    <p>
                      {t(
                        getInvoiceErrorMessageKey(
                          settingQuery.error instanceof InvoiceApiError
                            ? settingQuery.error.code
                            : 'INVOICE_INTERNAL_ERROR'
                        )
                      )}
                    </p>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => settingQuery.refetch()}
                    >
                      {t('Retry')}
                    </Button>
                  </AlertDescription>
                </Alert>
              ) : null}
              {!settingQuery.isLoading &&
              !settingQuery.isError &&
              settingQuery.data ? (
                <SettingsForm
                  key={JSON.stringify(settingQuery.data)}
                  setting={settingQuery.data}
                  pending={updateMutation.isPending}
                  onSave={(setting) => updateMutation.mutate(setting)}
                />
              ) : null}
            </CardContent>
          </Card>
        </div>
      </div>
    </Main>
  )
}
