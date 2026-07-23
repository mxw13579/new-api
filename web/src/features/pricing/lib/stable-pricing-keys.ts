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

/** Adds a content-local occurrence identity without coupling keys to list position. */
export function withStablePricingKeys<T>(
  items: readonly T[],
  namespace: string
): Array<KeyedPricingItem<T>> {
  const occurrences = new Map<string, number>()
  return items.map((item) => {
    const content = JSON.stringify(item) ?? String(item)
    const occurrence = occurrences.get(content) ?? 0
    occurrences.set(content, occurrence + 1)
    return {
      item,
      key: `${namespace}:${content}:${occurrence}`,
    }
  })
}
