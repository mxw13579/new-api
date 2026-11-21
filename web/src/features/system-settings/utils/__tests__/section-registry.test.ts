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
import { describe, expect, it } from 'vitest'

import { createSectionMeta } from '../section-meta'
import { createSectionContentRegistry } from '../section-registry'

const meta = createSectionMeta({
  sections: [
    { id: 'first', titleKey: 'First section' },
    { id: 'second', titleKey: 'Second section' },
  ],
  defaultSection: 'first',
  basePath: '/system-settings/example',
  urlStyle: 'path',
})

const registry = createSectionContentRegistry<
  (typeof meta.sectionIds)[number],
  { label: string }
>(meta, {
  first: (settings) => `first:${settings.label}`,
  second: (settings) => `second:${settings.label}`,
})

describe('section content registry', () => {
  it('renders the builder that matches a known section id', () => {
    expect(registry.getSectionContent('second', { label: 'x' })).toBe(
      'second:x'
    )
    expect(registry.getSectionMeta('second').titleKey).toBe('Second section')
  })

  it('falls back to the first section for an unknown section id', () => {
    const unknown = 'missing' as (typeof meta.sectionIds)[number]

    expect(registry.getSectionContent(unknown, { label: 'x' })).toBe('first:x')
    expect(registry.getSectionMeta(unknown).titleKey).toBe('First section')
  })
})
