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
import { lazy, Suspense } from 'react'

import { HtmlContent } from '@/components/html-content'
import type { RichContentProps } from '@/components/rich-content'

const Markdown = lazy(() =>
  import('@/components/ui/markdown').then((module) => ({
    default: module.Markdown,
  }))
)

/**
 * `RichContent` for always-loaded surfaces (the home page and the header
 * notification popover): the markdown renderer and its KaTeX, marked, and
 * DOMPurify dependencies load only when markdown is actually shown. While
 * they load, an empty box with the same classes holds the place.
 */
export function LazyRichContent(props: RichContentProps) {
  if (props.mode === 'html') {
    return (
      <HtmlContent
        content={props.content}
        className={props.className}
        variant={props.htmlVariant}
      />
    )
  }

  return (
    <Suspense fallback={<div className={props.className} aria-busy='true' />}>
      <Markdown breaks={props.breaks} className={props.className}>
        {props.content}
      </Markdown>
    </Suspense>
  )
}
