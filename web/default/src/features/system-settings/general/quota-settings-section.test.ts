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

import { quotaSchema } from './quota-settings-section.schema'

const baseQuotaSettings = {
  QuotaForNewUser: 0,
  PreConsumedQuota: 0,
  QuotaForInviter: 0,
  RechargeRebateRatioForInviter: 0,
  QuotaForInvitee: 0,
  TopUpLink: '',
  general_setting: {
    docs_link: '',
  },
  quota_setting: {
    enable_free_model_pre_consume: false,
  },
}

describe('quota settings schema', () => {
  it('accepts wallet top-up rebate ratio boundaries', () => {
    expect(
      quotaSchema.safeParse({
        ...baseQuotaSettings,
        RechargeRebateRatioForInviter: 0,
      }).success
    ).toBe(true)
    expect(
      quotaSchema.safeParse({
        ...baseQuotaSettings,
        RechargeRebateRatioForInviter: 100,
      }).success
    ).toBe(true)
  })

  it('rejects wallet top-up rebate ratio outside 0..100', () => {
    expect(
      quotaSchema.safeParse({
        ...baseQuotaSettings,
        RechargeRebateRatioForInviter: -1,
      }).success
    ).toBe(false)
    expect(
      quotaSchema.safeParse({
        ...baseQuotaSettings,
        RechargeRebateRatioForInviter: 101,
      }).success
    ).toBe(false)
  })
})
