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
import { extractJsonErrorPosition } from '../utils/json-parser'

export type EditableListKeyEntry<T> = {
  item: T
  key: string
}

export function getOrderedItemState(
  currentIndex: number,
  itemIndex: number
): 'active' | 'completed' | 'pending' {
  if (currentIndex === itemIndex) {
    return 'active'
  }
  return currentIndex > itemIndex ? 'completed' : 'pending'
}

export function getAsyncContentState(
  loading: boolean,
  isError: boolean,
  isEmpty: boolean
): 'loading' | 'error' | 'empty' | 'content' {
  if (loading) {
    return 'loading'
  }
  if (isError) {
    return 'error'
  }
  return isEmpty ? 'empty' : 'content'
}

export function createStaticListKeys(prefix: string, count: number): string[] {
  return Array.from({ length: count }, (_, index) => `${prefix}-${index + 1}`)
}

export function reconcileEditableListKeys<T>(
  previous: readonly EditableListKeyEntry<T>[],
  items: readonly T[],
  createKey: () => string
): EditableListKeyEntry<T>[] {
  const available = new Set(previous)
  const next: Array<EditableListKeyEntry<T> | undefined> = Array.from({
    length: items.length,
  })

  for (const [index, item] of items.entries()) {
    const match = previous.find(
      (entry) => available.has(entry) && Object.is(entry.item, item)
    )
    if (match) {
      next[index] = { item, key: match.key }
      available.delete(match)
    }
  }

  for (const [index, item] of items.entries()) {
    if (next[index]) {
      continue
    }
    const positionalMatch = previous[index]
    if (positionalMatch && available.has(positionalMatch)) {
      next[index] = { item, key: positionalMatch.key }
      available.delete(positionalMatch)
      continue
    }
    next[index] = { item, key: createKey() }
  }

  return next as EditableListKeyEntry<T>[]
}

export function formatJsonForTextarea(value: string) {
  if (!value || !value.trim()) {
    return ''
  }

  try {
    const parsed = JSON.parse(value)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return value
  }
}

export function normalizeJsonString(value: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return ''
  }

  try {
    const parsed = JSON.parse(trimmed)
    return JSON.stringify(parsed)
  } catch {
    return trimmed
  }
}

type JsonValidationOptions = {
  allowEmpty?: boolean
  predicate?: (value: unknown) => boolean
  predicateMessage?: string
}

export type JsonValidationError = {
  type: 'required' | 'structure' | 'syntax'
  line?: number
  column?: number
  position?: number
  missingCommaLine?: number
}

function buildSyntaxError(
  error: unknown,
  jsonString: string
): JsonValidationError {
  if (!(error instanceof Error)) {
    return {
      type: 'syntax',
    } satisfies JsonValidationError
  }

  const position = extractJsonErrorPosition(error, jsonString)
  const message = error.message

  // Check if it's a "missing comma" type error
  const isMissingCommaError =
    message.includes("Expected ','") ||
    message.includes('Expected property name') ||
    message.includes('Unexpected string')

  const missingCommaLine =
    isMissingCommaError && position.line && position.line > 1
      ? position.line - 1
      : undefined

  return {
    type: 'syntax',
    ...position,
    missingCommaLine,
  } satisfies JsonValidationError
}

function formatErrorMessage(error: unknown, jsonString: string): string {
  if (!(error instanceof Error)) return 'Invalid JSON'

  const position = extractJsonErrorPosition(error, jsonString)
  const message = error.message
  const syntaxError = buildSyntaxError(error, jsonString)

  if (position.line && position.column) {
    let hint = ''
    if (syntaxError.missingCommaLine) {
      hint = ` (check line ${syntaxError.missingCommaLine} for missing comma)`
    }
    return `Error at line ${position.line}, column ${position.column}: ${message}${hint}`
  }

  if (position.position !== undefined) {
    return `Error at position ${position.position}: ${message}`
  }

  return message
}

export function validateJsonString(
  value: string,
  options: JsonValidationOptions = {}
) {
  const { allowEmpty = true, predicate, predicateMessage } = options
  const trimmed = value.trim()

  if (!trimmed) {
    return {
      valid: allowEmpty,
      message: allowEmpty ? undefined : 'Value is required',
      error: allowEmpty
        ? undefined
        : ({
            type: 'required',
          } satisfies JsonValidationError),
    }
  }

  try {
    const parsed = JSON.parse(trimmed)
    if (predicate && !predicate(parsed)) {
      return {
        valid: false,
        message: predicateMessage || 'JSON structure is invalid',
        error: {
          type: 'structure',
        } satisfies JsonValidationError,
      }
    }

    return { valid: true }
  } catch (error: unknown) {
    return {
      valid: false,
      message: formatErrorMessage(error, trimmed),
      error: buildSyntaxError(error, trimmed),
    }
  }
}
