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
import { Toaster } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { ClientImportTarget } from '../../../types'
import { ClientImportDialog } from '../client-import-dialog'

let queryClient: QueryClient

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
})

afterEach(() => {
  queryClient.clear()
})

function renderDialog(
  models = ['gpt-5.4', 'claude-sonnet-4-6'],
  target: ClientImportTarget = 'cc-switch',
  tokenKey = 'test-only'
) {
  queryClient.setQueryData(['user-models-ccswitch'], {
    success: true,
    data: models,
  })
  render(
    <QueryClientProvider client={queryClient}>
      <ClientImportDialog
        target={target}
        open
        onOpenChange={vi.fn()}
        tokenKey={tokenKey}
      />
      <Toaster />
    </QueryClientProvider>
  )
}

describe('CC Switch model selection', () => {
  it.each(['Claude', 'Codex', 'Gemini'])(
    'opens %s models outside the clipping dialog and keeps the dialog open after selection',
    async (app) => {
      renderDialog()
      const user = userEvent.setup()
      await user.click(screen.getByRole('radio', { name: app }))
      const input = screen.getByRole('combobox', { name: 'Primary Model' })

      await user.click(input)

      expect(input).toHaveAttribute('aria-expanded', 'true')
      const list = await screen.findByRole('listbox')
      const dialog = screen.getByRole('dialog', { name: 'Import to CC Switch' })
      // The dialog is translated and clips overflow. Its popup must escape
      // that containing block to remain aligned and fully visible.
      expect(dialog).not.toContainElement(list)
      await user.click(screen.getByRole('option', { name: 'gpt-5.4' }))
      await waitFor(() => expect(input).toHaveValue('gpt-5.4'))
      expect(input).toHaveAttribute('aria-expanded', 'false')
      expect(dialog).toBeVisible()
    }
  )

  it('filters model names and supports keyboard selection and Escape without closing the dialog', async () => {
    renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    const input = screen.getByRole('combobox', { name: 'Primary Model' })
    await user.click(input)
    await user.type(input, 'sonnet')

    expect(
      screen.queryByRole('option', { name: 'gpt-5.4' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('option', { name: 'claude-sonnet-4-6' })
    ).toBeVisible()
    await user.keyboard('{ArrowDown}{Enter}')
    await waitFor(() => expect(input).toHaveValue('claude-sonnet-4-6'))
    await user.click(input)
    await user.keyboard('{Escape}')

    expect(input).toHaveValue('claude-sonnet-4-6')
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByRole('dialog')).toBeVisible()
  })

  it('shows an empty result when no models are available and updates an open dropdown when models arrive', async () => {
    renderDialog([])
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    const input = screen.getByRole('combobox', { name: 'Primary Model' })
    await user.click(screen.getByRole('button', { name: 'Primary Model' }))

    expect(await screen.findByText('No models found')).toBeVisible()
    await act(async () => {
      queryClient.setQueryData(['user-models-ccswitch'], {
        success: true,
        data: ['gpt-5.4'],
      })
    })
    await user.click(await screen.findByRole('option', { name: 'gpt-5.4' }))
    await waitFor(() => expect(input).toHaveValue('gpt-5.4'))
  })

  it('lets users edit the provider name without opening a model dropdown', async () => {
    renderDialog()
    const user = userEvent.setup()
    const input = screen.getByRole('textbox', { name: 'Name' })

    await user.clear(input)
    await user.type(input, 'Development')

    expect(input).toHaveValue('Development')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })
})

type UsageScriptConfig = {
  request: { url: string; method: string; headers: Record<string, string> }
  extractor: (response: unknown) => Record<string, unknown>
}

const SITE = 'https://api.example.com'
const USAGE_SWITCH = { name: 'Also configure usage query' }

describe('CC Switch usage query', () => {
  let openSpy: ReturnType<typeof vi.spyOn>

  function setSite(serverAddress: string) {
    localStorage.setItem(
      'status',
      JSON.stringify({ server_address: serverAddress })
    )
  }

  function setCurrency(currency: Partial<typeof DEFAULT_CURRENCY_CONFIG>) {
    useSystemConfigStore.getState().setConfig({
      currency: { ...DEFAULT_CURRENCY_CONFIG, ...currency },
    })
  }

  beforeEach(() => {
    setSite(SITE)
    setCurrency({ quotaPerUnit: 1000000 })
    openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  afterEach(() => {
    vi.useRealTimers()
    openSpy.mockRestore()
    localStorage.removeItem('status')
    setCurrency({})
  })

  async function submitImport(options: { app?: string; usage?: boolean }) {
    renderDialog()
    const user = userEvent.setup()
    if (options.app) {
      await user.click(screen.getByRole('radio', { name: options.app }))
    }
    if (options.usage === false) {
      await user.click(screen.getByRole('switch', USAGE_SWITCH))
    }
    const input = screen.getByRole('combobox', { name: 'Primary Model' })
    await user.click(input)
    await user.click(await screen.findByRole('option', { name: 'gpt-5.4' }))
    await user.click(screen.getByRole('button', { name: 'Open CC Switch' }))
    expect(openSpy).toHaveBeenCalledTimes(1)
    return new URL(String(openSpy.mock.calls[0][0]))
  }

  function usageParamsOf(link: URL): string[] {
    return [...link.searchParams.keys()].filter((key) =>
      key.startsWith('usage')
    )
  }

  function loadUsageScript(link: URL): UsageScriptConfig {
    const script = atob(link.searchParams.get('usageScript') ?? '')
    const withVars = script
      .replaceAll('{{apiKey}}', 'sk-test-only')
      .replaceAll('{{baseUrl}}', link.searchParams.get('endpoint') ?? '')
    // CC Switch evaluates the script as a single expression in QuickJS.
    // eslint-disable-next-line no-new-func
    return new Function(`return ${withVars}`)() as UsageScriptConfig
  }

  const limitedToken = {
    name: 'dev',
    unlimited_quota: false,
    total_granted: 3000000,
    total_used: 1000000,
    total_available: 2000000,
    expires_at: 0,
  }

  it('turns the usage query switch on by default with a description', () => {
    renderDialog()

    const toggle = screen.getByRole('switch', USAGE_SWITCH)

    expect(toggle).toBeChecked()
    expect(toggle).not.toHaveAttribute('aria-disabled', 'true')
    expect(toggle).toHaveAccessibleDescription(
      'Lets CC Switch show the remaining quota after import.'
    )
  })

  it('adds an ASCII usage script without the real API key when the switch is on', async () => {
    const link = await submitImport({})

    const script = atob(link.searchParams.get('usageScript') ?? '')
    expect(script).toContain('{{apiKey}}')
    expect(script).not.toContain('test-only')
    expect(/^[\t\n\x20-\x7e]*$/.test(script)).toBe(true)
  })

  it('keeps the link free of usage parameters when the switch is turned off', async () => {
    const link = await submitImport({ usage: false })

    expect(usageParamsOf(link)).toEqual([])
    expect(link.searchParams.get('apiKey')).toBe('sk-test-only')
  })

  it('turns the switch back on when the dialog is closed and reopened', async () => {
    queryClient.setQueryData(['user-models-ccswitch'], {
      success: true,
      data: ['gpt-5.4'],
    })
    const view = (open: boolean) => (
      <QueryClientProvider client={queryClient}>
        <ClientImportDialog
          target='cc-switch'
          open={open}
          onOpenChange={vi.fn()}
          tokenKey='k'
        />
      </QueryClientProvider>
    )
    const { rerender } = render(view(true))
    const user = userEvent.setup()
    await user.click(screen.getByRole('switch', USAGE_SWITCH))
    expect(screen.getByRole('switch', USAGE_SWITCH)).not.toBeChecked()

    rerender(view(false))
    await waitFor(() =>
      expect(screen.queryByRole('switch', USAGE_SWITCH)).toBeNull()
    )
    rerender(view(true))

    expect(await screen.findByRole('switch', USAGE_SWITCH)).toBeChecked()
  })

  it('disables the switch and omits usage parameters for a non-HTTPS site', async () => {
    setSite('http://api.example.com')

    const link = await submitImport({})

    const toggle = screen.getByRole('switch', USAGE_SWITCH)
    expect(toggle).not.toBeChecked()
    expect(toggle).toHaveAttribute('aria-disabled', 'true')
    expect(toggle).toHaveAccessibleDescription(
      'CC Switch can only query usage for HTTPS sites.'
    )
    expect(usageParamsOf(link)).toEqual([])
  })

  it('keeps the usage query available for a loopback HTTP site', async () => {
    setSite('http://localhost:3000')

    const link = await submitImport({})

    expect(screen.getByRole('switch', USAGE_SWITCH)).not.toHaveAttribute(
      'aria-disabled',
      'true'
    )
    expect(loadUsageScript(link).request.url).toBe(
      'http://localhost:3000/api/usage/token/'
    )
  })

  it.each([
    [
      'Codex with its primary model',
      'Codex',
      [['Primary Model', 'gpt-5.4']],
      [
        ['resource', 'provider'],
        ['app', 'codex'],
        ['name', 'My Codex'],
        ['endpoint', `${SITE}/v1`],
        ['apiKey', 'sk-test-only'],
        ['model', 'gpt-5.4'],
        ['homepage', SITE],
        ['enabled', 'true'],
        ['usageScript', 'script'],
      ],
    ],
    [
      'Claude with all four models',
      'Claude',
      [
        ['Primary Model', 'claude-sonnet-4-6'],
        ['Haiku Model', 'gpt-5.4'],
        ['Sonnet Model', 'claude-sonnet-4-6'],
        ['Opus Model', 'gpt-5.4'],
      ],
      [
        ['resource', 'provider'],
        ['app', 'claude'],
        ['name', 'My Claude'],
        ['endpoint', SITE],
        ['apiKey', 'sk-test-only'],
        ['model', 'claude-sonnet-4-6'],
        ['haikuModel', 'gpt-5.4'],
        ['sonnetModel', 'claude-sonnet-4-6'],
        ['opusModel', 'gpt-5.4'],
        ['homepage', SITE],
        ['enabled', 'true'],
        ['usageScript', 'script'],
      ],
    ],
  ])(
    'builds the complete CC Switch parameter list for %s',
    async (_case, app, selections, expected) => {
      renderDialog()
      const user = userEvent.setup()
      await user.click(screen.getByRole('radio', { name: app }))
      for (const [field, model] of selections) {
        await user.click(screen.getByRole('combobox', { name: field }))
        await user.click(await screen.findByRole('option', { name: model }))
      }

      await user.click(screen.getByRole('button', { name: 'Open CC Switch' }))

      const raw = String(openSpy.mock.calls[0][0])
      const link = new URL(raw)
      expect(raw.startsWith('ccswitch://v1/import?')).toBe(true)
      expect(raw).toContain(`name=My+${app}`)
      expect(
        [...link.searchParams.entries()].map(([key, value]) => [
          key,
          key === 'usageScript' ? 'script' : value,
        ])
      ).toEqual(expected)
      expect(atob(link.searchParams.get('usageScript') ?? '')).toContain(
        '/api/usage/token/'
      )
    }
  )

  it.each([
    ['Claude', SITE],
    ['Codex', `${SITE}/v1`],
    ['Gemini', SITE],
  ])(
    'requests the site root usage endpoint for %s with the provider key',
    async (app, endpoint) => {
      const link = await submitImport({ app })
      expect(link.searchParams.get('endpoint')).toBe(endpoint)

      const config = loadUsageScript(link)

      expect(config.request.url).toBe(`${SITE}/api/usage/token/`)
      expect(config.request.method).toBe('GET')
      expect(config.request.headers.Authorization).toBe('Bearer sk-test-only')
    }
  )

  it.each([
    [
      'a limited token reports its own quota',
      limitedToken,
      {
        isValid: true,
        unit: 'USD',
        planName: 'dev',
        remaining: 2,
        used: 1,
        total: 3,
      },
    ],
    [
      'an unlimited token with user quota reports the account balance',
      {
        name: 'dev',
        unlimited_quota: true,
        total_granted: 500000,
        total_used: 500000,
        total_available: 0,
        expires_at: 0,
        user_quota: { total_available: 4500000, total_used: 1500000 },
      },
      {
        isValid: true,
        unit: 'USD',
        planName: 'dev',
        remaining: 4.5,
        used: 1.5,
        total: 6,
      },
    ],
    [
      'an unlimited token without user quota omits the remaining balance',
      {
        name: 'dev',
        unlimited_quota: true,
        total_granted: 500000,
        total_used: 500000,
        total_available: 0,
        expires_at: 0,
      },
      { isValid: true, unit: 'USD', planName: 'dev', used: 0.5 },
    ],
  ])('extracts usage when %s', async (_case, data, expected) => {
    const link = await submitImport({})
    const config = loadUsageScript(link)

    const result = config.extractor({ code: true, message: 'ok', data })

    expect(result).toEqual(expected)
  })

  it.each([0, undefined])(
    'falls back to 500000 quota per USD when the site value is %s',
    async (quotaPerUnit) => {
      setCurrency({ quotaPerUnit: quotaPerUnit as number })
      const link = await submitImport({})

      const result = loadUsageScript(link).extractor({
        code: true,
        data: limitedToken,
      })

      expect(result).toMatchObject({ remaining: 4, used: 2, total: 6 })
    }
  )

  it('converts amounts to CNY with the USD exchange rate for a CNY site', async () => {
    setCurrency({
      quotaPerUnit: 1000000,
      quotaDisplayType: 'CNY',
      usdExchangeRate: 7,
    })
    const link = await submitImport({})

    const result = loadUsageScript(link).extractor({
      code: true,
      data: limitedToken,
    })

    expect(result).toMatchObject({
      unit: 'CNY',
      remaining: 14,
      used: 7,
      total: 21,
    })
  })

  it('reports an expired API key as invalid', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-06-01T00:00:00Z'))
    const link = await submitImport({})
    const expiresAt = Date.parse('2026-05-31T00:00:00Z') / 1000

    const result = loadUsageScript(link).extractor({
      code: true,
      data: { ...limitedToken, expires_at: expiresAt },
    })

    expect(result).toEqual({
      isValid: false,
      invalidMessage: 'API key has expired',
    })
  })

  it('keeps an API key valid before its expiry time', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-06-01T00:00:00Z'))
    const link = await submitImport({})
    const expiresAt = Date.parse('2026-06-02T00:00:00Z') / 1000

    const result = loadUsageScript(link).extractor({
      code: true,
      data: { ...limitedToken, expires_at: expiresAt },
    })

    expect(result).toMatchObject({ isValid: true, remaining: 2 })
  })

  it.each([
    [
      'the server message',
      {
        success: false,
        message: 'Failed to get token info, please try again later',
      },
      'Failed to get token info, please try again later',
    ],
    ['a fixed message', { success: false }, 'Failed to query API key usage'],
  ])(
    'reports an invalid result with %s when the query fails',
    async (_case, response, message) => {
      const link = await submitImport({})
      const config = loadUsageScript(link)

      expect(config.extractor(response)).toEqual({
        isValid: false,
        invalidMessage: message,
      })
    }
  )
})

describe('client imports without CC Switch usage query', () => {
  let openSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    localStorage.setItem('status', JSON.stringify({ server_address: SITE }))
    openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  afterEach(() => {
    openSpy.mockRestore()
    localStorage.removeItem('status')
  })

  function openedLink(): URL {
    expect(openSpy).toHaveBeenCalledTimes(1)
    return new URL(String(openSpy.mock.calls[0][0]))
  }

  function rawOpenedLink(): string {
    expect(openSpy).toHaveBeenCalledTimes(1)
    return String(openSpy.mock.calls[0][0])
  }

  describe('Codex++', () => {
    it.each([
      ['Claude', 'My Claude', 'anthropic'],
      ['Codex', 'My Codex', 'responses'],
      ['Gemini', 'My Gemini', 'chat'],
    ])(
      'imports a %s provider into the pool with the matching wire API',
      async (app, name, wireApi) => {
        renderDialog(undefined, 'codex-plus-plus')
        const user = userEvent.setup()
        await user.click(screen.getByRole('radio', { name: app }))

        await user.click(screen.getByRole('button', { name: 'Open Codex++' }))

        const link = openedLink()
        expect(`${link.protocol}//${link.host}${link.pathname}`).toBe(
          'codexplusplus://v1/import/provider'
        )
        expect([...link.searchParams.entries()]).toEqual([
          ['name', name],
          ['baseUrl', `${SITE}/v1`],
          ['apiKey', 'sk-test-only'],
          ['wireApi', wireApi],
          ['target', 'pool'],
        ])
      }
    )

    it('shows the three applications without model fields or the usage query switch', () => {
      renderDialog(undefined, 'codex-plus-plus')

      const dialog = screen.getByRole('dialog', { name: 'Import to Codex++' })
      expect(within(dialog).getAllByRole('radio')).toEqual(
        ['Claude', 'Codex', 'Gemini'].map((name) =>
          within(dialog).getByRole('radio', { name })
        )
      )
      expect(within(dialog).queryByRole('combobox')).not.toBeInTheDocument()
      expect(
        within(dialog).queryByRole('switch', USAGE_SWITCH)
      ).not.toBeInTheDocument()
    })

    it('encodes spaces in the name as %20 in the raw link', async () => {
      renderDialog(undefined, 'codex-plus-plus')

      await userEvent.click(
        screen.getByRole('button', { name: 'Open Codex++' })
      )

      const raw = rawOpenedLink()
      expect(raw).toContain('name=My%20Claude&')
      expect(raw).not.toContain('+')
    })

    it('does not request the user model list when the dialog opens', async () => {
      const get = vi.spyOn(api, 'get')
      render(
        <QueryClientProvider client={queryClient}>
          <ClientImportDialog
            target='codex-plus-plus'
            open
            onOpenChange={vi.fn()}
            tokenKey='test-only'
          />
        </QueryClientProvider>
      )

      expect(
        await screen.findByRole('dialog', { name: 'Import to Codex++' })
      ).toBeVisible()
      expect(get).not.toHaveBeenCalledWith('/api/user/models')
      get.mockRestore()
    })

    it('keeps a key that already has the sk- prefix unchanged', async () => {
      renderDialog(undefined, 'codex-plus-plus', 'sk-test-only')

      await userEvent.click(
        screen.getByRole('button', { name: 'Open Codex++' })
      )

      expect(openedLink().searchParams.get('apiKey')).toBe('sk-test-only')
    })
  })

  describe('AiMaMi', () => {
    it('imports a Codex provider with the selected primary model', async () => {
      renderDialog(undefined, 'aimami')
      const user = userEvent.setup()
      await user.click(screen.getByRole('combobox', { name: 'Primary Model' }))
      await user.click(await screen.findByRole('option', { name: 'gpt-5.4' }))

      await user.click(screen.getByRole('button', { name: 'Open AiMaMi' }))

      const link = openedLink()
      expect(`${link.protocol}//${link.host}${link.pathname}`).toBe(
        'aimami://v1/import'
      )
      expect([...link.searchParams.entries()]).toEqual([
        ['resource', 'provider'],
        ['app', 'codex'],
        ['name', 'My Codex'],
        ['endpoint', `${SITE}/v1`],
        ['apiKey', 'sk-test-only'],
        ['model', 'gpt-5.4'],
      ])
    })

    it('encodes spaces in the name as %20 in the raw link', async () => {
      renderDialog(undefined, 'aimami')
      const user = userEvent.setup()
      await user.click(screen.getByRole('combobox', { name: 'Primary Model' }))
      await user.click(await screen.findByRole('option', { name: 'gpt-5.4' }))

      await user.click(screen.getByRole('button', { name: 'Open AiMaMi' }))

      const raw = rawOpenedLink()
      expect(raw).toContain('name=My%20Codex&')
      expect(raw).not.toContain('+')
    })

    it('offers only Codex with a single primary model field and no usage query switch', () => {
      renderDialog(undefined, 'aimami')

      const dialog = screen.getByRole('dialog', { name: 'Import to AiMaMi' })
      const radios = within(dialog).getAllByRole('radio')
      expect(radios).toHaveLength(1)
      expect(within(dialog).getByRole('radio', { name: 'Codex' })).toBeChecked()
      expect(within(dialog).getAllByRole('combobox')).toEqual([
        within(dialog).getByRole('combobox', { name: 'Primary Model' }),
      ])
      expect(
        within(dialog).queryByRole('switch', USAGE_SWITCH)
      ).not.toBeInTheDocument()
    })

    it('warns and does not open AiMaMi when no primary model is selected', async () => {
      renderDialog(undefined, 'aimami')

      await userEvent.click(screen.getByRole('button', { name: 'Open AiMaMi' }))

      expect(
        await screen.findByText('Please select a primary model')
      ).toBeVisible()
      expect(openSpy).not.toHaveBeenCalled()
      expect(
        screen.getByRole('dialog', { name: 'Import to AiMaMi' })
      ).toBeVisible()
    })
  })
})
