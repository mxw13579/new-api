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
import i18next from 'i18next'
import { useCallback, useEffect, useState } from 'react'

import { getAffiliateLogs, isApiSuccess } from '../api'
import type { AffiliateLogItem, AffiliateLogType } from '../types'

interface UseAffiliateLogsOptions {
  initialType?: AffiliateLogType
  pageSize?: number
}

export function useAffiliateLogs(options: UseAffiliateLogsOptions = {}) {
  const { initialType = 'invite_reward', pageSize = 5 } = options
  const [type, setType] = useState<AffiliateLogType>(initialType)
  const [items, setItems] = useState<AffiliateLogItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchLogs = useCallback(async () => {
    setLoading(true)
    setError(null)

    try {
      const response = await getAffiliateLogs(type, page, pageSize)

      if (isApiSuccess(response) && response.data) {
        setItems(response.data.items || [])
        setTotal(response.data.total || 0)
        return
      }

      setItems([])
      setTotal(0)
      setError(response.message || i18next.t('Failed to load affiliate logs'))
    } catch (err) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch affiliate logs:', err)
      setItems([])
      setTotal(0)
      setError(i18next.t('Failed to load affiliate logs'))
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, type])

  const handleTypeChange = useCallback((nextType: AffiliateLogType) => {
    setType(nextType)
    setPage(1)
  }, [])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  useEffect(() => {
    fetchLogs()
  }, [fetchLogs])

  return {
    type,
    items,
    total,
    page,
    pageSize,
    totalPages,
    loading,
    error,
    setType: handleTypeChange,
    setPage,
    refresh: fetchLogs,
  }
}
