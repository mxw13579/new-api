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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  runPromptSubmission,
  shouldClearSubmittedText,
} from './prompt-submission'

describe('runPromptSubmission', () => {
  test('does not clear text entered while an earlier submission is pending', () => {
    assert.equal(shouldClearSubmittedText('next message', 'submitted'), false)
    assert.equal(shouldClearSubmittedText('submitted', 'submitted'), true)
  })

  for (const failurePoint of ['conversion', 'submit'] as const) {
    test(`reports a ${failurePoint} rejection without running success cleanup`, async () => {
      const error = new Error(`${failurePoint} failed`)
      let successCalls = 0
      const reportedErrors: unknown[] = []

      await runPromptSubmission({
        text: 'retryable draft',
        event: 'submit-event',
        convertFiles: async () => {
          if (failurePoint === 'conversion') {
            throw error
          }
          return ['attachment']
        },
        submit: async () => {
          if (failurePoint === 'submit') {
            throw error
          }
        },
        onSuccess: () => {
          successCalls += 1
        },
        onError: (reportedError) => {
          reportedErrors.push(reportedError)
        },
      })

      assert.equal(successCalls, 0)
      assert.deepEqual(reportedErrors, [error])
    })
  }

  test('runs cleanup only after conversion and submit succeed', async () => {
    let successCalls = 0
    const submissions: unknown[][] = []
    const submit = async (...arguments_: unknown[]) => {
      submissions.push(arguments_)
    }

    await runPromptSubmission({
      text: 'message',
      event: 'submit-event',
      convertFiles: async () => ['attachment'],
      submit,
      onSuccess: () => {
        successCalls += 1
      },
      onError: () => {
        throw new Error('unexpected error')
      },
    })

    assert.deepEqual(submissions[0], [
      { text: 'message', files: ['attachment'] },
      'submit-event',
    ])
    assert.equal(successCalls, 1)
  })
})
