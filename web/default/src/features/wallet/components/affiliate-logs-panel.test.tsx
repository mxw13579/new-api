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
import { describe, expect, it } from 'bun:test'

import { formatQuota } from '@/lib/format'

import type { AffiliateLogItem } from '../types'
import { createAffiliateLogColumns } from './affiliate-logs-panel.columns'

const t = (key: string) => key

const inviteLog: AffiliateLogItem = {
  id: 1,
  inviter_id: 10,
  invitee_id: 20,
  invitee_display_name: 'Ada Lovelace',
  invitee_username: 'ada',
  type: 'invite_reward',
  reward_quota: 500000,
  base_quota: 0,
  rebate_percent: 0,
  created_at: 1760000000,
}

const rebateLog: AffiliateLogItem = {
  id: 2,
  inviter_id: 10,
  invitee_id: 21,
  invitee_display_name: 'Grace Hopper',
  invitee_username: 'grace',
  type: 'topup_rebate',
  reward_quota: 250000,
  base_quota: 5000000,
  rebate_percent: 5,
  created_at: 1760000100,
}

describe('createAffiliateLogColumns', () => {
  it('creates invite log columns with the invitee display name', () => {
    const columns = createAffiliateLogColumns(t, 'invite_reward')

    expect(columns.map((column) => column.id)).toEqual([
      'invitee',
      'reward_quota',
      'created_at',
    ])
    expect(columns.map((column) => column.header)).toEqual([
      'Invitee',
      'Reward quota',
      'Created time',
    ])
    expect(columns[0].cell?.(inviteLog, 0)).toBe('Ada Lovelace')
    expect(columns[1].cell?.(inviteLog, 0)).toBe(
      formatQuota(inviteLog.reward_quota)
    )
    expect(columns[2].cell?.(inviteLog, 0)).toContain('2025-')
  })

  it('falls back to username and then invitee ID for invitee display', () => {
    const columns = createAffiliateLogColumns(t, 'invite_reward')

    expect(
      columns[0].cell?.({ ...inviteLog, invitee_display_name: '' }, 0)
    ).toBe('ada')
    expect(
      columns[0].cell?.(
        { ...inviteLog, invitee_display_name: '', invitee_username: '' },
        0
      )
    ).toBe('20')
  })

  it('creates rebate log columns without exposing the transaction number', () => {
    const columns = createAffiliateLogColumns(t, 'topup_rebate')

    expect(columns.map((column) => column.id)).toEqual([
      'invitee',
      'base_quota',
      'rebate_percent',
      'reward_quota',
      'created_at',
    ])
    expect(columns.map((column) => column.header)).toEqual([
      'Invitee',
      'Base quota',
      'Rebate percent',
      'Reward quota',
      'Created time',
    ])
    expect(columns[0].cell?.(rebateLog, 0)).toBe('Grace Hopper')
    expect(columns[1].cell?.(rebateLog, 0)).toBe(
      formatQuota(rebateLog.base_quota)
    )
    expect(columns[2].cell?.(rebateLog, 0)).toBe('5%')
    expect(columns[3].cell?.(rebateLog, 0)).toBe(
      formatQuota(rebateLog.reward_quota)
    )
    expect(columns[4].cell?.(rebateLog, 0)).toContain('2025-')
  })
})
