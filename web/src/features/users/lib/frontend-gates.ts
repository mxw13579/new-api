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
import type { QuotaAdjustMode } from '../types'

export function parseQuotaInput(value: string): number {
  if (value === '') return 0
  return Number.parseFloat(value)
}

export function getQuotaModeLabel(mode: QuotaAdjustMode): string {
  if (mode === 'add') return 'Add'
  if (mode === 'subtract') return 'Subtract'
  return 'Override'
}

export function resolveDisabledUserRowClass(
  disabled: boolean,
  isMobile: boolean
): 'mobile' | 'desktop' | undefined {
  if (!disabled) return undefined
  return isMobile ? 'mobile' : 'desktop'
}

type UserResponse<T> = { success: boolean; data?: T }

export async function loadUserForDrawer<T extends { id: number }>(
  userId: number,
  request: (id: number) => Promise<UserResponse<T>>,
  apply: (user: T) => void,
  reportError: (error: unknown) => void,
  isCurrent: () => boolean
): Promise<void> {
  try {
    const result = await request(userId)
    if (isCurrent() && result.success && result.data) apply(result.data)
  } catch (error) {
    if (isCurrent()) reportError(error)
  }
}
