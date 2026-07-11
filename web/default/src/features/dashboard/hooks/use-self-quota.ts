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
import {
  useQuery,
  type QueryClient,
  type UseQueryOptions,
} from '@tanstack/react-query'

import { getSelfQuota, type SelfQuotaData } from '../api'

export const SELF_QUOTA_QUERY_KEY = ['user', 'self', 'quota'] as const
export const SELF_QUOTA_REFETCH_INTERVAL = 300000
export const SELF_QUOTA_STALE_TIME = 60 * 1000

export function getSelfQuotaQueryOptions(): UseQueryOptions<
  SelfQuotaData,
  Error,
  SelfQuotaData,
  typeof SELF_QUOTA_QUERY_KEY
> {
  return {
    queryKey: SELF_QUOTA_QUERY_KEY,
    queryFn: async () => {
      const result = await getSelfQuota()
      if (!result.success || !result.data) {
        throw new Error(result.message || 'Failed to refresh quota')
      }
      return { quota: Number(result.data.quota) }
    },
    staleTime: SELF_QUOTA_STALE_TIME,
    refetchInterval: SELF_QUOTA_REFETCH_INTERVAL,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
  }
}

export function useSelfQuota() {
  return useQuery(getSelfQuotaQueryOptions())
}

export async function invalidateSelfQuotaQuery(
  queryClient: QueryClient
): Promise<void> {
  await queryClient.invalidateQueries({
    queryKey: SELF_QUOTA_QUERY_KEY,
    exact: true,
  })
}
