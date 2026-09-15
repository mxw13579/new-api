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

// Parse actual supported expressions independently: adding tiers together is
// not a displayable tier chain in the upstream AST-based pricing parser.
function parsePricingExpressions(expressions: string[]) {
  const splits = expressions.map(splitBillingExprAndRequestRules)
  return {
    tiers: splits.flatMap((split) => parseTiersFromExpr(split.billingExpr)),
    ruleGroups: splits.flatMap(
      (split) => tryParseRequestRuleExpr(split.requestRuleExpr) ?? []
    ),
  }
}

const small = 'tier("small", p * 1 + c * 2)'
const large = 'tier("large", p * 3 + c * 4)'
const proRule = '(header("x-plan") == "pro" ? 2 : 1)'
const teamRule = '(header("x-plan") == "team" ? 3 : 1)'

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
      parsePricingExpressions([small, small, large])
    )
    coordinator.commit(beforePlan.nextSnapshot)
    const before = beforePlan.viewModel
    const insertedPlan = coordinator.plan(
      parsePricingExpressions([small, small, small, large])
    )
    coordinator.commit(insertedPlan.nextSnapshot)
    const inserted = insertedPlan.viewModel
    const reorderedPlan = coordinator.plan(
      parsePricingExpressions([large, small, small, small])
    )
    coordinator.commit(reorderedPlan.nextSnapshot)
    const reordered = reorderedPlan.viewModel

    assert.equal(before.tiers.length, 3)
    assert.notEqual(inserted.tiers[0].key, before.tiers[0].key)
    assert.deepEqual(
      inserted.tiers.slice(1).map(({ key }) => key),
      before.tiers.map(({ key }) => key)
    )
    assert.equal(reordered.tiers[0].key, before.tiers[2].key)
    assert.deepEqual(
      new Set(reordered.tiers.slice(1).map(({ key }) => key)),
      new Set(inserted.tiers.slice(0, 3).map(({ key }) => key))
    )
    assert.deepEqual(
      coordinator.plan(parsePricingExpressions([large, small, small, small]))
        .viewModel,
      reordered
    )

    const rulesBeforePlan = coordinator.plan(
      parsePricingExpressions([
        `(${small}) * ${proRule} * ${proRule} * ${teamRule}`,
      ])
    )
    coordinator.commit(rulesBeforePlan.nextSnapshot)
    const rulesBefore = rulesBeforePlan.viewModel
    const rulesInsertedPlan = coordinator.plan(
      parsePricingExpressions([
        `(${small}) * ${proRule} * ${proRule} * ${proRule} * ${teamRule}`,
      ])
    )
    coordinator.commit(rulesInsertedPlan.nextSnapshot)
    const rulesInserted = rulesInsertedPlan.viewModel
    const rulesReordered = coordinator.plan(
      parsePricingExpressions([
        `(${small}) * ${teamRule} * ${proRule} * ${proRule} * ${proRule}`,
      ])
    ).viewModel

    assert.equal(rulesBefore.ruleGroups.length, 3)
    assert.notEqual(
      rulesInserted.ruleGroups[0].key,
      rulesBefore.ruleGroups[0].key
    )
    assert.deepEqual(
      rulesInserted.ruleGroups.slice(1).map(({ key }) => key),
      rulesBefore.ruleGroups.map(({ key }) => key)
    )
    assert.equal(
      rulesReordered.ruleGroups[0].key,
      rulesBefore.ruleGroups[2].key
    )
    assert.deepEqual(
      new Set(rulesReordered.ruleGroups.slice(1).map(({ key }) => key)),
      new Set(rulesInserted.ruleGroups.slice(0, 3).map(({ key }) => key))
    )
    assert.ok(
      rulesReordered.ruleGroups.every(({ key }) =>
        key.startsWith('calculation[tier:')
      )
    )
  })

  it('does not let an abandoned plan mutate the committed pricing keys', () => {
    const coordinator = new StablePricingSequenceCoordinator()
    const committedInput = parsePricingExpressions([small, large])
    const committedPlan = coordinator.plan(committedInput)
    coordinator.commit(committedPlan.nextSnapshot)
    coordinator.commit(committedPlan.nextSnapshot)
    const abandonedPlan = coordinator.plan(
      parsePricingExpressions([small, small, large])
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
      parsePricingExpressions([`(${small}) * ${proRule}`, large])
    )
    coordinator.commit(initialPlan.nextSnapshot)
    const reorderedPlan = coordinator.plan(
      parsePricingExpressions([`(${large}) * ${proRule}`, small])
    )
    coordinator.commit(reorderedPlan.nextSnapshot)
    const changedPlan = coordinator.plan(
      parsePricingExpressions([
        `(tier("enterprise", p * 5 + c * 6)) * ${proRule}`,
        small,
      ])
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
