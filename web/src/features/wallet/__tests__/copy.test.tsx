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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { BillingHistoryDialog } from '@/features/wallet/components/dialogs/billing-history-dialog'
import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { RechargeFormCard } from '../components/recharge-form-card'

const i18n = createInstance()

beforeEach(async () => {
  await i18n.use(initReactI18next).init({
    lng: 'zh',
    fallbackLng: false,
    nsSeparator: false,
    resources: { en, zh },
    interpolation: { escapeValue: false },
  })
})

afterEach(() => {
  useAuthStore.getState().auth.reset()
})

it('loads owner invoice fee history only after selecting its tab and keeps top-up history accessible', async () => {
  await i18n.changeLanguage('en')
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (
      !url.startsWith('/api/user/topup/self?') &&
      !url.startsWith('/api/user/invoice/fee-ledger?')
    ) {
      throw new Error('Unexpected billing-history request')
    }
    return { data: { success: true, data: { total: 0, items: [] } } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <BillingHistoryDialog open onOpenChange={vi.fn()} />
      </I18nextProvider>
    </QueryClientProvider>
  )
  await screen.findByText('No billing records found')
  expect(
    get.mock.calls.some(([url]) => url.includes('/invoice/fee-ledger'))
  ).toBe(false)
  await user.click(screen.getByRole('tab', { name: 'Invoice fees' }))
  expect(await screen.findByText('No invoice fee records found')).toBeVisible()
  expect(
    get.mock.calls.some(([url]) =>
      url.startsWith('/api/user/invoice/fee-ledger?')
    )
  ).toBe(true)
  await user.click(screen.getByRole('tab', { name: 'Top-up history' }))
  expect(screen.getByText('No billing records found')).toBeVisible()
  expect(screen.getByRole('dialog')).toHaveClass(
    'max-h-(--dialog-available-height)'
  )
  client.clear()
})

async function renderPendingBillingHistory() {
  await i18n.changeLanguage('en')
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        total: 1,
        items: [
          {
            id: 1,
            status: 'pending',
            amount: 10,
            money: 10,
            trade_no: 'synthetic-order',
            payment_method: 'stripe',
            create_time: 1,
          },
        ],
      },
    },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <BillingHistoryDialog open onOpenChange={vi.fn()} />
    </I18nextProvider>
  )
  await screen.findByText('synthetic-order')
}

it('copies a top-up order number and displays confirmation on the copy button', async () => {
  const user = userEvent.setup()
  const writeText = vi
    .spyOn(navigator.clipboard, 'writeText')
    .mockResolvedValue()
  await renderPendingBillingHistory()
  await user.click(screen.getByRole('button', { name: 'Copy to clipboard' }))
  expect(writeText).toHaveBeenCalledWith('synthetic-order')
  expect(screen.getByRole('button', { name: 'Copied' })).toBeVisible()
})

it('retains manual completion confirmation while the request is pending or failed', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'operator', role: 10 })
  let finish!: (response: {
    data: { success: boolean; message: string }
  }) => void
  const response = new Promise<{ data: { success: boolean; message: string } }>(
    (resolve) => {
      finish = resolve
    }
  )
  const post = vi.spyOn(api, 'post').mockReturnValue(response)
  const user = userEvent.setup()
  await renderPendingBillingHistory()
  await user.click(screen.getByRole('button', { name: 'Complete Order' }))
  const confirmation = screen.getByRole('alertdialog')
  expect(post).not.toHaveBeenCalled()
  await user.click(
    within(confirmation).getByRole('button', { name: 'Confirm' })
  )
  expect(post).toHaveBeenCalledExactlyOnceWith('/api/user/topup/complete', {
    trade_no: 'synthetic-order',
  })
  expect(
    within(confirmation).getByRole('button', { name: 'Processing...' })
  ).toBeDisabled()
  expect(
    within(confirmation).getByRole('button', { name: 'Cancel' })
  ).toBeDisabled()
  await user.keyboard('{Escape}')
  expect(confirmation).toBeVisible()
  await act(async () =>
    finish({
      data: { success: false, message: 'Synthetic completion failure' },
    })
  )
  await waitFor(() =>
    expect(
      within(confirmation).getByRole('button', { name: 'Confirm' })
    ).toBeEnabled()
  )
  expect(confirmation).toBeVisible()
})

it('translates billing history statuses and updates them when the language changes', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        total: 3,
        items: ['success', 'pending', 'expired'].map((status, id) => ({
          id,
          status,
          amount: 10,
          money: 10,
          trade_no: `order-${id}`,
          payment_method: 'stripe',
          create_time: 1,
        })),
      },
    },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <BillingHistoryDialog open onOpenChange={vi.fn()} />
    </I18nextProvider>
  )
  await screen.findByText('order-0')
  for (const label of ['成功', '待确认', '已过期']) {
    expect(screen.getByText(label)).toBeVisible()
  }
  await act(() => i18n.changeLanguage('en'))
  for (const label of ['Success', 'Pending', 'Expired']) {
    expect(screen.getByText(label)).toBeVisible()
  }
})

it.each([
  { priceRatio: 7.3, paid: '584', saved: '146', full: '146' },
  { priceRatio: 0.000123, paid: '0.0098', saved: '0.0025', full: '0.0025' },
])(
  'translates recharge presets and minimum amount without changing precision at price ratio $priceRatio',
  async ({ priceRatio, paid, saved, full }) => {
    render(
      <I18nextProvider i18n={i18n}>
        <RechargeFormCard
          topupInfo={{
            enable_online_topup: true,
            enable_stripe_topup: false,
            pay_methods: [],
            min_topup: 1,
            stripe_min_topup: 1,
            amount_options: [100, 20],
            discount: {},
          }}
          presetAmounts={[{ value: 100, discount: 0.8 }, { value: 20 }]}
          selectedPreset={null}
          topupAmount={1}
          paymentAmount={priceRatio}
          priceRatio={priceRatio}
          calculating={false}
          paymentLoading={null}
          redemptionCode=''
          redeeming={false}
          onSelectPreset={vi.fn()}
          onTopupAmountChange={vi.fn()}
          onPaymentMethodSelect={vi.fn()}
          onRedemptionCodeChange={vi.fn()}
          onRedeem={vi.fn()}
        />
      </I18nextProvider>
    )
    expect(screen.getByRole('button', { name: /^100 / })).toHaveTextContent(
      `优惠 20%支付 ${paid} • 节省 ${saved}`
    )
    expect(screen.getByRole('button', { name: /^20 / })).toHaveTextContent(
      `支付 ${full}`
    )
    expect(screen.getByPlaceholderText('最低 1')).toBeVisible()
    await act(() => i18n.changeLanguage('en'))
    expect(screen.getByRole('button', { name: /^100 / })).toHaveTextContent(
      `20% OFFPay ${paid} • Save ${saved}`
    )
    expect(screen.getByRole('button', { name: /^20 / })).toHaveTextContent(
      `Pay ${full}`
    )
    expect(screen.getByPlaceholderText('Minimum 1')).toBeVisible()
  }
)
