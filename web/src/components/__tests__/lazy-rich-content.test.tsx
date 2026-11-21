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
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { LazyRichContent } from '../lazy-rich-content'

// The first markdown render must run before any test loads the renderer:
// React.lazy keeps the resolved module for the rest of the file.
describe('LazyRichContent', () => {
  it('holds the place with the content classes while the markdown renderer loads, then renders it', async () => {
    const { container } = render(
      <LazyRichContent content='# Welcome' className='custom-home-content' />
    )

    const placeholder = container.querySelector('[aria-busy="true"]')
    expect(placeholder).toHaveClass('custom-home-content')

    expect(
      await screen.findByRole('heading', { name: 'Welcome' })
    ).toBeInTheDocument()
    expect(container.querySelector('[aria-busy="true"]')).toBeNull()
    expect(container.firstElementChild).toHaveClass('custom-home-content')
  })

  it('renders HTML content without waiting for the markdown renderer', () => {
    const { container } = render(
      <LazyRichContent mode='html' content='<p>Hello</p>' />
    )

    expect(screen.getByText('Hello')).toBeInTheDocument()
    expect(container.querySelector('[aria-busy="true"]')).toBeNull()
  })
})
