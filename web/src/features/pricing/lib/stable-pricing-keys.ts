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
export type KeyedPricingItem<T> = {
  item: T
  key: string
}

/** Maintains React keys for object identities over one component lifecycle. */
export class StablePricingKeyRegistry {
  private readonly keys = new WeakMap<object, Map<string, string>>()
  private readonly nextIds = new Map<string, number>()

  keyFor(item: object, namespace: string): string {
    let itemKeys = this.keys.get(item)
    if (!itemKeys) {
      itemKeys = new Map<string, string>()
      this.keys.set(item, itemKeys)
    }

    const existingKey = itemKeys.get(namespace)
    if (existingKey) return existingKey

    const nextId = this.nextIds.get(namespace) ?? 0
    const key = `${namespace}:${nextId}`
    this.nextIds.set(namespace, nextId + 1)
    itemKeys.set(namespace, key)
    return key
  }

  withKeys<T extends object>(
    items: readonly T[],
    namespace: string
  ): Array<KeyedPricingItem<T>> {
    return items.map((item) => ({
      item,
      key: this.keyFor(item, namespace),
    }))
  }
}
