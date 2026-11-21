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
import { createSectionContentRegistry } from '../utils/section-registry'
import { ChannelHealthSection } from './channel-health-section'
import type { RequestPolicySettings } from './defaults'
import { RoutingPolicySection } from './routing-section'
import { POLICY_SECTION_META, type PolicySectionId } from './section-meta'

// One builder per section id; ids, titles, and order live in section-meta.ts.
const registry = createSectionContentRegistry<
  PolicySectionId,
  RequestPolicySettings
>(POLICY_SECTION_META, {
  routing: () => <RoutingPolicySection />,
  health: (settings: RequestPolicySettings) => (
    <ChannelHealthSection defaultValues={settings} />
  ),
})

export const getPolicySectionContent = registry.getSectionContent
export const getPolicySectionMeta = registry.getSectionMeta
