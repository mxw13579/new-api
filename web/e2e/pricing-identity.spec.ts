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
import { spawnSync } from 'node:child_process'
import { resolve } from 'node:path'

import { expect, test } from '@playwright/test'

const beforeExpression =
  '(tier("small", 1, 2) + tier("small", 1, 2) + tier("large", 3, 4)) * (header("x-plan") == "pro" ? 2 : 1)'
const insertedExpression =
  '(tier("small", 1, 2) + tier("small", 1, 2) + tier("small", 1, 2) + tier("large", 3, 4)) * (header("x-plan") == "pro" ? 2 : 1)'
const reorderedExpression =
  '(tier("large", 3, 4) + tier("small", 1, 2) + tier("small", 1, 2) + tier("small", 1, 2)) * (header("x-plan") == "pro" ? 2 : 1)'

let browserBundle = ''

test.beforeAll(async () => {
  const componentPath = resolve(
    'src/features/pricing/components/dynamic-pricing-breakdown.tsx'
  ).replaceAll('\\', '/')
  const shells = {
    'react-i18next': `export const useTranslation = () => ({ t: (key) => key })`,
    'lucide-react': `export const Tag = () => null`,
    '@/components/data-table': `export const StaticDataTable = () => null`,
    '@/components/ui/badge': `import { createElement } from 'react'; export const Badge = ({ children }) => createElement('span', null, children)`,
    '@/lib/utils': `export const cn = (...values) => values.flat().filter(Boolean).join(' ')`,
    '@/stores/system-config-store': `export const useSystemConfigStore = (selector) => selector({ config: { currency: { quotaDisplayType: 'USD' } } })`,
  }
  const buildScript = `
    const componentPath = ${JSON.stringify(componentPath)}
    const shells = ${JSON.stringify(shells)}
    const entry = [
      "import { createElement } from 'react'",
      "import { createRoot } from 'react-dom/client'",
      "import { flushSync } from 'react-dom'",
      'import { DynamicPricingBreakdown } from ' + JSON.stringify(componentPath),
      "export const hasTestOnlyPlan = 'plan' in DynamicPricingBreakdown",
      "export function mount(container, billingExpr) { const root = createRoot(container); const rerender = (expression) => flushSync(() => root.render(createElement(DynamicPricingBreakdown, { billingExpr: expression, compact: true }))); rerender(billingExpr); return { rerender } }",
    ].join('\\n')
    const build = await Bun.build({ entrypoints: ['pricing-browser-entry'], target: 'browser', format: 'esm', plugins: [{
      name: 'pricing-shell', setup(builder) {
        builder.onResolve({ filter: /^pricing-browser-entry$/ }, () => ({ path: 'entry', namespace: 'pricing-shell' }))
        builder.onResolve({ filter: /^(react-i18next|lucide-react|@\\/components\\/data-table|@\\/components\\/ui\\/badge|@\\/lib\\/utils|@\\/stores\\/system-config-store)$/ }, (args) => ({ path: args.path, namespace: 'pricing-shell' }))
        builder.onLoad({ filter: /.*/, namespace: 'pricing-shell' }, (args) => ({ loader: 'js', contents: args.path === 'entry' ? entry : shells[args.path] }))
      }
    }] })
    if (!build.success) throw new Error(build.logs.map(String).join('\\n'))
    process.stdout.write(await build.outputs[0].text())
  `
  const result = spawnSync('bun', ['-e', buildScript], {
    cwd: process.cwd(),
    encoding: 'utf8',
    maxBuffer: 20 * 1024 * 1024,
  })
  if (result.status !== 0) throw new Error(result.stderr)
  browserBundle = result.stdout
})

test('pricing identities survive real component rerenders', async ({
  page,
}) => {
  await page.setContent('<div id="root"></div>')
  await page.evaluate(
    async ({ bundle, expression }) => {
      const url = URL.createObjectURL(
        new Blob([bundle], { type: 'text/javascript' })
      )
      const pricing = await import(url)
      const root = document.querySelector('#root')
      if (!(root instanceof HTMLElement)) throw new Error('Missing root')
      window.pricingRenderer = pricing.mount(root, expression)
      window.pricingSnapshots = new Map()
      window.hasTestOnlyPricingPlan = pricing.hasTestOnlyPlan
    },
    { bundle: browserBundle, expression: beforeExpression }
  )

  const capture = (name: string) =>
    page.evaluate((snapshotName) => {
      const tiers = [
        ...document.querySelectorAll('div[class~="sm:hidden"] > div'),
      ]
      const rules = [...document.querySelectorAll('section ul > li')]
      window.pricingSnapshots.set(snapshotName, { tiers, rules })
      return {
        tiers: tiers.map((node) => node.textContent),
        rules: rules.map((node) => node.textContent),
      }
    }, name)
  const before = await capture('before')
  await page.evaluate(
    (expression) => window.pricingRenderer.rerender(expression),
    insertedExpression
  )
  const inserted = await capture('inserted')
  await page.evaluate(
    (expression) => window.pricingRenderer.rerender(expression),
    reorderedExpression
  )
  const reordered = await capture('reordered')
  await page.evaluate(
    (expression) => window.pricingRenderer.rerender(expression),
    reorderedExpression
  )
  const repeated = await capture('repeated')

  expect(await page.evaluate(() => window.hasTestOnlyPricingPlan)).toBe(false)
  expect(before.tiers.map((text) => text?.startsWith('small'))).toEqual([
    true,
    true,
    false,
  ])
  expect(inserted.tiers).toHaveLength(4)
  expect(reordered.tiers[0]?.startsWith('large')).toBe(true)
  expect(repeated).toEqual(reordered)
  expect(repeated.rules).toEqual(['Header x-plan = pro2x'])
  expect(
    await page.evaluate(() => {
      const snapshots = window.pricingSnapshots
      const beforeNodes = snapshots.get('before')?.tiers ?? []
      const insertedNodes = snapshots.get('inserted')?.tiers ?? []
      const reorderedNodes = snapshots.get('reordered')?.tiers ?? []
      const repeatedNodes = snapshots.get('repeated')?.tiers ?? []
      return (
        beforeNodes.every((node, index) => node === insertedNodes[index + 1]) &&
        beforeNodes[2] === reorderedNodes[0] &&
        reorderedNodes.every((node, index) => node === repeatedNodes[index]) &&
        snapshots.get('reordered')?.rules[0] ===
          snapshots.get('repeated')?.rules[0]
      )
    })
  ).toBe(true)
})

declare global {
  interface Window {
    hasTestOnlyPricingPlan: boolean
    pricingRenderer: { rerender: (billingExpr: string) => void }
    pricingSnapshots: Map<string, { tiers: Element[]; rules: Element[] }>
  }
}
