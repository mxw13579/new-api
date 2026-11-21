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
 * Keys are English source strings, and i18next returns the key itself
 * (interpolated) when no resource has it. An English entry whose value equals
 * its key therefore renders the same text whether or not it is shipped, so the
 * runtime `en` bundle keeps only the entries that change the output.
 *
 * Entries whose key contains `_` are kept even when the value equals the key:
 * i18next resolves plural (`_one`, `_other`, ...) and context (`_male`, ...)
 * suffixes before the bare key, so dropping such an entry could switch the
 * lookup to a different key.
 *
 * Applied to `locales/en.json` at bundle time (see `rsbuild.config.ts`); the
 * source file stays complete for the i18n tooling.
 */
export function slimEnglishLocale(
  locale: Record<string, Record<string, string>>
): Record<string, Record<string, string>> {
  const slim: Record<string, Record<string, string>> = {}
  for (const [namespace, entries] of Object.entries(locale)) {
    slim[namespace] = Object.fromEntries(
      Object.entries(entries).filter(
        ([key, value]) => value !== key || key.includes('_')
      )
    )
  }
  return slim
}
