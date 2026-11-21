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

export const OPERATIONS_SECTION_META = createSectionMeta({
  sections: [
    { id: 'behavior', titleKey: 'System Behavior' },
    { id: 'alerts', titleKey: 'Model performance metrics' },
    { id: 'email', titleKey: 'Email & Notifications' },
    { id: 'worker', titleKey: 'Worker Proxy' },
    { id: 'logs', titleKey: 'Log Maintenance' },
    { id: 'performance', titleKey: 'Performance' },
    { id: 'update-checker', titleKey: 'System maintenance' },
  ],
  defaultSection: 'behavior',
  basePath: '/system-settings/operations',
  urlStyle: 'path',
})

export type OperationsSectionId =
  (typeof OPERATIONS_SECTION_META.sectionIds)[number]

export const OPERATIONS_SECTION_IDS = OPERATIONS_SECTION_META.sectionIds
export const OPERATIONS_DEFAULT_SECTION = OPERATIONS_SECTION_META.defaultSection
export const getOperationsSectionNavItems =
  OPERATIONS_SECTION_META.getSectionNavItems
