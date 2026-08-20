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
import { describe, it } from 'bun:test'
import assert from 'node:assert/strict'

import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

type MockModule = (
  specifier: string,
  factory: () => Record<string, unknown>
) => void
const mockModule = (
  (await import('bun:test')) as unknown as { mock: { module: MockModule } }
).mock.module

mockModule('react-i18next', () => ({
  I18nextProvider: (props: { children?: ReactNode }) => props.children,
  initReactI18next: {
    type: '3rdParty',
    init: () => undefined,
  },
  useTranslation: () => ({ t: (key: string) => key }),
}))

const { ApplicationProfileSelection } =
  await import('./application-profile-selection')

describe('invoice application profile selection', () => {
  it('renders the intended profile version separator without mojibake', () => {
    const html = renderToStaticMarkup(
      <ApplicationProfileSelection
        config={undefined}
        profiles={[
          {
            id: 7,
            type: 'company',
            title: 'Acme',
            tax_number: '91310000TEST',
            identity_card_number: '',
            is_default: true,
            version: 12,
            created_at: 1,
            updated_at: 2,
          },
        ]}
        selectedProfileId={7}
        onSelectedProfileIdChange={() => undefined}
        loading={false}
        error={false}
        retry={() => undefined}
      />
    )

    assert.match(html, />Acme · v12<\/option>/)
    assert.doesNotMatch(html, /路|璺/)
  })
})
