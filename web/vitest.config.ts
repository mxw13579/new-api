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
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { defineConfig } from 'vitest/config'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  // jsdom supplies browser APIs, but optimized dependencies execute in Node.
  // Keep Node built-ins available to their CommonJS dependencies.
  environments: { client: { consumer: 'server' } },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    environment: 'jsdom',
    pool: 'threads',
    reporters: ['verbose'],
    // These suites load large UI graphs; avoid competing cold transforms.
    maxWorkers: 1,
    server: {
      deps: {
        inline: [/@lobehub\//, /antd-style/, /@codemirror\//, /@lezer\//],
      },
    },
    // Bundle the icon/UI dependency graph once instead of transforming its
    // thousands of modules in every worker. React stays external to the bundle.
    deps: {
      optimizer: {
        client: {
          enabled: true,
          include: ['@lobehub/icons'],
          // CodeMirror and Lezer use class/facet identities across packages.
          // Keep their entire dependency families outside the icon bundle.
          exclude: ['@codemirror', '@lezer'],
          rolldownOptions: {
            output: {
              // Some bundled CommonJS dependencies require external React.
              intro:
                'import { createRequire as __vitestCreateRequire } from "node:module"; const require = __vitestCreateRequire(import.meta.url);',
            },
          },
        },
      },
    },
    setupFiles: ['./src/test-setup.ts'],
    // Several heavy jsdom suites (channel-configuration, visual-billing-editor)
    // legitimately take >5s per test on contended CI runners; the vitest
    // default of 5000ms fails whichever of them crosses the line first. The
    // heaviest test measures ~3.2s uncontended, so 20s keeps headroom for the
    // ~4x slowdown observed on shared runners.
    testTimeout: 20000,
    clearMocks: true,
    restoreMocks: true,
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
})
