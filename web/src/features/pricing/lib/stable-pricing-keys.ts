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

type PricingSequence = Array<{ fingerprint: string; key: string }>

export type ReconciledPricingViewModel = {
  tiers: Array<KeyedPricingItem<ParsedTier>>
  ruleGroups: Array<KeyedPricingItem<RequestRuleGroup>>
}

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

/** Reconciles newly parsed pricing sequences over one component lifecycle. */
export class StablePricingSequenceCoordinator {
  private readonly sequences = new Map<string, PricingSequence>()
  private readonly nextIds = new Map<string, number>()

  private createKey(namespace: string): string {
    const nextId = this.nextIds.get(namespace) ?? 0
    const key = `${namespace}:${nextId}`
    this.nextIds.set(namespace, nextId + 1)
    return key
  }

  private reconcileSequence<T>(
    items: readonly T[],
    namespace: string
  ): Array<KeyedPricingItem<T>> {
    const previousKeys = new Map<string, string[]>()
    for (const entry of this.sequences.get(namespace) ?? []) {
      const keys = previousKeys.get(entry.fingerprint) ?? []
      keys.push(entry.key)
      previousKeys.set(entry.fingerprint, keys)
    }

    const fingerprints = items.map(fingerprintValue)
    const currentCounts = new Map<string, number>()
    for (const fingerprint of fingerprints) {
      currentCounts.set(fingerprint, (currentCounts.get(fingerprint) ?? 0) + 1)
    }
    const additions = new Map<string, number>()
    for (const [fingerprint, count] of currentCounts) {
      additions.set(
        fingerprint,
        Math.max(count - (previousKeys.get(fingerprint)?.length ?? 0), 0)
      )
    }

    const sequence: PricingSequence = []
    const keyedItems = items.map((item, index) => {
      const fingerprint = fingerprints[index]
      const additionsRemaining = additions.get(fingerprint) ?? 0
      let key: string
      if (additionsRemaining > 0) {
        key = this.createKey(namespace)
        additions.set(fingerprint, additionsRemaining - 1)
      } else {
        key =
          previousKeys.get(fingerprint)?.shift() ?? this.createKey(namespace)
      }
      sequence.push({ fingerprint, key })
      return { item, key }
    })
    this.sequences.set(namespace, sequence)
    return keyedItems
  }

  reconcile(input: {
    tiers: readonly ParsedTier[]
    ruleGroups: readonly RequestRuleGroup[]
  }): ReconciledPricingViewModel {
    return {
      tiers: this.reconcileSequence(input.tiers, 'tier'),
      ruleGroups: this.reconcileSequence(input.ruleGroups, 'tier:rule-group'),
    }
  }
}
