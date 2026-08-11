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

import { Window } from 'happy-dom'
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

import type { RequestRuleTrace } from '../../lib/billing-expr'

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

  it('retains duplicate trace-rule DOM nodes after equivalent traces are reallocated', async () => {
    const domWindow = new Window()
    const domGlobals = [
      'window',
      'document',
      'navigator',
      'HTMLElement',
      'SVGElement',
      'Node',
      'Element',
      'Event',
      'CustomEvent',
      'MutationObserver',
      'requestAnimationFrame',
      'cancelAnimationFrame',
      'getComputedStyle',
    ] as const
    const originalDomGlobals = domGlobals.map((key) => ({
      key,
      descriptor: Object.getOwnPropertyDescriptor(globalThis, key),
    }))

    for (const key of domGlobals) {
      Object.defineProperty(globalThis, key, {
        configurable: true,
        value: domWindow[key],
      })
    }

    const reactTestGlobals = globalThis as typeof globalThis & {
      IS_REACT_ACT_ENVIRONMENT?: boolean
    }
    const originalActEnvironment = Object.getOwnPropertyDescriptor(
      reactTestGlobals,
      'IS_REACT_ACT_ENVIRONMENT'
    )
    Object.defineProperty(reactTestGlobals, 'IS_REACT_ACT_ENVIRONMENT', {
      configurable: true,
      value: true,
    })

    const consoleErrors: string[] = []
    const runtimeConsole = globalThis.console
    const originalConsoleError = Object.getOwnPropertyDescriptor(
      runtimeConsole,
      'error'
    )
    Object.defineProperty(runtimeConsole, 'error', {
      configurable: true,
      value: (...args: unknown[]) => {
        consoleErrors.push(args.map(String).join(' '))
      },
    })

    const { act } = await import('react')
    const { createRoot } = await import('react-dom/client')
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const duplicateRule: RequestRuleTrace = {
      cond: 'header("x-plan") == "pro"',
      multiplier: 2,
      matched: false,
    }

    try {
      await act(async () => {
        root.render(
          <DynamicPricingBreakdown
            billingExpr='tier("base", 1, 2)'
            requestRules={[{ ...duplicateRule }, { ...duplicateRule }]}
            compact
          />
        )
      })
      const firstRows = [...container.querySelectorAll('ul > li')]
      assert.equal(firstRows.length, 2)

      await act(async () => {
        root.render(
          <DynamicPricingBreakdown
            billingExpr='tier("base", 1, 2)'
            requestRules={[{ ...duplicateRule }, { ...duplicateRule }]}
            compact
          />
        )
      })
      const repeatedRows = [...container.querySelectorAll('ul > li')]

      assert.equal(repeatedRows.length, 2)
      assert.strictEqual(repeatedRows[0], firstRows[0])
      assert.strictEqual(repeatedRows[1], firstRows[1])
      assert.deepEqual(
        consoleErrors.filter((message) =>
          /same key|unique.*key|duplicate key/i.test(message)
        ),
        []
      )
    } finally {
      await act(async () => root.unmount())
      container.remove()
      if (originalConsoleError) {
        Object.defineProperty(runtimeConsole, 'error', originalConsoleError)
      } else {
        Reflect.deleteProperty(runtimeConsole, 'error')
      }
      domWindow.close()

      for (const { key, descriptor } of originalDomGlobals) {
        if (descriptor) {
          Object.defineProperty(globalThis, key, descriptor)
        } else {
          Reflect.deleteProperty(globalThis, key)
        }
      }
      if (originalActEnvironment) {
        Object.defineProperty(
          reactTestGlobals,
          'IS_REACT_ACT_ENVIRONMENT',
          originalActEnvironment
        )
      } else {
        Reflect.deleteProperty(reactTestGlobals, 'IS_REACT_ACT_ENVIRONMENT')
      }
    }
  })
})
