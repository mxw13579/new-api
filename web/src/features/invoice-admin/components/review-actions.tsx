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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldSet,
  FieldLegend,
} from '@/components/ui/field'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'

import { getInvoiceReviewActions } from '../contract'
import type { InvoiceApplicationDetail } from '../types'

export type ReviewAction = 'reviewing' | 'approve' | 'reject' | null

interface ReviewActionsProps {
  application: InvoiceApplicationDetail
  pendingAction: ReviewAction
  onReview: (action: 'reviewing' | 'approve') => void
  onReject: (reason: string) => Promise<void>
}

/** Renders concurrency-safe administrator review transitions. */
export function ReviewActions(props: ReviewActionsProps) {
  const { t } = useTranslation()
  const [rejectOpen, setRejectOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [reasonInvalid, setReasonInvalid] = useState(false)
  const actions = getInvoiceReviewActions(props.application.status)
  const pending = props.pendingAction !== null

  if (actions.length === 0) return null

  async function reject(): Promise<void> {
    const trimmedReason = reason.trim()
    if (trimmedReason.length === 0) {
      setReasonInvalid(true)
      return
    }
    try {
      await props.onReject(trimmedReason)
      setRejectOpen(false)
      setReason('')
    } catch {
      // The mutation owns the stable page-level error; keep the dialog open.
    }
  }

  return (
    <FieldSet>
      <FieldLegend variant='label'>{t('Review decision')}</FieldLegend>
      <FieldGroup className='gap-3'>
        <div className='flex flex-wrap gap-2'>
          {actions.includes('reviewing') ? (
            <Button
              variant='outline'
              disabled={pending}
              onClick={() => props.onReview('reviewing')}
            >
              {props.pendingAction === 'reviewing' ? (
                <Spinner data-icon='inline-start' />
              ) : null}
              {t('Start review')}
            </Button>
          ) : null}
          {actions.includes('approve') ? (
            <Button
              disabled={pending}
              onClick={() => props.onReview('approve')}
            >
              {props.pendingAction === 'approve' ? (
                <Spinner data-icon='inline-start' />
              ) : null}
              {t('Approve invoice')}
            </Button>
          ) : null}
          <AlertDialog
            open={rejectOpen}
            onOpenChange={(open) => {
              if (props.pendingAction !== 'reject') setRejectOpen(open)
            }}
          >
            <AlertDialogTrigger
              render={<Button variant='destructive' disabled={pending} />}
            >
              {t('Reject invoice')}
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{t('Reject this invoice?')}</AlertDialogTitle>
                <AlertDialogDescription>
                  {t(
                    'The rejection reason will be visible in the application record.'
                  )}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <FieldGroup>
                <Field data-invalid={reasonInvalid || undefined}>
                  <FieldLabel htmlFor='invoice-reject-reason'>
                    {t('Rejection reason')}
                  </FieldLabel>
                  <Textarea
                    id='invoice-reject-reason'
                    autoFocus
                    value={reason}
                    disabled={props.pendingAction === 'reject'}
                    aria-invalid={reasonInvalid || undefined}
                    aria-describedby={
                      reasonInvalid ? 'invoice-reject-reason-error' : undefined
                    }
                    onChange={(event) => {
                      setReason(event.target.value)
                      if (event.target.value.trim()) setReasonInvalid(false)
                    }}
                  />
                  {reasonInvalid ? (
                    <FieldDescription id='invoice-reject-reason-error'>
                      {t('A rejection reason is required')}
                    </FieldDescription>
                  ) : null}
                </Field>
              </FieldGroup>
              <AlertDialogFooter>
                <AlertDialogCancel disabled={props.pendingAction === 'reject'}>
                  {t('Cancel')}
                </AlertDialogCancel>
                <AlertDialogAction
                  variant='destructive'
                  disabled={props.pendingAction === 'reject'}
                  onClick={(event) => {
                    event.preventDefault()
                    void reject()
                  }}
                >
                  {props.pendingAction === 'reject' ? (
                    <Spinner data-icon='inline-start' />
                  ) : null}
                  {t('Reject invoice')}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
      </FieldGroup>
    </FieldSet>
  )
}
