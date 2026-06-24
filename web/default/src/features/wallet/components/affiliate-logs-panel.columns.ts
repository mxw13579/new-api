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
import type { StaticDataTableColumn } from '@/components/data-table'
import { formatQuota, formatTimestampToDate } from '@/lib/format'

import type { AffiliateLogItem, AffiliateLogType } from '../types'

function getInviteeDisplay(log: AffiliateLogItem): string {
  if (log.invitee_display_name) {
    return log.invitee_display_name
  }

  if (log.invitee_username) {
    return log.invitee_username
  }

  return String(log.invitee_id)
}

export function createAffiliateLogColumns(
  t: (key: string) => string,
  type: AffiliateLogType
): StaticDataTableColumn<AffiliateLogItem>[] {
  const baseColumns: StaticDataTableColumn<AffiliateLogItem>[] = [
    {
      id: 'invitee',
      header: t('Invitee'),
      cellClassName: 'font-medium',
      cell: getInviteeDisplay,
    },
  ]

  if (type === 'topup_rebate') {
    baseColumns.push(
      {
        id: 'base_quota',
        header: t('Base quota'),
        cellClassName: 'text-right font-medium tabular-nums',
        className: 'text-right',
        cell: (log) => formatQuota(log.base_quota),
      },
      {
        id: 'rebate_percent',
        header: t('Rebate percent'),
        cellClassName: 'text-right tabular-nums',
        className: 'text-right',
        cell: (log) => `${log.rebate_percent}%`,
      }
    )
  }

  baseColumns.push(
    {
      id: 'reward_quota',
      header: t('Reward quota'),
      cellClassName: 'text-right font-semibold tabular-nums',
      className: 'text-right',
      cell: (log) => formatQuota(log.reward_quota),
    },
    {
      id: 'created_at',
      header: t('Created time'),
      cellClassName: 'text-muted-foreground text-xs tabular-nums',
      cell: (log) => formatTimestampToDate(log.created_at),
    }
  )

  return baseColumns
}
