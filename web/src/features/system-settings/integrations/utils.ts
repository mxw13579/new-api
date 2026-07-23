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

export function normalizeAmountOptions(items: unknown[]): number[] {
  return items
    .filter((item) => !Number.isNaN(Number(item)))
    .map(Number)
    .sort((a, b) => a - b)
}

export function removeTrailingSlash(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return ''
  return trimmed.replace(/\/+$/, '')
}

export function formatJsonForEditor(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return ''
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2)
  } catch {
    return trimmed
  }
}

export function normalizeJsonForComparison(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return ''
  try {
    return JSON.stringify(JSON.parse(trimmed))
  } catch {
    return trimmed
  }
}

function formatJsonError(error: unknown, jsonString: string): string {
  if (!(error instanceof Error)) return 'Invalid JSON'

  const position = extractJsonErrorPosition(error, jsonString)
  const message = error.message

  const isMissingCommaError =
    message.includes("Expected ','") ||
    message.includes('Expected property name') ||
    message.includes('Unexpected string')

  if (position.line && position.column) {
    let hint = ''
    if (isMissingCommaError && position.line > 1) {
      hint = ` (check line ${position.line - 1} for missing comma)`
    }
    return `Error at line ${position.line}, column ${position.column}: ${message}${hint}`
  }

  return message
}

export function isValidJson(
  value: string,
  predicate?: (parsed: unknown) => boolean
): boolean {
  const trimmed = value.trim()
  if (!trimmed) return true
  try {
    const parsed = JSON.parse(trimmed)
    if (predicate && !predicate(parsed)) {
      return false
    }
    return true
  } catch {
    return false
  }
}

export function getJsonError(
  value: string,
  predicate?: (parsed: unknown) => boolean
): string | null {
  const trimmed = value.trim()
  if (!trimmed) return null
  try {
    const parsed = JSON.parse(trimmed)
    if (predicate && !predicate(parsed)) {
      return 'JSON structure is invalid'
    }
    return null
  } catch (error) {
    return formatJsonError(error, trimmed)
  }
}
