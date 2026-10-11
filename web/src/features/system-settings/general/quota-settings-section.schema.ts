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
import * as z from 'zod'

export const quotaSchema = z.object({
  QuotaForNewUser: z.coerce.number().min(0),
  QuotaForInviter: z.coerce.number().min(0),
  RechargeRebateRatioForInviter: z.coerce.number().min(0).max(100),
  QuotaForInvitee: z.coerce.number().min(0),
  TopUpLink: z.string(),
  quota_setting: z.object({
    enable_free_model_pre_consume: z.boolean(),
    trust_quota_usd: z.preprocess(
      (value) => (value === '' ? undefined : value),
      z.coerce
        .number({ error: () => i18next.t('Please enter a valid number') })
        .min(0, {
          error: () => i18next.t('Must be greater than or equal to 0'),
        })
    ),
    pre_consume_multiplier: z.coerce
      .number({ error: () => i18next.t('Please enter a valid number') })
      .positive({ error: () => i18next.t('Must be greater than 0') }),
  }),
})

export type QuotaFormValues = z.infer<typeof quotaSchema>
