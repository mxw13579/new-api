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
import type { TFunction } from 'i18next'

/**
 * Plain section data: id and title only. Modules that need section ids or
 * navigation items (the sidebar, route `beforeLoad` guards) import this so the
 * section panels stay out of their bundle.
 */
export type SectionMetaDefinition<TSectionId extends string = string> = {
  id: TSectionId
  titleKey: string
}

export type SectionMetaConfig<TSectionId extends string> = {
  sections: readonly SectionMetaDefinition<TSectionId>[]
  defaultSection: NoInfer<TSectionId>
  basePath: string
  /** 'query' = `${basePath}?section=${id}`, 'path' = `${basePath}/${id}` */
  urlStyle?: 'query' | 'path'
}

/**
 * Create section ids, navigation items, and title lookup from plain section
 * data.
 */
export function createSectionMeta<TSectionId extends string>(
  config: SectionMetaConfig<TSectionId>
) {
  const { sections, defaultSection, basePath, urlStyle = 'query' } = config

  const sectionIds = sections.map((section) => section.id) as [
    TSectionId,
    ...TSectionId[],
  ]

  /**
   * Get navigation items for sidebar, with an optional badge per section
   */
  function getSectionNavItems(
    t: TFunction,
    badges?: Partial<Record<TSectionId, string>>
  ) {
    return sections.map((section) => ({
      title: t(section.titleKey),
      url:
        urlStyle === 'path'
          ? `${basePath}/${section.id}`
          : `${basePath}?section=${section.id}`,
      badge: badges?.[section.id],
    }))
  }

  /**
   * Get section data by id; unknown ids resolve to the first section.
   */
  function getSectionMeta(sectionId: TSectionId) {
    return sections.find((item) => item.id === sectionId) ?? sections[0]
  }

  return {
    sectionIds,
    defaultSection,
    getSectionNavItems,
    getSectionMeta,
  }
}

export type SectionMeta<TSectionId extends string> = ReturnType<
  typeof createSectionMeta<TSectionId>
>
