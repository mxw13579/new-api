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

interface InvoicePermissions {
  review: boolean
  'document.upload': boolean
  'sensitive.read': boolean
  settings: boolean
}

interface AdminBackendOptions {
  permissions?: Partial<InvoicePermissions>
  role?: number
  language?: 'en' | 'ru'
  conflictReviewOnce?: boolean
  approvalFailureOnce?: boolean
  uploadFailureOnce?: boolean
  initialR2?: {
    endpoint: string
    bucket: string
    accessKeyId: string
    secretConfigured: boolean
  }
}

const now = 1_900_000_000

const defaultPermissions: InvoicePermissions = {
  review: true,
  'document.upload': true,
  'sensitive.read': true,
  settings: true,
}

function detail(
  id: number,
  status: 'submitted' | 'reviewing' | 'approved' | 'issued' | 'rejected'
) {
  const issued = status === 'issued'
  return {
    id,
    application_no: `ADMIN-INV-${id}`,
    type: 'company',
    status,
    payment_review_status: 'none',
    currency: 'CNY',
    amount_minor: 12_800,
    fee_quota: 500,
    fee_status: 'paid',
    submitted_at: now - id * 60,
    reviewed_at: status === 'submitted' ? null : now - 30,
    cancelled_at: null,
    issued_at: issued ? now - 10 : null,
    reject_reason: '',
    document_status: issued ? 'available' : 'missing',
    document_expires_at: issued ? now + 86_400 : null,
    document_deleted_at: null,
    can_cancel: false,
    can_download: issued,
    profile_snapshot: {
      type: 'company',
      title: 'Secret Company Ltd',
      tax_number: '91310000SECRET',
      version: 3,
    },
    policy_snapshot: {
      application_window_days: 90,
      minimum_amount_minor: 10_000,
      fee_percent: 5,
      fee_quota: 500,
      pdf_retention_days: 365,
    },
    items: [
      {
        topup_id: id * 10,
        order_no: `TOPUP-${id}`,
        paid_amount_minor: 12_800,
        currency: 'CNY',
        product_description: 'API credit top-up',
        paid_at: now - 3_600,
      },
    ],
    issuance: issued
      ? {
          id: 90 + id,
          invoice_number: 'LOCKED-NUMBER',
          invoice_code: 'LOCKED-CODE',
          invoice_date: 1_900_000_000,
          face_amount_minor: 12_800,
          currency: 'CNY',
        }
      : null,
    document: issued
      ? {
          id: 100 + id,
          status: 'available',
          expires_at: now + 86_400,
          deleted_at: null,
        }
      : null,
  }
}

function summary(application: ReturnType<typeof detail>) {
  const {
    profile_snapshot,
    policy_snapshot,
    items,
    issuance,
    document,
    ...item
  } = application
  void profile_snapshot
  void policy_snapshot
  void items
  void issuance
  void document
  return item
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

async function installAdminBackend(
  page: Page,
  options: AdminBackendOptions = {}
) {
  const permissions = { ...defaultPermissions, ...options.permissions }
  const applications = new Map([
    [7, detail(7, 'submitted')],
    [8, detail(8, 'reviewing')],
  ])
  let conflictPending = options.conflictReviewOnce ?? false
  let approvalFailurePending = options.approvalFailureOnce ?? false
  let uploadFailurePending = options.uploadFailureOnce ?? false
  let savedSetting = {
    personal_enabled: true,
    company_enabled: false,
    application_window_days: 90,
    minimum_amount_minor: 10_000,
    fee_percent: 5,
    pdf_retention_days: 365,
    r2_endpoint: options.initialR2?.endpoint ?? '',
    r2_bucket: options.initialR2?.bucket ?? '',
    r2_access_key_id: options.initialR2?.accessKeyId ?? '',
    r2_secret_configured: options.initialR2?.secretConfigured ?? false,
  }
  const settingWrites: unknown[] = []
  const uploadBodies: string[] = []
  const operationCalls: string[] = []

  await page.addInitScript((language) => {
    window.localStorage.setItem('setup_status_checked', 'true')
    window.localStorage.setItem('i18nextLng', language)
    window.localStorage.setItem(
      'status',
      JSON.stringify({
        system_name: 'Invoice Admin E2E',
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
          access_token: 'e2e-admin-token',
          token_type: 'Bearer',
          access_expires_at: now + 86_400,
          user: {
            id: 1,
            username: 'invoice-admin-e2e',
            display_name: 'Invoice Admin E2E',
            role: options.role ?? 10,
            group: 'default',
            quota: 100_000,
            language: options.language || 'en',
            permissions: {
              sidebar_settings: true,
              admin_permissions: { invoice: permissions },
            },
          },
          session: {
            sid: 'invoice-admin-session',
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
          system_name: 'Invoice Admin E2E',
          announcements_enabled: false,
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
    if (path === '/api/option/invoice') {
      if (request.method() === 'PUT') {
        const update = JSON.parse(request.postData() || '{}')
        settingWrites.push(update)
        const publicR2Empty =
          update.r2_endpoint === '' &&
          update.r2_bucket === '' &&
          update.r2_access_key_id === ''
        savedSetting = {
          ...update,
          r2_secret_configured: publicR2Empty
            ? false
            : Boolean(update.r2_secret_access_key) ||
              savedSetting.r2_secret_configured,
        }
        delete (savedSetting as { r2_secret_access_key?: string })
          .r2_secret_access_key
      }
      await fulfill(route, success(savedSetting))
      return
    }
    if (path === '/api/admin/invoices') {
      const items = [...applications.values()].map(summary)
      await fulfill(
        route,
        success({ items, page: 1, page_size: 20, total: items.length })
      )
      return
    }
    const detailMatch = path.match(/^\/api\/admin\/invoices\/(\d+)$/)
    if (detailMatch) {
      await fulfill(route, success(applications.get(Number(detailMatch[1]))))
      return
    }
    const actionMatch = path.match(
      /^\/api\/admin\/invoices\/(\d+)\/(review|reject|document)$/
    )
    if (actionMatch) {
      const id = Number(actionMatch[1])
      const action = actionMatch[2]
      const current = applications.get(id)
      if (!current) throw new Error(`missing fixture ${id}`)
      if (action === 'review' && conflictPending) {
        conflictPending = false
        applications.set(id, detail(id, 'reviewing'))
        await fulfill(
          route,
          {
            success: false,
            message: 'conflict',
            data: { code: 'INVOICE_STATE_CONFLICT' },
          },
          409
        )
        return
      }
      if (action === 'review') {
        const body = JSON.parse(request.postData() || '{}')
        operationCalls.push(
          `review:${id}:${body.action}:${body.expected_status}`
        )
        if (body.action === 'approve' && approvalFailurePending) {
          approvalFailurePending = false
          await fulfill(
            route,
            {
              success: false,
              message: 'approval failed',
              data: { code: 'INVOICE_STATE_CONFLICT' },
            },
            409
          )
          return
        }
        const nextStatus = body.action === 'approve' ? 'approved' : 'reviewing'
        applications.set(id, detail(id, nextStatus))
      } else if (action === 'reject') {
        applications.set(id, {
          ...detail(id, 'rejected'),
          reject_reason: JSON.parse(request.postData() || '{}').reason,
        })
      } else {
        operationCalls.push(`upload:${id}`)
        uploadBodies.push(
          (await request.postDataBuffer())?.toString('utf8') || ''
        )
        if (uploadFailurePending) {
          uploadFailurePending = false
          await fulfill(
            route,
            {
              success: false,
              message: 'upload failed',
              data: { code: 'INVOICE_STORAGE_NOT_CONFIGURED' },
            },
            503
          )
          return
        }
        applications.set(id, detail(id, 'issued'))
      }
      await fulfill(route, success(applications.get(id)))
      return
    }
    await fulfill(route, success(null))
  })

  return { settingWrites, uploadBodies, operationCalls }
}

async function fillIssuanceForm(page: Page): Promise<void> {
  await page.getByLabel('PDF document').setInputFiles({
    name: 'invoice.pdf',
    mimeType: 'application/pdf',
    buffer: Buffer.from('%PDF-1.7 e2e'),
  })
  await page.getByLabel('Invoice number').fill('E2E-NUMBER')
  await page.getByLabel('Invoice code').fill('E2E-CODE')
  await page.getByLabel('Invoice date').fill('2030-03-17')
  await page
    .getByRole('checkbox', {
      name: 'I confirm the PDF matches these issuance facts.',
    })
    .check()
}

test('admin routes enforce independent permissions and mask sensitive values in the DOM', async ({
  page,
}) => {
  await installAdminBackend(page, {
    permissions: {
      'document.upload': false,
      'sensitive.read': false,
      settings: false,
    },
  })
  await page.goto('/admin-invoices')
  await expect(
    page.getByRole('heading', { name: 'Invoice management' })
  ).toBeVisible()
  await expect(
    page.getByRole('link', { name: 'Invoice settings' })
  ).toHaveCount(0)
  await page.getByRole('button', { name: 'View details' }).first().click()
  await expect(
    page.getByRole('heading', { name: 'Invoice application details' })
  ).toBeVisible()
  await expect(page.getByText('Protected invoice value')).toHaveCount(2)
  await expect(page.locator('body')).not.toContainText('Secret Company Ltd')
  await expect(page.locator('body')).not.toContainText('91310000SECRET')
  await expect(page.getByRole('button', { name: 'Upload PDF' })).toHaveCount(0)

  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'View details' }).nth(1).click()
  await expect(
    page.getByRole('button', { name: 'Approve invoice' })
  ).toBeVisible()
  await expect(page.getByLabel('PDF document')).toHaveCount(0)

  await page.goto('/invoice-settings')
  await expect(page).toHaveURL(/\/403\/?$/)
})

test('admin review conflict refetches before approve/reject and strict PDF replacement', async ({
  page,
}) => {
  const backend = await installAdminBackend(page, { conflictReviewOnce: true })
  await page.goto('/admin-invoices')
  const details = page.getByRole('button', { name: 'View details' })

  await details.first().click()
  await page.getByRole('button', { name: 'Start review' }).click()
  await expect(
    page.getByText('Invoice error: data changed, refresh and try again')
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Approve invoice' })
  ).toHaveCount(0)
  await page.getByRole('button', { name: 'Approve and issue invoice' }).click()
  await expect(page.getByRole('alert')).toContainText(
    'Select a non-empty invoice PDF'
  )
  await fillIssuanceForm(page)
  await page.getByRole('button', { name: 'Approve and issue invoice' }).click()
  await expect(page.getByText('Invoice PDF saved')).toBeVisible()
  expect(backend.operationCalls).toEqual([
    'review:7:approve:reviewing',
    'upload:7',
  ])
  expect(backend.uploadBodies[0]).toContain('name="expected_status"')
  expect(backend.uploadBodies[0]).toContain('approved')
  await expect(page.getByLabel('Invoice number')).toBeDisabled()
  await expect(page.getByLabel('Invoice number')).toHaveValue('LOCKED-NUMBER')

  await page.keyboard.press('Escape')
  await details.nth(1).click()
  await page.getByRole('button', { name: 'Reject invoice' }).click()
  const rejectDialog = page.getByRole('alertdialog')
  await expect(rejectDialog.getByLabel('Rejection reason')).toBeFocused()
  await rejectDialog.getByRole('button', { name: 'Reject invoice' }).click()
  await expect(rejectDialog).toContainText('A rejection reason is required')
  await rejectDialog.getByLabel('Rejection reason').fill('Evidence mismatch')
  await rejectDialog.getByRole('button', { name: 'Reject invoice' }).click()
  await expect(
    page.getByLabel('Notifications alt+T').getByText('Invoice rejected')
  ).toBeVisible()
})

test('approve-and-issue never uploads after approval failure', async ({
  page,
}) => {
  const backend = await installAdminBackend(page, { approvalFailureOnce: true })
  await page.goto('/admin-invoices')
  await page.getByRole('button', { name: 'View details' }).nth(1).click()
  await fillIssuanceForm(page)
  await page.getByRole('button', { name: 'Approve and issue invoice' }).click()

  await expect(
    page.getByText('Invoice error: data changed, refresh and try again')
  ).toBeVisible()
  expect(backend.operationCalls).toEqual(['review:8:approve:reviewing'])
  await expect(
    page.getByRole('button', { name: 'Approve and issue invoice' })
  ).toBeVisible()
})

test('upload failure converges to approved and retries without another approval', async ({
  page,
}) => {
  const backend = await installAdminBackend(page, { uploadFailureOnce: true })
  await page.goto('/admin-invoices')
  await page.getByRole('button', { name: 'View details' }).nth(1).click()
  await fillIssuanceForm(page)
  await page.getByRole('button', { name: 'Approve and issue invoice' }).click()

  await expect(
    page.getByText('Invoice PDF storage is not configured or invalid')
  ).toBeVisible()
  expect(backend.operationCalls).toEqual([
    'review:8:approve:reviewing',
    'upload:8',
  ])
  await expect(page.getByRole('button', { name: 'Upload PDF' })).toBeVisible()

  await fillIssuanceForm(page)
  await page.getByRole('button', { name: 'Upload PDF' }).click()
  await expect(page.getByText('Invoice PDF saved')).toBeVisible()
  expect(backend.operationCalls).toEqual([
    'review:8:approve:reviewing',
    'upload:8',
    'upload:8',
  ])
})

test('invoice settings validates and saves percentage fees and R2 fields', async ({
  page,
}) => {
  const backend = await installAdminBackend(page)
  await page.goto('/invoice-settings')
  await expect(
    page.getByRole('heading', { name: 'Invoice settings' })
  ).toBeVisible()
  await page.getByLabel('Application window days').fill('0')
  await page.getByRole('button', { name: 'Save invoice settings' }).click()
  expect(
    await page
      .getByLabel('Application window days')
      .evaluate((input: HTMLInputElement) => input.validity.valid)
  ).toBe(false)
  expect(backend.settingWrites).toEqual([])

  await page.getByLabel('Application window days').fill('30')
  await page.getByLabel('Minimum amount in minor units').fill('9999')
  await page.getByRole('button', { name: 'Save invoice settings' }).click()
  await expect(page.getByText('Minimum invoice amount must be at least 100 CNY')).toBeVisible()
  expect(backend.settingWrites).toEqual([])

  await page.getByLabel('Minimum amount in minor units').fill('10000')
  await page.getByLabel('Invoice fee percentage').fill('5')
  await page.getByLabel('PDF retention days').fill('120')
  await page.getByRole('switch', { name: 'Enable company invoices' }).click()
  await page.getByRole('button', { name: 'Save invoice settings' }).click()
  await expect(page.getByText('Invoice settings saved')).toBeVisible()
  expect(backend.settingWrites).toEqual([
    {
      personal_enabled: true,
      company_enabled: true,
      application_window_days: 30,
      minimum_amount_minor: 10000,
      fee_percent: 5,
      pdf_retention_days: 120,
      r2_endpoint: '',
      r2_bucket: '',
      r2_access_key_id: '',
      r2_secret_access_key: '',
    },
  ])
})

test('invoice settings masks and preserves an already configured R2 secret', async ({
  page,
}) => {
  const backend = await installAdminBackend(page, {
    initialR2: {
      endpoint:
        'https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com',
      bucket: 'private-invoices',
      accessKeyId: 'configured-access-id',
      secretConfigured: true,
    },
  })
  await page.goto('/invoice-settings')

  await expect(page.getByLabel('R2 secret access key')).toHaveValue('')
  await expect(
    page.getByText('Leave blank to keep the current secret.')
  ).toBeVisible()
  await page.getByRole('button', { name: 'Save invoice settings' }).click()

  expect(backend.settingWrites).toHaveLength(1)
  expect(backend.settingWrites[0]).not.toHaveProperty('r2_secret_configured')
  expect(backend.settingWrites[0]).toMatchObject({
    r2_access_key_id: 'configured-access-id',
    r2_secret_access_key: '',
  })
})

test('invoice settings saves a complete new R2 configuration', async ({
  page,
}) => {
  const backend = await installAdminBackend(page)
  await page.goto('/invoice-settings')

  await page
    .getByLabel('R2 endpoint')
    .fill('https://fedcba9876543210fedcba9876543210.r2.cloudflarestorage.com')
  await page.getByLabel('R2 bucket').fill('new-private-invoices')
  await page.getByLabel('R2 access key ID').fill('new-access-id')
  await page.getByLabel('R2 secret access key').fill('test-fake-r2-secret')
  await page.getByRole('button', { name: 'Save invoice settings' }).click()

  await expect(page.getByText('Invoice settings saved')).toBeVisible()
  expect(backend.settingWrites).toHaveLength(1)
  expect(backend.settingWrites[0]).not.toHaveProperty('r2_secret_configured')
  expect(backend.settingWrites[0]).toMatchObject({
    r2_bucket: 'new-private-invoices',
    r2_access_key_id: 'new-access-id',
    r2_secret_access_key: 'test-fake-r2-secret',
  })
})

test('invoice settings rejects an incomplete R2 configuration', async ({
  page,
}) => {
  const backend = await installAdminBackend(page)
  await page.goto('/invoice-settings')

  await page
    .getByLabel('R2 endpoint')
    .fill('https://fedcba9876543210fedcba9876543210.r2.cloudflarestorage.com')
  await page.getByRole('button', { name: 'Save invoice settings' }).click()

  await expect(
    page.getByRole('alert').getByText('Enter a complete R2 configuration')
  ).toBeVisible()
  expect(backend.settingWrites).toEqual([])
})

test.describe('mobile admin invoices', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('keeps the Russian review queue and detail sheet within the viewport', async ({
    page,
  }) => {
    await installAdminBackend(page, { language: 'ru' })
    await page.goto('/admin-invoices')
    await expect(
      page.getByRole('heading', { name: 'Управление счетами' })
    ).toBeVisible()
    await page
      .getByRole('button', { name: 'Просмотреть детали' })
      .first()
      .click()
    const detailSheet = page.getByRole('dialog')
    await expect(detailSheet).toBeVisible()
    await expect
      .poll(async () => {
        const box = await detailSheet.boundingBox()
        return box ? Math.ceil(box.x + box.width) : Number.POSITIVE_INFINITY
      })
      .toBeLessThanOrEqual(390)
    expect(
      await detailSheet.evaluate(
        (element) => element.scrollWidth <= element.clientWidth
      )
    ).toBe(true)
  })
})
