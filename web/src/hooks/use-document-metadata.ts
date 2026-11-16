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
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { useStatus } from '@/hooks/use-status'
import { toIntlLocale } from '@/i18n/languages'

/**
 * Keep `document.title` and `<html lang>` in step with the current route and
 * interface language. Public pages use the same titles the server renders
 * (router/web-router.go); every other page shows the system name.
 *
 * The server already rendered the right title for the page the document
 * loaded on, so that page keeps it until the route or interface language
 * changes; a crawler whose language detects as `en` then still sees titles
 * that match the server's Chinese description. Until a real `system_name` is
 * known (cached status or `/api/status`), the title is never touched, so a
 * first visit cannot flash the built-in default name.
 */
export function useDocumentMetadata(pathname: string): void {
  const { t, i18n } = useTranslation()
  const systemName = useStatus().status?.system_name
  const language = i18n.resolvedLanguage || i18n.language
  // Cleared on the first route or language change after load.
  const initialPage = useRef<{ pathname: string; language: string } | null>({
    pathname,
    language,
  })

  useEffect(() => {
    if (
      initialPage.current?.pathname === pathname &&
      initialPage.current.language === language
    ) {
      return
    }
    initialPage.current = null
    if (!systemName) return
    const path = pathname.replace(/\/+$/, '') || '/'
    // document.title is plain text, so the name must not be HTML-escaped.
    const options = { name: systemName, interpolation: { escapeValue: false } }
    let title = systemName
    if (path === '/') {
      title = t(
        '{{name}} · Unified API for Claude, Codex, Gemini and OpenAI-compatible models',
        options
      )
    } else if (path === '/pricing') {
      title = t('Model marketplace and pricing · {{name}}', options)
    } else if (path === '/about') {
      title = t('About · {{name}}', options)
    } else if (path === '/privacy-policy') {
      title = t('Privacy policy · {{name}}', options)
    } else if (path === '/user-agreement') {
      title = t('User agreement · {{name}}', options)
    }
    document.title = title
  }, [pathname, systemName, t, language])

  useEffect(() => {
    const locale = toIntlLocale(language)
    if (locale) document.documentElement.lang = locale
  }, [language])
}
