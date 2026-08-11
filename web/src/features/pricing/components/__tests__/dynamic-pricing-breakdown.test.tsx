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
import { describe, it } from 'bun:test'
import assert from 'node:assert/strict'

import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

import {
  parseTiersFromExpr,
  requestRuleGroupsFromTrace,
  type RequestRuleTrace,
} from '../../lib/billing-expr'
import { StablePricingSequenceCoordinator } from '../../lib/stable-pricing-keys'

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

const { DynamicPricingBreakdown } = await import('../dynamic-pricing-breakdown')

const BILLING_EXPRESSION =
  '(tier("base", 1, 2)) * (header("x-plan") == "expression-only" ? 9 : 1)'

describe('trace-driven dynamic pricing breakdown', () => {
  it('renders matched and unmatched trace rules instead of expression rules', () => {
    const html = renderToStaticMarkup(
      <DynamicPricingBreakdown
        billingExpr={BILLING_EXPRESSION}
        requestRules={[
          {
            cond: 'header("x-plan") == "pro"',
            multiplier: 2,
            matched: true,
          },
          {
            cond: 'header("x-plan") == "team"',
            multiplier: 3,
            matched: false,
          },
        ]}
        compact
      />
    )

    assert.match(html, /Header x-plan = pro/)
    assert.match(html, /2x[^<]*Matched/)
    assert.match(html, /Header x-plan = team/)
    assert.match(html, />3x</)
    assert.equal((html.match(/Matched/g) ?? []).length, 1)
    assert.doesNotMatch(html, /expression-only/)
  })

  it('renders raw text when a trace condition is unparsable', () => {
    const html = renderToStaticMarkup(
      <DynamicPricingBreakdown
        billingExpr='tier("base", 1, 2)'
        requestRules={[
          {
            cond: 'unsupported(condition) ~= "raw-value"',
            multiplier: 4,
            matched: false,
          },
        ]}
        compact
      />
    )

    assert.match(html, /unsupported\(condition\) ~= &quot;raw-value&quot;/)
    assert.match(html, />4x</)
  })

  it('keeps duplicate trace-rule keys unique and stable after reparsing', () => {
    const coordinator = new StablePricingSequenceCoordinator()
    const duplicateRule: RequestRuleTrace = {
      cond: 'header("x-plan") == "pro"',
      multiplier: 2,
      matched: false,
    }
    const input = {
      tiers: parseTiersFromExpr('tier("base", 1, 2)'),
      ruleGroups: requestRuleGroupsFromTrace([
        { ...duplicateRule },
        { ...duplicateRule },
      ]),
    }

    const firstPlan = coordinator.plan(input)
    coordinator.commit(firstPlan.nextSnapshot)
    const repeatedPlan = coordinator.plan({
      tiers: parseTiersFromExpr('tier("base", 1, 2)'),
      ruleGroups: requestRuleGroupsFromTrace([
        { ...duplicateRule },
        { ...duplicateRule },
      ]),
    })
    const firstKeys = firstPlan.viewModel.ruleGroups.map(({ key }) => key)
    const repeatedKeys = repeatedPlan.viewModel.ruleGroups.map(({ key }) => key)

    assert.equal(new Set(firstKeys).size, 2)
    assert.deepEqual(repeatedKeys, firstKeys)
  })
})
