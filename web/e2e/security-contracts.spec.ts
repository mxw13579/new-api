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

interface SecurityBackendOptions {
  chats: Array<Record<string, string>>
  footerHtml?: string
}

const now = Math.floor(Date.now() / 1000)

function success(data: unknown) {
  return { success: true, message: '', data }
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify(data),
  })
}

async function installSecurityBackend(
  page: Page,
  options: SecurityBackendOptions
) {
  const apiPaths: string[] = []
  await page.addInitScript((status) => {
    window.localStorage.setItem('setup_status_checked', 'true')
    window.localStorage.setItem('i18nextLng', 'en')
    window.localStorage.setItem('status', JSON.stringify(status))
  }, options)

  await page.route('**/pg/chat/completions', async (route) => {
    await fulfill(route, success({ choices: [{ message: { content: 'ok' } }] }))
  })

  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    apiPaths.push(path)
    if (path === '/api/user/auth/refresh') {
      await fulfill(
        route,
        success({
          access_token: 'security-access-token',
          token_type: 'Bearer',
          access_expires_at: now + 86_400,
          user: {
            id: 19,
            username: 'security-e2e',
            display_name: 'Security E2E',
            role: 1,
            group: 'default',
            quota: 100_000,
            language: 'en',
            permissions: { sidebar_settings: false },
          },
          session: {
            sid: 'security-session',
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
          system_name: 'Security E2E',
          announcements_enabled: false,
          chats: options.chats,
          footer_html: options.footerHtml,
        })
      )
      return
    }
    if (path === '/api/notice') {
      await fulfill(route, success(''))
      return
    }
    if (path === '/api/home_page_content') {
      await fulfill(route, success(''))
      return
    }
    if (path === '/api/token/') {
      await fulfill(route, success({ items: [{ id: 7, status: 1 }], total: 1 }))
      return
    }
    if (path === '/api/token/7/key') {
      await fulfill(route, success({ key: 'test-fake-chat-key' }))
      return
    }
    if (path === '/api/user/self/groups') {
      await fulfill(route, success({ default: { desc: 'Default', ratio: 1 } }))
      return
    }
    if (path === '/api/user/models') {
      await fulfill(route, success(['security-model']))
      return
    }
    await fulfill(route, success({}))
  })

  return apiPaths
}

test('invalid chat2link stays in a safe error state without key lookup or navigation', async ({
  page,
}) => {
  const apiPaths = await installSecurityBackend(page, {
    chats: [{ Unsafe: 'http://127.0.0.1:4179/leak?key={key}' }],
  })

  await page.goto('/chat2link')

  await expect(page).toHaveURL(/\/chat2link$/)
  await expect(page.getByText('Unable to open this URL safely.')).toBeVisible()
  expect(apiPaths.filter((path) => path.startsWith('/api/token'))).toEqual([])
  await expect(page.locator('iframe')).toHaveCount(0)
  await expect(page.locator('body')).not.toContainText('test-fake-chat-key')
})

test('an earlier attachment submission cannot clear newer prompt input', async ({
  page,
}) => {
  await page.addInitScript(() => {
    const readAsDataUrl = FileReader.prototype.readAsDataURL
    FileReader.prototype.readAsDataURL = function (blob): void {
      window.setTimeout(() => readAsDataUrl.call(this, blob), 500)
    }
  })
  await installSecurityBackend(page, { chats: [] })
  await page.goto('/playground')

  const prompt = page.getByPlaceholder('Ask anything')
  await expect(prompt).toBeVisible()
  await page.getByLabel('Upload files').setInputFiles({
    name: 'pending.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('pending attachment'),
  })
  await prompt.fill('submitted message')
  const submission = page.waitForRequest('**/pg/chat/completions')
  await page.getByRole('button', { name: 'Send' }).click()
  await prompt.fill('newer message')
  await submission

  await expect(prompt).toHaveValue('newer message', { timeout: 2_000 })
})

test('chat iframe is opaque, no-referrer, and keeps the key outside parent DOM', async ({
  page,
}) => {
  await installSecurityBackend(page, {
    chats: [{ AIAW: 'https://chat.example/?key={key}' }],
  })
  const externalRequests: Array<{ url: string; referer?: string }> = []
  await page.route('https://chat.example/**', async (route) => {
    externalRequests.push({
      url: route.request().url(),
      referer: route.request().headers().referer,
    })
    await route.abort()
  })

  await page.goto('/chat/0')
  const frame = page.locator('iframe[title="Chat preset: AIAW"]')
  await expect(frame).toHaveAttribute(
    'sandbox',
    'allow-scripts allow-forms allow-popups allow-presentation'
  )
  await expect(frame).toHaveAttribute('referrerpolicy', 'no-referrer')
  await expect(frame).not.toHaveAttribute('srcdoc', /.+/)
  await expect(frame).toHaveAttribute('allow', 'camera; microphone')
  await expect(page.locator('body')).not.toContainText('test-fake-chat-key')
  await expect.poll(() => externalRequests.length).toBeGreaterThan(0)
  expect(externalRequests[0]?.url).toContain('sk-test-fake-chat-key')
  expect(externalRequests[0]?.referer).toBeUndefined()
})

for (const presetName of ['Lobe', 'AIAW']) {
  test(`${presetName} external fallback validates before key-bearing navigation`, async ({
    page,
  }) => {
    const apiPaths = await installSecurityBackend(page, {
      chats: [{ [presetName]: 'https://chat.example/?key={key}' }],
    })
    const externalRequests: string[] = []
    await page.route('https://chat.example/**', async (route) => {
      externalRequests.push(route.request().url())
      await route.abort()
    })

    await page.goto('/chat2link')

    await expect.poll(() => externalRequests.length).toBe(1)
    expect(externalRequests[0]).toContain('key=sk-test-fake-chat-key')
    expect(apiPaths).toContain('/api/token/')
    expect(apiPaths).toContain('/api/token/7/key')
    await expect(page.locator('iframe')).toHaveCount(0)
    await expect(page.locator('body')).not.toContainText(
      'sk-test-fake-chat-key'
    )
  })
}

test('footer strips active content and preserves hardened safe links', async ({
  page,
}) => {
  await installSecurityBackend(page, {
    chats: [],
    footerHtml:
      '<svg><script>window.pwned=1</script></svg><math><mi>x</mi></math><form><input autofocus></form><a href="javascript:alert(1)" onclick="window.pwned=2">bad</a><strong>safe emphasis</strong><a id="safe-link" href="https://safe.example/" target="_blank">safe link</a>',
  })

  await page.goto('/')
  const footer = page.locator('footer')
  await expect(footer.getByText('safe emphasis')).toBeVisible()
  const customFooter = footer.locator('.custom-footer')
  await expect(
    customFooter.locator('svg, math, form, input, script')
  ).toHaveCount(0)
  await expect(customFooter.getByText('bad')).not.toHaveAttribute('href')
  await expect(customFooter.locator('#safe-link')).toHaveAttribute(
    'href',
    'https://safe.example/'
  )
  await expect(customFooter.locator('#safe-link')).toHaveAttribute(
    'rel',
    'noopener noreferrer'
  )
  expect(
    await page.evaluate(() => Reflect.get(window, 'pwned'))
  ).toBeUndefined()
})
