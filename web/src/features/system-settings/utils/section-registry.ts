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
import type { ReactNode } from 'react'

import { createSectionMeta, type SectionMeta } from './section-meta'

/**
 * Section definition for settings pages
 */
export type SectionDefinition<TSettings, TExtraArgs extends unknown[] = []> = {
  id: string
  titleKey: string
  build: (settings: TSettings, ...extraArgs: TExtraArgs) => ReactNode
}

export type SectionBuilder<TSettings, TExtraArgs extends unknown[] = []> = (
  settings: TSettings,
  ...extraArgs: TExtraArgs
) => ReactNode

/**
 * Section registry configuration
 */
export type SectionRegistryConfig<
  TSectionId extends string,
  TSettings,
  TExtraArgs extends unknown[] = [],
> = {
  sections: readonly SectionDefinition<TSettings, TExtraArgs>[]
  defaultSection: TSectionId
  basePath: string
  /** 'query' = `${basePath}?section=${id}`, 'path' = `${basePath}/${id}` */
  urlStyle?: 'query' | 'path'
}

/**
 * Create a section registry with helper functions
 */
export function createSectionRegistry<
  TSectionId extends string,
  TSettings,
  TExtraArgs extends unknown[] = [],
>(config: SectionRegistryConfig<TSectionId, TSettings, TExtraArgs>) {
  const meta = createSectionMeta<TSectionId>({
    ...config,
    sections: config.sections.map((section) => ({
      id: section.id as TSectionId,
      titleKey: section.titleKey,
    })),
  })

  function getSectionMeta(sectionId: TSectionId) {
    return (
      config.sections.find((item) => item.id === sectionId) ??
      config.sections[0]
    )
  }

  /**
   * Get section content by section ID
   */
  function getSectionContent(
    sectionId: TSectionId,
    settings: TSettings,
    ...extraArgs: TExtraArgs
  ) {
    return getSectionMeta(sectionId).build(settings, ...extraArgs)
  }

  return {
    sectionIds: meta.sectionIds,
    defaultSection: meta.defaultSection,
    getSectionNavItems: meta.getSectionNavItems,
    getSectionContent,
    getSectionMeta,
  }
}

/**
 * Attach section panels to section data created by `createSectionMeta`.
 * `builders` must have exactly one entry per section id, so the panel list
 * cannot drift from the ids the sidebar and routes use.
 */
export function createSectionContentRegistry<
  TSectionId extends string,
  TSettings,
  TExtraArgs extends unknown[] = [],
>(
  meta: SectionMeta<TSectionId>,
  builders: Record<TSectionId, SectionBuilder<TSettings, TExtraArgs>>
) {
  function getSectionContent(
    sectionId: TSectionId,
    settings: TSettings,
    ...extraArgs: TExtraArgs
  ) {
    return builders[meta.getSectionMeta(sectionId).id](settings, ...extraArgs)
  }

  return {
    getSectionContent,
    getSectionMeta: meta.getSectionMeta,
  }
}
