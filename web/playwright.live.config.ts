/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { defineConfig, devices } from '@playwright/test'

const port = Number(process.env.INVOICE_LIVE_PORT || 4187)
const baseURL = process.env.INVOICE_LIVE_BASE_URL || `http://127.0.0.1:${port}`
const externallyManaged = Boolean(process.env.INVOICE_LIVE_BASE_URL)

export default defineConfig({
  testDir: './e2e',
  testMatch: 'invoice-live-*.spec.ts',
  fullyParallel: false,
  workers: 1,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: [['list']],
  use: { baseURL, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  webServer: externallyManaged
    ? undefined
    : {
        command: 'powershell -NoProfile -File ./e2e/start-invoice-live.ps1',
        url: `${baseURL}/api/status`,
        reuseExistingServer: false,
        timeout: 180_000,
        env: {
          INVOICE_LIVE_PORT: String(port),
          PUBLIC_INVOICE_LIVE_CACHE_SNAPSHOT: '1',
        },
      },
  projects: [
    { name: 'desktop-chromium', use: { ...devices['Desktop Chrome'] } },
    {
      name: 'mobile-chromium',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 390, height: 844 },
      },
    },
  ],
})
