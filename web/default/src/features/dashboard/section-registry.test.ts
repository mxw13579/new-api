import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  DASHBOARD_SECTION_IDS,
  getDashboardSectionNavItems,
} from './section-registry'

describe('dashboard section registry', () => {
  test('registers distribution as an independent dashboard section', () => {
    assert.ok(DASHBOARD_SECTION_IDS.includes('distribution'))

    const navItems = getDashboardSectionNavItems(
      ((key: string) => key) as never,
      { isAdmin: false }
    )

    assert.ok(navItems.some((item) => item.id === 'distribution'))
  })
})
