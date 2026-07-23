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
import type { ApiResponse, Redemption } from '../types'

type RedemptionEditLoad = {
  request: Promise<ApiResponse<Redemption>>
  isActive: () => boolean
  onLoaded: (redemption: Redemption) => void
  onError: (message: string) => void
  fallbackMessage: string
}

/** Applies an edit response only while its drawer selection is still active. */
export async function loadRedemptionForEdit(
  load: RedemptionEditLoad
): Promise<void> {
  try {
    const result = await load.request
    if (!load.isActive()) {
      return
    }
    if (result.success && result.data) {
      load.onLoaded(result.data)
      return
    }
    load.onError(result.message || load.fallbackMessage)
  } catch {
    if (load.isActive()) {
      load.onError(load.fallbackMessage)
    }
  }
}
