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

import {
  normalizeCondition,
  parseTiersFromExpr,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
} from './billing-expr'
import { StablePricingSequenceCoordinator } from './stable-pricing-keys'

function parsePricingExpression(expression: string) {
  const split = splitBillingExprAndRequestRules(expression)
  return {
    tiers: parseTiersFromExpr(split.billingExpr),
    ruleGroups: tryParseRequestRuleExpr(split.requestRuleExpr) ?? [],
  }
}

describe('pricing behavior contracts', () => {
  it('normalizes every condition-source branch without changing valid input', () => {
    assert.equal(normalizeCondition(undefined).source, 'param')
    assert.equal(normalizeCondition({ source: 'header' }).source, 'header')
    assert.deepEqual(
      normalizeCondition({
        source: 'time',
        timeFunc: 'minute',
        timezone: 'UTC',
        mode: 'gte',
        value: '5',
      }),
      {
        source: 'time',
        timeFunc: 'minute',
        timezone: 'UTC',
        mode: 'gte',
        value: '5',
        rangeStart: '',
        rangeEnd: '',
      }
    )
  })

  it('reconciles keys across real tier and rule reparsing', () => {
    const coordinator = new StablePricingSequenceCoordinator()
    const beforePlan = coordinator.plan(
      parsePricingExpression(
        'tier("small", 1, 2) + tier("small", 1, 2) + tier("large", 3, 4)'
      )
    )
    coordinator.commit(beforePlan.nextSnapshot)
    const before = beforePlan.viewModel
    const afterInsertPlan = coordinator.plan(
      parsePricingExpression(
        'tier("small", 1, 2) + tier("small", 1, 2) + tier("small", 1, 2) + tier("large", 3, 4)'
      )
    )
    coordinator.commit(afterInsertPlan.nextSnapshot)
    const afterInsert = afterInsertPlan.viewModel
    const afterReorderPlan = coordinator.plan(
      parsePricingExpression(
        'tier("large", 3, 4) + tier("small", 1, 2) + tier("small", 1, 2) + tier("small", 1, 2)'
      )
    )
    coordinator.commit(afterReorderPlan.nextSnapshot)
    const afterReorder = afterReorderPlan.viewModel
    const repeated = coordinator.plan(
      parsePricingExpression(
        'tier("large", 3, 4) + tier("small", 1, 2) + tier("small", 1, 2) + tier("small", 1, 2)'
      )
    ).viewModel

    assert.notEqual(afterInsert.tiers[0].key, before.tiers[0].key)
    assert.deepEqual(
      afterInsert.tiers.slice(1).map(({ key }) => key),
      before.tiers.map(({ key }) => key)
    )
    assert.equal(afterReorder.tiers[0].key, before.tiers[2].key)
    assert.deepEqual(
      new Set(afterReorder.tiers.slice(1).map(({ key }) => key)),
      new Set(afterInsert.tiers.slice(0, 3).map(({ key }) => key))
    )
    assert.deepEqual(repeated, afterReorder)

    const rulesBeforePlan = coordinator.plan(
      parsePricingExpression(
        '(tier("base", 1, 2)) * (header("x-plan") == "pro" ? 2 : 1) * (header("x-plan") == "pro" ? 2 : 1) * (header("x-plan") == "team" ? 3 : 1)'
      )
    )
    coordinator.commit(rulesBeforePlan.nextSnapshot)
    const rulesBefore = rulesBeforePlan.viewModel
    const rulesAfterInsertPlan = coordinator.plan(
      parsePricingExpression(
        '(tier("base", 1, 2)) * (header("x-plan") == "pro" ? 2 : 1) * (header("x-plan") == "pro" ? 2 : 1) * (header("x-plan") == "pro" ? 2 : 1) * (header("x-plan") == "team" ? 3 : 1)'
      )
    )
    coordinator.commit(rulesAfterInsertPlan.nextSnapshot)
    const rulesAfterInsert = rulesAfterInsertPlan.viewModel
    const rulesAfterReorder = coordinator.plan(
      parsePricingExpression(
        '(tier("base", 1, 2)) * (header("x-plan") == "team" ? 3 : 1) * (header("x-plan") == "pro" ? 2 : 1) * (header("x-plan") == "pro" ? 2 : 1) * (header("x-plan") == "pro" ? 2 : 1)'
      )
    ).viewModel

    assert.notEqual(
      rulesAfterInsert.ruleGroups[0].key,
      rulesBefore.ruleGroups[0].key
    )
    assert.deepEqual(
      rulesAfterInsert.ruleGroups.slice(1).map(({ key }) => key),
      rulesBefore.ruleGroups.map(({ key }) => key)
    )
    assert.equal(
      rulesAfterReorder.ruleGroups[0].key,
      rulesBefore.ruleGroups[2].key
    )
    assert.deepEqual(
      new Set(rulesAfterReorder.ruleGroups.slice(1).map(({ key }) => key)),
      new Set(rulesAfterInsert.ruleGroups.slice(0, 3).map(({ key }) => key))
    )
    assert.ok(
      rulesAfterReorder.ruleGroups.every(({ key }) =>
        key.startsWith('calculation[tier:')
      )
    )
  })

  it('does not let an abandoned plan mutate the committed pricing keys', () => {
    const coordinator = new StablePricingSequenceCoordinator()
    const committedInput = parsePricingExpression(
      'tier("small", 1, 2) + tier("large", 3, 4)'
    )
    const committedPlan = coordinator.plan(committedInput)
    coordinator.commit(committedPlan.nextSnapshot)
    coordinator.commit(committedPlan.nextSnapshot)

    const abandonedPlan = coordinator.plan(
      parsePricingExpression(
        'tier("small", 1, 2) + tier("small", 1, 2) + tier("large", 3, 4)'
      )
    )
    const restoredPlan = coordinator.plan(committedInput)

    assert.deepEqual(
      restoredPlan.viewModel.tiers.map(({ key }) => key),
      committedPlan.viewModel.tiers.map(({ key }) => key)
    )
    assert.deepEqual(
      coordinator.plan(committedInput),
      restoredPlan,
      'repeat evaluation must stay independent of an uncommitted snapshot'
    )
    assert.notDeepEqual(
      abandonedPlan.viewModel.tiers.map(({ key }) => key),
      restoredPlan.viewModel.tiers.map(({ key }) => key)
    )
  })

  it('namespaces rule keys by the stable tier calculation identity', () => {
    const coordinator = new StablePricingSequenceCoordinator()
    const initialPlan = coordinator.plan(
      parsePricingExpression(
        '(tier("small", 1, 2) + tier("large", 3, 4)) * (header("x-plan") == "pro" ? 2 : 1)'
      )
    )
    coordinator.commit(initialPlan.nextSnapshot)

    const reorderedPlan = coordinator.plan(
      parsePricingExpression(
        '(tier("large", 3, 4) + tier("small", 1, 2)) * (header("x-plan") == "pro" ? 2 : 1)'
      )
    )
    coordinator.commit(reorderedPlan.nextSnapshot)
    const changedPlan = coordinator.plan(
      parsePricingExpression(
        '(tier("enterprise", 5, 6) + tier("small", 1, 2)) * (header("x-plan") == "pro" ? 2 : 1)'
      )
    )

    assert.deepEqual(
      reorderedPlan.viewModel.ruleGroups.map(({ key }) => key),
      initialPlan.viewModel.ruleGroups.map(({ key }) => key)
    )
    assert.notDeepEqual(
      changedPlan.viewModel.ruleGroups.map(({ key }) => key),
      reorderedPlan.viewModel.ruleGroups.map(({ key }) => key)
    )
  })
})
