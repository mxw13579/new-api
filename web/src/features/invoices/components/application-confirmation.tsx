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
import { InvoiceIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { RefObject } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import { Spinner } from '@/components/ui/spinner'
import { useIsMobile } from '@/hooks/use-mobile'

interface ApplicationConfirmationProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  reviewButtonRef: RefObject<HTMLButtonElement | null>
  profileTitle: string | undefined
  profileVersion: number | undefined
  selectedOrderCount: number
  formattedAmount: string
  feeQuota: number
  pending: boolean
  onSubmit: () => void
}

/** Confirms the immutable profile, complete orders, amount, and fee. */
export function ApplicationConfirmation(props: ApplicationConfirmationProps) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const confirmation = (
    <div className='space-y-3 px-4 pb-4 sm:px-0 sm:pb-0'>
      <dl className='grid gap-2 text-sm'>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Invoice profile')}</dt>
          <dd className='text-right font-medium'>{props.profileTitle}</dd>
        </div>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Profile version')}</dt>
          <dd>{props.profileVersion}</dd>
        </div>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Selected orders')}</dt>
          <dd>{props.selectedOrderCount}</dd>
        </div>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Invoice amount')}</dt>
          <dd className='font-medium'>{props.formattedAmount}</dd>
        </div>
        <div className='flex justify-between gap-4'>
          <dt className='text-muted-foreground'>{t('Wallet quota fee')}</dt>
          <dd>{props.feeQuota}</dd>
        </div>
      </dl>
    </div>
  )
  const submitButton = (
    <Button
      autoFocus={isMobile}
      onClick={props.onSubmit}
      disabled={props.pending}
    >
      {props.pending ? (
        <Spinner data-icon='inline-start' />
      ) : (
        <HugeiconsIcon
          icon={InvoiceIcon}
          strokeWidth={2}
          data-icon='inline-start'
        />
      )}
      {t('Submit invoice application')}
    </Button>
  )

  if (isMobile) {
    return (
      <Drawer
        open={props.open}
        onOpenChange={(open) => {
          props.onOpenChange(open)
          if (!open) {
            requestAnimationFrame(() => props.reviewButtonRef.current?.focus())
          }
        }}
      >
        <DrawerContent>
          <DrawerHeader>
            <DrawerTitle>{t('Confirm invoice application')}</DrawerTitle>
            <DrawerDescription>
              {t(
                'Confirm the profile version, complete orders, amount, and fee.'
              )}
            </DrawerDescription>
          </DrawerHeader>
          {confirmation}
          <DrawerFooter>
            {submitButton}
            <Button variant='outline' onClick={() => props.onOpenChange(false)}>
              {t('Cancel')}
            </Button>
          </DrawerFooter>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('Confirm invoice application')}</DialogTitle>
          <DialogDescription>
            {t(
              'Confirm the profile version, complete orders, amount, and fee.'
            )}
          </DialogDescription>
        </DialogHeader>
        {confirmation}
        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          {submitButton}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
