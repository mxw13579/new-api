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
import { createSectionMeta } from '../utils/section-meta'

export const CONTENT_SECTION_META = createSectionMeta({
  sections: [
    { id: 'dashboard', titleKey: 'Data Dashboard' },
    { id: 'announcements', titleKey: 'Announcements' },
    { id: 'api-info', titleKey: 'API Addresses' },
    { id: 'faq', titleKey: 'FAQ' },
    { id: 'uptime-kuma', titleKey: 'Uptime Kuma' },
    { id: 'chat', titleKey: 'Chat Presets' },
    { id: 'drawing', titleKey: 'Drawing' },
  ],
  defaultSection: 'dashboard',
  basePath: '/system-settings/content',
  urlStyle: 'path',
})

export type ContentSectionId = (typeof CONTENT_SECTION_META.sectionIds)[number]

export const CONTENT_SECTION_IDS = CONTENT_SECTION_META.sectionIds
export const CONTENT_DEFAULT_SECTION = CONTENT_SECTION_META.defaultSection
export const getContentSectionNavItems = CONTENT_SECTION_META.getSectionNavItems
