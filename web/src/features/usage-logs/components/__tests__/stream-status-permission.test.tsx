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
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { after, describe, test } from 'node:test'

const isolatedRun = process.env.USAGE_LOG_STREAM_STATUS_TEST_ISOLATED === '1'

if (!isolatedRun) {
  describe('usage log stream status permission isolation', () => {
    test('verifies the permission boundary in a disposable Bun process', () => {
      const child = spawnSync(
        process.execPath,
        [
          'test',
          'src/features/usage-logs/components/__tests__/stream-status-permission.test.tsx',
        ],
        {
          cwd: process.cwd(),
          env: {
            ...process.env,
            USAGE_LOG_STREAM_STATUS_TEST_ISOLATED: '1',
          },
          encoding: 'utf8',
          timeout: 15_000,
        }
      )
      const output = child.stdout + child.stderr

      assert.equal(child.status, 0, output)
      assert.equal(
        output.includes(
          'Detected multiple renderers concurrently rendering the same context provider'
        ),
        false,
        output
      )
    })
  })
} else {
  const { Window } = await import('happy-dom')
  const domWindow = new Window()
  const domGlobals = [
    'window',
    'document',
    'navigator',
    'HTMLElement',
    'HTMLButtonElement',
    'SVGElement',
    'Node',
    'Element',
    'Event',
    'KeyboardEvent',
    'PointerEvent',
    'MouseEvent',
    'FocusEvent',
    'CustomEvent',
    'MutationObserver',
    'ResizeObserver',
    'requestAnimationFrame',
    'cancelAnimationFrame',
    'getComputedStyle',
  ] as const

  for (const key of domGlobals) {
    Object.defineProperty(globalThis, key, {
      configurable: true,
      value: domWindow[key],
    })
  }

  const { act } = await import('react')
  const { createRoot } = await import('react-dom/client')
  const { createInstance } = await import('i18next')
  const { I18nextProvider, initReactI18next } = await import('react-i18next')
  const { DetailsDialog } = await import('../dialogs/details-dialog')

  const i18n = createInstance()
  await i18n.use(initReactI18next).init({
    lng: 'en',
    resources: { en: { translation: {} } },
  })

  const reactTestGlobals = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

  const log: React.ComponentProps<typeof DetailsDialog>['log'] = {
    id: 1,
    user_id: 2,
    created_at: 1_700_000_000,
    type: 2,
    content: 'request completed with a stream interruption',
    username: 'user',
    token_name: 'token',
    model_name: 'model',
    quota: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 1,
    is_stream: true,
    channel: 1,
    channel_name: 'channel',
    token_id: 3,
    group: 'default',
    ip: '',
    other: JSON.stringify({
      stream_status: {
        status: 'interrupted',
        end_reason: 'client_disconnect',
        error_count: 2,
        end_error: 'UPSTREAM_SECRET_DIAGNOSTIC',
        errors: ['PARSER_SECRET_DIAGNOSTIC'],
      },
    }),
    request_id: 'request-id',
    upstream_request_id: '',
  }

  async function renderDetails(isAdmin: boolean) {
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <DetailsDialog
            log={log}
            isAdmin={isAdmin}
            open
            onOpenChange={() => undefined}
          />
        </I18nextProvider>
      )
    })

    return { host, root }
  }

  async function unmountDetails(
    rendered: Awaited<ReturnType<typeof renderDetails>>
  ) {
    await act(async () => rendered.root.unmount())
    rendered.host.remove()
  }

  describe('usage log stream status permissions', () => {
    after(() => {
      domWindow.close()
    })

    test('keeps safe stream status visible but restricts raw diagnostics to admins', async () => {
      const userView = await renderDetails(false)
      const userText = document.body.textContent ?? ''

      assert.equal(userText.includes('Stream Status'), true)
      assert.equal(userText.includes('interrupted'), true)
      assert.equal(userText.includes('client_disconnect'), true)
      assert.equal(userText.includes('UPSTREAM_SECRET_DIAGNOSTIC'), false)
      assert.equal(userText.includes('PARSER_SECRET_DIAGNOSTIC'), false)

      await unmountDetails(userView)

      const adminView = await renderDetails(true)
      const adminText = document.body.textContent ?? ''

      assert.equal(adminText.includes('UPSTREAM_SECRET_DIAGNOSTIC'), true)
      assert.equal(adminText.includes('PARSER_SECRET_DIAGNOSTIC'), true)

      await unmountDetails(adminView)
    })
  })
}
