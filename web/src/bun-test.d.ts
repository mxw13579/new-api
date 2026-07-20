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
declare module 'bun:test' {
  type TestCallback = () => void | Promise<void>

  interface Matchers<T> {
    toBe(expected: T): void
    toContain(expected: string): void
    toEqual(expected: unknown): void
  }

  export function describe(name: string, callback: TestCallback): void
  export function expect<T>(actual: T): Matchers<T>
  export function it(name: string, callback: TestCallback): void
}
