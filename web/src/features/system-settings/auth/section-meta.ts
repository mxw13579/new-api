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

export const AUTH_SECTION_META = createSectionMeta({
  sections: [
    { id: 'basic-auth', titleKey: 'Basic Authentication' },
    { id: 'oauth', titleKey: 'OAuth Integrations' },
    { id: 'passkey', titleKey: 'Passkey Authentication' },
    { id: 'bot-protection', titleKey: 'Bot Protection' },
    { id: 'custom-oauth', titleKey: 'Custom OAuth' },
  ],
  defaultSection: 'basic-auth',
  basePath: '/system-settings/auth',
  urlStyle: 'path',
})

export type AuthSectionId = (typeof AUTH_SECTION_META.sectionIds)[number]

export const AUTH_SECTION_IDS = AUTH_SECTION_META.sectionIds
export const AUTH_DEFAULT_SECTION = AUTH_SECTION_META.defaultSection
export const getAuthSectionNavItems = AUTH_SECTION_META.getSectionNavItems
