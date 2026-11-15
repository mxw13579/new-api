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
/**
 * Reload the page once after a lazily loaded chunk fails, which usually means
 * a newer deployment replaced the hashed chunk this page still references.
 * A sessionStorage marker keyed by the failure keeps a persistent failure from
 * reloading forever. Returns true when a reload was started.
 */
export function reloadOnceAfterChunkError(
  error: unknown,
  markerPrefix: string
): boolean {
  if (
    !(error instanceof Error) ||
    (error.name !== 'ChunkLoadError' &&
      !/^Loading (CSS )?chunk .+ failed\./.test(error.message))
  ) {
    return false
  }

  try {
    const key = `${markerPrefix}:${error.message}`
    if (sessionStorage.getItem(key)) return false
    sessionStorage.setItem(key, '1')
  } catch {
    // Without a persistent marker, reloading could loop indefinitely.
    return false
  }
  window.location.reload()
  return true
}
