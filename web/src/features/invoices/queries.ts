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

import type { InvoiceApi } from './types'

export const invoiceQueryKeys = {
  all: ['invoices'] as const,
  config: () => [...invoiceQueryKeys.all, 'config'] as const,
  profiles: () => [...invoiceQueryKeys.all, 'profiles'] as const,
  eligibleOrders: (page: number, pageSize: number) =>
    [...invoiceQueryKeys.all, 'eligible-orders', page, pageSize] as const,
  applications: (page: number, pageSize: number) =>
    [...invoiceQueryKeys.all, 'applications', page, pageSize] as const,
  application: (applicationId: number) =>
    [...invoiceQueryKeys.all, 'application', applicationId] as const,
}

export function useInvoiceQueries(invoiceApi: InvoiceApi) {
  const configQuery = useQuery({
    queryKey: invoiceQueryKeys.config(),
    queryFn: () => invoiceApi.getConfig(),
  })
  const profilesQuery = useQuery({
    queryKey: invoiceQueryKeys.profiles(),
    queryFn: () => invoiceApi.listProfiles(),
  })
  const ordersQuery = useQuery({
    queryKey: invoiceQueryKeys.eligibleOrders(1, 100),
    queryFn: () => invoiceApi.listEligibleOrders({ page: 1, page_size: 100 }),
  })
  const applicationsQuery = useQuery({
    queryKey: invoiceQueryKeys.applications(1, 50),
    queryFn: () => invoiceApi.listApplications({ page: 1, page_size: 50 }),
  })

  return { configQuery, profilesQuery, ordersQuery, applicationsQuery }
}
