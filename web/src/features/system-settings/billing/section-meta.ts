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

export const BILLING_SECTION_META = createSectionMeta({
  sections: [
    { id: 'quota', titleKey: 'Quota Settings' },
    { id: 'currency', titleKey: 'Currency & Display' },
    { id: 'model-pricing', titleKey: 'Model Pricing' },
    { id: 'group-pricing', titleKey: 'Group Pricing' },
    { id: 'payment', titleKey: 'Payment Gateway' },
    { id: 'checkin', titleKey: 'Check-in Rewards' },
  ],
  defaultSection: 'quota',
  basePath: '/system-settings/billing',
  urlStyle: 'path',
})

export type BillingSectionId = (typeof BILLING_SECTION_META.sectionIds)[number]

export const BILLING_SECTION_IDS = BILLING_SECTION_META.sectionIds
export const BILLING_DEFAULT_SECTION = BILLING_SECTION_META.defaultSection
export const getBillingSectionNavItems = BILLING_SECTION_META.getSectionNavItems
