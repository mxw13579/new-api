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
import type { ContentSettings } from '../types'
import { createSectionContentRegistry } from '../utils/section-registry'
import { AnnouncementsSection } from './announcements-section'
import { ApiInfoSection } from './api-info-section'
import { ChatSettingsSection } from './chat-settings-section'
import { DashboardSection } from './dashboard-section'
import { DrawingSettingsSection } from './drawing-settings-section'
import { FAQSection } from './faq-section'
import { CONTENT_SECTION_META, type ContentSectionId } from './section-meta'
import { UptimeKumaSection } from './uptime-kuma-section'

/**
 * Validate and coerce DataExportDefaultTime to a safe value
 */
function validateDataExportDefaultTime(value: string): 'week' | 'hour' | 'day' {
  if (value === 'week' || value === 'hour' || value === 'day') {
    return value
  }
  // Default to 'hour' if value is unexpected
  return 'hour'
}

// One builder per section id; ids, titles, and order live in section-meta.ts.
const contentRegistry = createSectionContentRegistry<
  ContentSectionId,
  ContentSettings
>(CONTENT_SECTION_META, {
  dashboard: (settings: ContentSettings) => (
    <DashboardSection
      defaultValues={{
        DataExportEnabled: settings.DataExportEnabled,
        DataExportInterval: settings.DataExportInterval,
        DataExportDefaultTime: validateDataExportDefaultTime(
          settings.DataExportDefaultTime
        ),
      }}
    />
  ),
  announcements: (settings: ContentSettings) => (
    <AnnouncementsSection
      enabled={settings['console_setting.announcements_enabled']}
      data={settings['console_setting.announcements']}
    />
  ),
  'api-info': (settings: ContentSettings) => (
    <ApiInfoSection
      enabled={settings['console_setting.api_info_enabled']}
      data={settings['console_setting.api_info']}
    />
  ),
  faq: (settings: ContentSettings) => (
    <FAQSection
      enabled={settings['console_setting.faq_enabled']}
      data={settings['console_setting.faq']}
    />
  ),
  'uptime-kuma': (settings: ContentSettings) => (
    <UptimeKumaSection
      enabled={settings['console_setting.uptime_kuma_enabled']}
      data={settings['console_setting.uptime_kuma_groups']}
    />
  ),
  chat: (settings: ContentSettings) => (
    <ChatSettingsSection defaultValue={settings.Chats} />
  ),
  drawing: (settings: ContentSettings) => (
    <DrawingSettingsSection
      defaultValues={{
        DrawingEnabled: settings.DrawingEnabled,
        MjNotifyEnabled: settings.MjNotifyEnabled,
        MjAccountFilterEnabled: settings.MjAccountFilterEnabled,
        MjForwardUrlEnabled: settings.MjForwardUrlEnabled,
        MjModeClearEnabled: settings.MjModeClearEnabled,
        MjActionCheckSuccessEnabled: settings.MjActionCheckSuccessEnabled,
      }}
    />
  ),
})

export const getContentSectionContent = contentRegistry.getSectionContent
export const getContentSectionMeta = contentRegistry.getSectionMeta
