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
import { expect, test, type Page, type Route } from '@playwright/test'

const now = 1_900_000_000

function success(data: unknown) {
  return { success: true, message: '', data }
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(data),
  })
}

async function installWalletBackend(page: Page) {
  let feeLedgerRequests = 0
  const user = {
    id: 7,
    username: 'wallet-invoice-e2e',
    display_name: 'Wallet Invoice E2E',
    role: 1,
    group: 'default',
    quota: 2_000_000,
    used_quota: 500_000,
    request_count: 3,
    aff_quota: 0,
    aff_history_quota: 0,
    aff_count: 0,
    language: 'en',
    permissions: { sidebar_settings: false },
  }

  await page.addInitScript(() => {
    window.localStorage.setItem('setup_status_checked', 'true')
    window.localStorage.setItem('i18nextLng', 'en')
  })
  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/user/auth/refresh') {
      await fulfill(
        route,
        success({
          access_token: 'wallet-e2e-token',
          token_type: 'Bearer',
          access_expires_at: now + 86_400,
          user,
          session: {
            sid: 'wallet-invoice-e2e-session',
            current: true,
            login_method: 'e2e',
            ip: '127.0.0.1',
            user_agent: 'playwright',
            created_at: now - 60,
            last_active_at: now,
            expires_at: now + 86_400,
          },
        })
      )
      return
    }
    if (path === '/api/status') {
      await fulfill(
        route,
        success({
          system_name: 'Wallet Invoice E2E',
          price: 1,
          quota_per_unit: 500_000,
          display_type: 'USD',
        })
      )
      return
    }
    if (path === '/api/notice') {
      await fulfill(route, success(''))
      return
    }
    if (path === '/api/setup') {
      await fulfill(route, success({ status: true, root_init: true }))
      return
    }
    if (path === '/api/user/self') {
      await fulfill(route, success(user))
      return
    }
    if (path === '/api/user/topup/info') {
      await fulfill(
        route,
        success({
          enable_online_topup: false,
          enable_stripe_topup: false,
          pay_methods: [],
          min_topup: 1,
          stripe_min_topup: 1,
          amount_options: [],
          discount: {},
          enable_redemption: false,
        })
      )
      return
    }
    if (path === '/api/user/topup/self') {
      await fulfill(route, success({ items: [], total: 0 }))
      return
    }
    if (path === '/api/user/invoice/fee-ledger') {
      feeLedgerRequests += 1
      await fulfill(
        route,
        success({
          items: [
            {
              id: 1,
              application_id: 9,
              application_no: 'INV-FEE-E2E',
              entry_type: 'charge',
              fee_percent: 5,
              quota: 500_000,
              balance_before: 2_000_000,
              balance_after: 1_500_000,
              status: 'applied',
              applied_at: now - 60,
            },
          ],
          page: 1,
          page_size: 10,
          total: 1,
        })
      )
      return
    }
    await fulfill(route, success(null))
  })

  return { feeLedgerRequests: () => feeLedgerRequests }
}

test('wallet loads owner invoice fees only after the Invoice fees tab is selected', async ({
  page,
}) => {
  const backend = await installWalletBackend(page)
  await page.goto('/wallet')
  await page.getByRole('button', { name: 'Order History' }).click()

  await expect(page.getByRole('dialog')).toBeVisible()
  expect(backend.feeLedgerRequests()).toBe(0)
  await page.getByRole('tab', { name: 'Invoice fees' }).click()

  await expect.poll(backend.feeLedgerRequests).toBe(1)
  await expect(page.getByText('INV-FEE-E2E')).toBeVisible()
  await expect(page.getByText('Invoice fee charge')).toBeVisible()
  await expect(page.getByText('Invoice fee applied')).toBeVisible()
})
