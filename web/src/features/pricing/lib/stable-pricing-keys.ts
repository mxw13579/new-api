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
import type { ParsedTier, RequestRuleGroup } from './billing-expr'

export type KeyedPricingItem<T> = {
  item: T
  key: string
}

type PricingSequenceEntry = Readonly<{ fingerprint: string; key: string }>
type PricingSequence = readonly PricingSequenceEntry[]

export type ReconciledPricingViewModel = {
  tiers: Array<KeyedPricingItem<ParsedTier>>
  ruleGroups: Array<KeyedPricingItem<RequestRuleGroup>>
}

export type StablePricingSnapshot = Readonly<{
  baseRevision: number
  revision: number
  sequences: Readonly<Record<string, PricingSequence>>
  nextIds: Readonly<Record<string, number>>
  contentKey: string
}>

export type StablePricingPlan = Readonly<{
  viewModel: ReconciledPricingViewModel
  nextSnapshot: StablePricingSnapshot
}>

function fingerprintValue(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(fingerprintValue).join(',')}]`
  }
  if (value !== null && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map(
        (key) =>
          `${JSON.stringify(key)}:${fingerprintValue(
            (value as Record<string, unknown>)[key]
          )}`
      )
      .join(',')}}`
  }
  return JSON.stringify(value) ?? String(value)
}

const EMPTY_SNAPSHOT: StablePricingSnapshot = Object.freeze({
  baseRevision: -1,
  revision: 0,
  sequences: Object.freeze({}),
  nextIds: Object.freeze({}),
  contentKey: fingerprintValue({ sequences: {}, nextIds: {} }),
})

/** Plans and commits stable pricing identities over one component lifecycle. */
export class StablePricingSequenceCoordinator {
  private committedSnapshot = EMPTY_SNAPSHOT

  plan(input: {
    tiers: readonly ParsedTier[]
    ruleGroups: readonly RequestRuleGroup[]
  }): StablePricingPlan {
    let sequences: Record<string, PricingSequence> = {
      ...this.committedSnapshot.sequences,
    }
    const nextIds: Record<string, number> = {
      ...this.committedSnapshot.nextIds,
    }

    const reconcileSequence = <T>(
      items: readonly T[],
      namespace: string
    ): Array<KeyedPricingItem<T>> => {
      const previousKeys = new Map<string, string[]>()
      for (const entry of sequences[namespace] ?? []) {
        const keys = previousKeys.get(entry.fingerprint) ?? []
        keys.push(entry.key)
        previousKeys.set(entry.fingerprint, keys)
      }

      const fingerprints = items.map(fingerprintValue)
      const currentCounts = new Map<string, number>()
      for (const fingerprint of fingerprints) {
        currentCounts.set(
          fingerprint,
          (currentCounts.get(fingerprint) ?? 0) + 1
        )
      }
      const additions = new Map<string, number>()
      for (const [fingerprint, count] of currentCounts) {
        additions.set(
          fingerprint,
          Math.max(count - (previousKeys.get(fingerprint)?.length ?? 0), 0)
        )
      }

      const sequence: PricingSequenceEntry[] = []
      const keyedItems = items.map((item, index) => {
        const fingerprint = fingerprints[index]
        const additionsRemaining = additions.get(fingerprint) ?? 0
        let key: string
        if (additionsRemaining > 0) {
          const nextId = nextIds[namespace] ?? 0
          key = `${namespace}:${nextId}`
          nextIds[namespace] = nextId + 1
          additions.set(fingerprint, additionsRemaining - 1)
        } else {
          const previousKey = previousKeys.get(fingerprint)?.shift()
          if (previousKey !== undefined) {
            key = previousKey
          } else {
            const nextId = nextIds[namespace] ?? 0
            key = `${namespace}:${nextId}`
            nextIds[namespace] = nextId + 1
          }
        }
        sequence.push(Object.freeze({ fingerprint, key }))
        return { item, key }
      })
      sequences = {
        ...sequences,
        [namespace]: Object.freeze(sequence),
      }
      return keyedItems
    }

    const tiers = reconcileSequence(input.tiers, 'tier')
    const calculationIdentity = tiers
      .map(({ key }) => key)
      .sort()
      .join(',')
    const ruleGroups = reconcileSequence(
      input.ruleGroups,
      `calculation[${calculationIdentity}]:rule-group`
    )
    const frozenSequences = Object.freeze(sequences)
    const frozenNextIds = Object.freeze(nextIds)
    const contentKey = fingerprintValue({
      sequences: frozenSequences,
      nextIds: frozenNextIds,
    })
    const nextSnapshot =
      contentKey === this.committedSnapshot.contentKey
        ? this.committedSnapshot
        : Object.freeze({
            baseRevision: this.committedSnapshot.revision,
            revision: this.committedSnapshot.revision + 1,
            sequences: frozenSequences,
            nextIds: frozenNextIds,
            contentKey,
          })

    return Object.freeze({
      viewModel: { tiers, ruleGroups },
      nextSnapshot,
    })
  }

  commit(nextSnapshot: StablePricingSnapshot): void {
    if (
      this.committedSnapshot.revision === nextSnapshot.revision &&
      this.committedSnapshot.contentKey === nextSnapshot.contentKey
    ) {
      return
    }
    if (nextSnapshot.baseRevision !== this.committedSnapshot.revision) {
      throw new Error('Cannot commit a stale pricing identity snapshot')
    }
    this.committedSnapshot = nextSnapshot
  }
}
