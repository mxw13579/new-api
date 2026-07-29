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
import { after } from 'node:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Window } from 'happy-dom'
import type { ChangeEvent, ReactNode } from 'react'
import type { Root } from 'react-dom/client'

import type { TopupInfo } from '../types'

const domWindow = new Window()
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

type MockModule = (
  specifier: string,
  factory: () => Record<string, unknown>
) => void
const mockModule = (
  (await import('bun:test')) as unknown as { mock: { module: MockModule } }
).mock.module

mockModule('react-i18next', () => ({
  I18nextProvider: (props: { children?: ReactNode }) => props.children,
  initReactI18next: {
    type: '3rdParty',
    init: () => undefined,
  },
  useTranslation: () => ({ t: (key: string) => key }),
}))

let topupInfo: TopupInfo = createTopupInfo(10)
const paymentCalculations: Array<[number, string]> = []
const calculatePaymentAmount = async (amount: number, paymentType: string) => {
  paymentCalculations.push([amount, paymentType])
  return 0
}

const walletHooks = await import('../hooks')
mockModule('../hooks', () => ({
  ...walletHooks,
  useTopupInfo: () => ({
    topupInfo,
    presetAmounts: [],
    loading: false,
    refetch: async () => undefined,
  }),
  usePayment: () => ({
    amount: 0,
    calculating: false,
    processing: false,
    calculatePaymentAmount,
    processPayment: async () => false,
  }),
  useAffiliate: () => ({
    affiliateLink: '',
    loading: false,
    transferQuota: async () => false,
    transferring: false,
  }),
  useRedemption: () => ({
    redeeming: false,
    redeemCode: async () => false,
  }),
  useCreemPayment: () => ({
    processing: false,
    processCreemPayment: async () => false,
  }),
  useWaffoPayment: () => ({
    processing: false,
    processWaffoPayment: async () => false,
  }),
  useWaffoPancakePayment: () => ({
    processing: false,
    processWaffoPancakePayment: async () => false,
  }),
}))

const statusHook = await import('@/hooks/use-status')
mockModule('@/hooks/use-status', () => ({
  ...statusHook,
  useStatus: () => ({ status: { price: 1 }, loading: false, error: null }),
}))

const systemConfigHook = await import('@/hooks/use-system-config')
mockModule('@/hooks/use-system-config', () => ({
  ...systemConfigHook,
  useSystemConfig: () => ({
    currency: { quotaDisplayType: 'USD', usdExchangeRate: 1 },
  }),
}))

const api = await import('@/lib/api')
mockModule('@/lib/api', () => ({
  ...api,
  getSelf: async () => ({ success: true, data: null }),
}))

mockModule('../components/recharge-form-card', () => ({
  RechargeFormCard: (props: {
    topupAmount: number
    onTopupAmountChange: (amount: number) => void
  }) => (
    <label>
      Custom Amount
      <input
        aria-label='Custom Amount'
        type='number'
        value={props.topupAmount}
        onChange={(event: ChangeEvent<HTMLInputElement>) =>
          props.onTopupAmountChange(Number(event.target.value))
        }
      />
    </label>
  ),
}))

for (const specifier of [
  '../components/affiliate-rewards-card',
  '../components/subscription-plans-card',
  '../components/wallet-stats-card',
  '../components/dialogs/billing-history-dialog',
  '../components/dialogs/creem-confirm-dialog',
  '../components/dialogs/payment-confirm-dialog',
  '../components/dialogs/transfer-dialog',
]) {
  const moduleName = specifier.split('/').at(-1)
  if (!moduleName) throw new Error(`Invalid component specifier: ${specifier}`)
  const exportName = moduleName
    .split('-')
    .map((part) => part[0].toUpperCase() + part.slice(1))
    .join('')
  mockModule(specifier, () => ({ [exportName]: () => null }))
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { Wallet } = await import('../index')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function createTopupInfo(minTopup: number): TopupInfo {
  return {
    enable_online_topup: true,
    enable_stripe_topup: false,
    pay_methods: [{ name: 'Stripe', type: 'stripe' }],
    min_topup: minTopup,
    stripe_min_topup: minTopup,
    amount_options: [],
    discount: {},
  }
}

function renderWallet(root: Root, queryClient: QueryClient): void {
  root.render(
    <QueryClientProvider client={queryClient}>
      <Wallet />
    </QueryClientProvider>
  )
}

function changeInputValue(input: HTMLInputElement, value: string): void {
  const valueSetter = Object.getOwnPropertyDescriptor(
    domWindow.HTMLInputElement.prototype,
    'value'
  )?.set
  if (!valueSetter) throw new Error('HTMLInputElement value setter is required')
  valueSetter.call(input, value)
  input.dispatchEvent(
    new domWindow.Event('input', { bubbles: true }) as unknown as Event
  )
}

describe('Wallet topup initialization', () => {
  after(() => {
    domWindow.close()
  })

  it('initializes once and preserves a user-edited amount when topup info refreshes', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const queryClient = new QueryClient()

    await act(async () => renderWallet(root, queryClient))

    const amountInput = container.querySelector<HTMLInputElement>(
      'input[aria-label="Custom Amount"]'
    )
    if (!amountInput) throw new Error('Custom Amount input must render')
    expect(amountInput.value).toBe('10')
    expect(paymentCalculations).toEqual([[10, 'stripe']])

    await act(async () => changeInputValue(amountInput, '75'))
    expect(amountInput.value).toBe('75')
    expect(paymentCalculations).toEqual([
      [10, 'stripe'],
      [75, 'stripe'],
    ])

    topupInfo = createTopupInfo(25)
    await act(async () => renderWallet(root, queryClient))

    expect(amountInput.value).toBe('75')
    expect(paymentCalculations).toEqual([
      [10, 'stripe'],
      [75, 'stripe'],
    ])

    await act(async () => root.unmount())
    queryClient.clear()
    container.remove()
  })
})
