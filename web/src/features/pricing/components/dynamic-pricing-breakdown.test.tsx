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
import { describe, expect, it } from 'bun:test'

import { StablePricingSequenceCoordinator } from '../lib/stable-pricing-keys'
import { DynamicPricingBreakdown } from './dynamic-pricing-breakdown'

describe('DynamicPricingBreakdown rerender wiring', () => {
  it('preserves shared view-model identities through insertion, reorder, and repeat evaluation', () => {
    const coordinator = new StablePricingSequenceCoordinator()
    const before = DynamicPricingBreakdown.plan(
      coordinator,
      '(tier("small", 1, 2) + tier("small", 1, 2) + tier("large", 3, 4)) * (header("x-plan") == "pro" ? 2 : 1)'
    )
    coordinator.commit(before.nextSnapshot)

    const afterInsert = DynamicPricingBreakdown.plan(
      coordinator,
      '(tier("small", 1, 2) + tier("small", 1, 2) + tier("small", 1, 2) + tier("large", 3, 4)) * (header("x-plan") == "pro" ? 2 : 1)'
    )
    coordinator.commit(afterInsert.nextSnapshot)
    const afterReorder = DynamicPricingBreakdown.plan(
      coordinator,
      '(tier("large", 3, 4) + tier("small", 1, 2) + tier("small", 1, 2) + tier("small", 1, 2)) * (header("x-plan") == "pro" ? 2 : 1)'
    )
    coordinator.commit(afterReorder.nextSnapshot)
    const repeated = DynamicPricingBreakdown.plan(
      coordinator,
      '(tier("large", 3, 4) + tier("small", 1, 2) + tier("small", 1, 2) + tier("small", 1, 2)) * (header("x-plan") == "pro" ? 2 : 1)'
    )

    expect(afterInsert.viewModel.tiers.slice(1).map(({ key }) => key)).toEqual(
      before.viewModel.tiers.map(({ key }) => key)
    )
    expect(afterReorder.viewModel.tiers[0].key).toBe(
      before.viewModel.tiers[2].key
    )
    expect(repeated.viewModel).toEqual(afterReorder.viewModel)
    expect(repeated.nextSnapshot).toEqual(afterReorder.nextSnapshot)
    expect(repeated.viewModel.ruleGroups.length).toBe(1)
  })
})
