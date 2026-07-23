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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useSelfQuota } from '@/features/dashboard/hooks/use-self-quota'

import { ApplicationPanel } from './components/application-panel'
import { HistoryPanel } from './components/history-panel'
import { ProfilesPanel } from './components/profiles-panel'
import { invoiceApi } from './production-api'
import { invoiceQueryKeys } from './queries'
import { shouldPollInvoiceApplication } from './user-workspace'

const USER_PAGE_SIZE = 20

/** Renders the authenticated personal-invoice workspace. */
export function Invoices() {
  const { t } = useTranslation()
  const [ordersPage, setOrdersPage] = useState(1)
  const [applicationsPage, setApplicationsPage] = useState(1)
  const configQuery = useQuery({
    queryKey: invoiceQueryKeys.config(),
    queryFn: () => invoiceApi.getConfig(),
  })
  const profilesQuery = useQuery({
    queryKey: invoiceQueryKeys.profiles(),
    queryFn: () => invoiceApi.listProfiles(),
  })
  const ordersQuery = useQuery({
    queryKey: invoiceQueryKeys.eligibleOrders(ordersPage, USER_PAGE_SIZE),
    queryFn: () =>
      invoiceApi.listEligibleOrders({
        page: ordersPage,
        page_size: USER_PAGE_SIZE,
      }),
  })
  const applicationsQuery = useQuery({
    queryKey: invoiceQueryKeys.applications(applicationsPage, USER_PAGE_SIZE),
    queryFn: () =>
      invoiceApi.listApplications({
        page: applicationsPage,
        page_size: USER_PAGE_SIZE,
      }),
    refetchInterval: (query) =>
      query.state.data?.items.some(shouldPollInvoiceApplication)
        ? 30_000
        : false,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: 'always',
  })
  const selfQuotaQuery = useSelfQuota()
  const invoicesEnabled =
    configQuery.data?.personal_enabled || configQuery.data?.company_enabled

  return (
    <Main>
      <div className='min-h-0 flex-1 overflow-auto px-3 py-3 sm:px-4 sm:py-6'>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-5'>
          <header className='flex items-start gap-3'>
            <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-lg'>
              <HugeiconsIcon
                icon={InvoiceIcon}
                strokeWidth={2}
                aria-hidden='true'
              />
            </div>
            <div>
              <h1 className='text-2xl font-semibold tracking-tight'>
                {t('Invoices')}
              </h1>
              <p className='text-muted-foreground'>
                {t('Manage invoice profiles, applications, and PDF documents.')}
              </p>
            </div>
          </header>

          {configQuery.data && !invoicesEnabled ? (
            <Alert>
              <AlertTitle>{t('Invoice applications are disabled')}</AlertTitle>
              <AlertDescription>
                {t('Contact an administrator if invoice access is required.')}
              </AlertDescription>
            </Alert>
          ) : null}
          <Tabs defaultValue='apply'>
            <TabsList className='max-w-full overflow-x-auto'>
              <TabsTrigger value='apply'>{t('Apply')}</TabsTrigger>
              <TabsTrigger value='profiles'>{t('Profiles')}</TabsTrigger>
              <TabsTrigger value='history'>{t('History')}</TabsTrigger>
            </TabsList>
            <TabsContent value='apply'>
              <ApplicationPanel
                invoiceApi={invoiceApi}
                config={configQuery.data}
                profiles={profilesQuery.data}
                profilesLoading={profilesQuery.isLoading}
                profilesError={profilesQuery.isError}
                retryProfiles={() => profilesQuery.refetch()}
                ordersPage={ordersQuery.data}
                ordersLoading={ordersQuery.isLoading}
                ordersError={ordersQuery.isError}
                retryOrders={() => ordersQuery.refetch()}
                onOrdersPageChange={setOrdersPage}
                configLoading={configQuery.isLoading}
                configError={configQuery.isError}
                retryConfig={() => configQuery.refetch()}
                walletQuota={selfQuotaQuery.data?.quota}
                quotaLoading={selfQuotaQuery.isLoading}
                quotaError={selfQuotaQuery.isError}
                retryQuota={() => selfQuotaQuery.refetch()}
              />
            </TabsContent>
            <TabsContent value='profiles'>
              <ProfilesPanel
                invoiceApi={invoiceApi}
                profiles={profilesQuery.data}
                loading={profilesQuery.isLoading}
                error={profilesQuery.isError}
                retry={() => profilesQuery.refetch()}
                config={configQuery.data}
              />
            </TabsContent>
            <TabsContent value='history'>
              <HistoryPanel
                invoiceApi={invoiceApi}
                applicationsPage={applicationsQuery.data}
                loading={applicationsQuery.isLoading}
                error={applicationsQuery.isError}
                retry={() => applicationsQuery.refetch()}
                onPageChange={setApplicationsPage}
              />
            </TabsContent>
          </Tabs>
        </div>
      </div>
    </Main>
  )
}
