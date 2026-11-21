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
import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'

import { SYSTEM_SETTINGS_VIEW } from '../system-settings.config'

const t = ((key: string) => key) as unknown as TFunction

function getItems(badges = {}) {
  return SYSTEM_SETTINGS_VIEW.getNavGroups(t, badges)[0].items
}

describe('system settings sidebar navigation', () => {
  it('lists every settings group with its sections in order', () => {
    const items = getItems()

    expect(
      items.map((item) => ({
        title: item.title,
        urls: 'items' in item ? item.items?.map((sub) => sub.url) : [],
      }))
    ).toEqual([
      {
        title: 'Site & Branding',
        urls: [
          '/system-settings/site/system-info',
          '/system-settings/site/notice',
          '/system-settings/site/header-navigation',
          '/system-settings/site/sidebar-modules',
        ],
      },
      {
        title: 'Authentication',
        urls: [
          '/system-settings/auth/basic-auth',
          '/system-settings/auth/oauth',
          '/system-settings/auth/passkey',
          '/system-settings/auth/bot-protection',
          '/system-settings/auth/custom-oauth',
        ],
      },
      {
        title: 'Billing & Payment',
        urls: [
          '/system-settings/billing/quota',
          '/system-settings/billing/currency',
          '/system-settings/billing/model-pricing',
          '/system-settings/billing/group-pricing',
          '/system-settings/billing/payment',
          '/system-settings/billing/checkin',
        ],
      },
      {
        title: 'Models',
        urls: [
          '/system-settings/models/global',
          '/system-settings/models/gemini',
          '/system-settings/models/claude',
          '/system-settings/models/grok',
          '/system-settings/models/model-deployment',
        ],
      },
      {
        title: 'Request policies',
        urls: [
          '/system-settings/request-policies/routing',
          '/system-settings/request-policies/health',
        ],
      },
      {
        title: 'Security & Limits',
        urls: [
          '/system-settings/security/rate-limit',
          '/system-settings/security/ssrf',
          '/system-settings/security/token-limits',
        ],
      },
      {
        title: 'Console Content',
        urls: [
          '/system-settings/content/dashboard',
          '/system-settings/content/announcements',
          '/system-settings/content/api-info',
          '/system-settings/content/faq',
          '/system-settings/content/uptime-kuma',
          '/system-settings/content/chat',
          '/system-settings/content/drawing',
        ],
      },
      {
        title: 'Operations',
        urls: [
          '/system-settings/operations/behavior',
          '/system-settings/operations/alerts',
          '/system-settings/operations/email',
          '/system-settings/operations/worker',
          '/system-settings/operations/logs',
          '/system-settings/operations/performance',
          '/system-settings/operations/update-checker',
        ],
      },
    ])
  })

  it('shows the model pricing badge on the billing group and its section', () => {
    const billing = getItems({ modelPricing: '3' })[2]

    expect(billing.badge).toBe('3')
    expect(
      'items' in billing
        ? billing.items?.find((sub) => sub.url?.endsWith('/model-pricing'))
            ?.badge
        : undefined
    ).toBe('3')
  })
})
