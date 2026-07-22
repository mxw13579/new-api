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

type InvoiceScenario = 'normal' | 'empty' | 'error'

interface InvoiceBackendOptions {
  scenario?: InvoiceScenario
  createGate?: Promise<void>
  language?: 'en' | 'fr'
}

const now = 1_900_000_000

const profile = {
  id: 1,
  type: 'personal',
  title: 'Demo User',
  tax_number: '',
  is_default: true,
  version: 3,
  created_at: now - 10_000,
  updated_at: now - 100,
}

const order = {
  topup_id: 101,
  order_no: 'TOPUP-101',
  paid_amount_minor: 12_800,
  currency: 'CNY',
  product_description: 'API credit top-up',
  paid_at: now - 3_600,
}

function application(id: number, hold: boolean) {
  return {
    id,
    application_no: hold ? 'INV-HELD' : 'INV-READY',
    type: 'personal',
    status: 'issued',
    payment_review_status: hold ? 'post_issue_hold' : 'none',
    currency: 'CNY',
    amount_minor: 12_800,
    fee_quota: 500,
    fee_status: 'paid',
    submitted_at: now - id * 60,
    reviewed_at: now - 30,
    cancelled_at: null,
    issued_at: now - 20,
    reject_reason: '',
    document_status: 'available',
    document_expires_at: now + 86_400,
    document_deleted_at: null,
    can_cancel: false,
    can_download: !hold,
  }
}

function success(data: unknown) {
  return { success: true, message: '', data }
}

async function fulfill(route: Route, data: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data),
  })
}

async function installInvoiceBackend(
  page: Page,
  options: InvoiceBackendOptions = {}
) {
  const scenario = options.scenario || 'normal'
  let created = false

  await page.addInitScript((language) => {
    window.localStorage.setItem('setup_status_checked', 'true')
    if (!window.localStorage.getItem('i18nextLng')) {
      window.localStorage.setItem('i18nextLng', language)
    }
    window.localStorage.setItem(
      'status',
      JSON.stringify({
        system_name: 'Invoice E2E',
        announcements_enabled: false,
      })
    )
  }, options.language || 'en')

  await page.route('**/api/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/user/auth/refresh') {
      await fulfill(
        route,
        success({
          access_token: 'e2e-access-token',
          token_type: 'Bearer',
          access_expires_at: now + 86_400,
          user: {
            id: 7,
            username: 'invoice-e2e',
            display_name: 'Invoice E2E',
            role: 1,
            group: 'default',
            quota: 100_000,
            language: options.language || 'en',
            permissions: { sidebar_settings: false },
          },
          session: {
            sid: 'invoice-e2e-session',
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
          system_name: 'Invoice E2E',
          announcements_enabled: false,
          SidebarModulesAdmin: '',
        })
      )
      return
    }

    if (path === '/api/notice') {
      await fulfill(route, success(''))
      return
    }

    if (path === '/api/user/invoice/config') {
      if (scenario === 'error') {
        await fulfill(
          route,
          {
            success: false,
            message: 'test failure',
            data: { code: 'INVOICE_INVALID_REQUEST' },
          },
          400
        )
        return
      }
      await fulfill(
        route,
        success({
          personal_enabled: true,
          company_enabled: true,
          application_window_days: 90,
          minimum_amount_minor: 1_000,
          fee_quota: 500,
          pdf_retention_days: 365,
          currency: 'CNY',
        })
      )
      return
    }

    if (path === '/api/user/invoice/profiles') {
      await fulfill(route, success(scenario === 'empty' ? [] : [profile]))
      return
    }

    if (path === '/api/user/invoice/eligible-orders') {
      const items = scenario === 'empty' || created ? [] : [order]
      await fulfill(
        route,
        success({ items, page: 1, page_size: 100, total: items.length })
      )
      return
    }

    if (path === '/api/user/invoices' && request.method() === 'POST') {
      if (options.createGate) await options.createGate
      created = true
      const createdApplication = {
        ...application(3, false),
        application_no: 'INV-CREATED',
        status: 'submitted',
        document_status: 'uploading',
        can_download: false,
        profile_snapshot: { ...profile },
        policy_snapshot: {
          application_window_days: 90,
          minimum_amount_minor: 1_000,
          fee_quota: 500,
          pdf_retention_days: 365,
        },
        items: [order],
        issuance: null,
        document: null,
      }
      await fulfill(route, success(createdApplication))
      return
    }

    if (path === '/api/user/invoices') {
      const items =
        scenario === 'empty'
          ? []
          : [application(1, false), application(2, true)]
      await fulfill(
        route,
        success({ items, page: 1, page_size: 50, total: items.length })
      )
      return
    }

    if (path === '/api/setup') {
      await fulfill(route, success({ status: true, root_init: true }))
      return
    }

    await fulfill(route, success(null))
  })
}

test('desktop invoice route supports keyboard application flow and held download suppression', async ({
  page,
}) => {
  let releaseCreate = () => {}
  const createGate = new Promise<void>((resolve) => {
    releaseCreate = resolve
  })
  await installInvoiceBackend(page, { createGate })
  await page.goto('/invoices')

  await expect(
    page.getByRole('heading', { name: 'Invoices', exact: true })
  ).toBeVisible()
  await expect(
    page.getByRole('link', { name: 'Invoices', exact: true })
  ).toBeVisible()

  const applyTab = page.getByRole('tab', { name: 'Apply' })
  const profilesTab = page.getByRole('tab', { name: 'Profiles' })
  await applyTab.focus()
  await page.keyboard.press('ArrowRight')
  await expect(profilesTab).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(profilesTab).toHaveAttribute('aria-selected', 'true')
  await expect(
    page.getByRole('heading', { name: 'Invoice profiles' })
  ).toBeVisible()
  await page.keyboard.press('ArrowLeft')
  await expect(applyTab).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(applyTab).toHaveAttribute('aria-selected', 'true')

  await page.getByLabel('Invoice profile').selectOption('1')
  await page.getByRole('checkbox', { name: 'TOPUP-101' }).check()
  const reviewButton = page.getByRole('button', {
    name: 'Review invoice application',
  })
  await reviewButton.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('dialog')
  await expect(
    dialog.getByRole('heading', { name: 'Confirm invoice application' })
  ).toBeVisible()
  expect(
    await dialog.evaluate((element) => element.contains(document.activeElement))
  ).toBe(true)
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(reviewButton).toBeFocused()

  await reviewButton.click()
  const submitButton = page.getByRole('button', {
    name: 'Submit invoice application',
  })
  await submitButton.click()
  await expect(submitButton).toBeDisabled()
  releaseCreate()
  await expect(
    page.getByText('Invoice application submitted successfully')
  ).toBeVisible()

  await page.getByRole('tab', { name: 'History' }).click()
  await expect(page.getByText('INV-HELD')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Download PDF' })).toHaveCount(
    1
  )
  await expect(
    page.getByText('Invoice payment review post-issue hold')
  ).toBeVisible()
})

test('invoice route renders deterministic empty and error states', async ({
  page,
}) => {
  await installInvoiceBackend(page, { scenario: 'empty' })
  await page.goto('/invoices')
  await expect(page.getByText('No eligible orders')).toBeVisible()
  await page.getByRole('tab', { name: 'Profiles' }).click()
  await expect(page.getByText('No invoice profiles')).toBeVisible()
  await page.getByRole('tab', { name: 'History' }).click()
  await expect(page.getByText('No invoice applications')).toBeVisible()

  await page.unrouteAll({ behavior: 'wait' })
  await installInvoiceBackend(page, { scenario: 'error' })
  await page.reload()
  await expect(page.getByText('Invoice data failed to load')).toBeVisible()
})

test.describe('mobile invoice route', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('renders mobile navigation, drawer focus behavior, and long French layout', async ({
    page,
  }) => {
    await installInvoiceBackend(page)
    await page.goto('/invoices')

    await page.getByRole('button', { name: 'Toggle Sidebar' }).click()
    await expect(
      page.getByRole('link', { name: 'Invoices', exact: true })
    ).toBeVisible()
    await page.keyboard.press('Escape')

    await page.getByLabel('Invoice profile').selectOption('1')
    await page.getByRole('checkbox', { name: 'TOPUP-101' }).check()
    const reviewButton = page.getByRole('button', {
      name: 'Review invoice application',
    })
    await reviewButton.click()
    const drawer = page.getByRole('dialog')
    await expect(drawer.getByText('Confirm invoice application')).toBeVisible()
    expect(
      await drawer.evaluate((element) =>
        element.contains(document.activeElement)
      )
    ).toBe(true)
    await page.keyboard.press('Escape')
    await expect(drawer).toBeHidden()
    await expect(reviewButton).toBeFocused()

    await page.evaluate(() => window.localStorage.setItem('i18nextLng', 'fr'))
    await page.reload()
    await expect(
      page.getByRole('heading', { name: 'Factures', exact: true })
    ).toBeVisible()
    await expect(
      page.getByText(
        'Sélectionnez des commandes payées entières. Les montants partiels ne sont pas acceptés.'
      )
    ).toBeVisible()
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document.documentElement.scrollWidth <=
            document.documentElement.clientWidth
        )
      )
      .toBe(true)
  })
})
