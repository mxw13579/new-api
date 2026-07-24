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
import type { Dispatch, SetStateAction } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'

import type { InvoiceConfig, InvoiceType } from '../types'

export interface ProfileDraft {
  id: number | null
  type: InvoiceType
  title: string
  taxNumber: string
  isDefault: boolean
  version: number
}

interface ProfileEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  draft: ProfileDraft
  setDraft: Dispatch<SetStateAction<ProfileDraft>>
  config: InvoiceConfig | undefined
  saving: boolean
  onSave: () => void
}

/** Edits a versioned personal or company invoice identity. */
export function ProfileEditor(props: ProfileEditorProps) {
  const { t } = useTranslation()

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {props.draft.id === null
              ? t('Add invoice profile')
              : t('Edit invoice profile')}
          </DialogTitle>
          <DialogDescription>
            {props.draft.id === null
              ? t('Create a personal or company invoice identity.')
              : t('Saving requires the current profile version.')}
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field data-disabled={props.draft.id !== null || undefined}>
            <FieldLabel htmlFor='invoice-profile-type'>
              {t('Profile type')}
            </FieldLabel>
            <NativeSelect
              id='invoice-profile-type'
              className='w-full'
              value={props.draft.type}
              disabled={props.draft.id !== null}
              onChange={(event) =>
                props.setDraft((current) => ({
                  ...current,
                  type: event.target.value as InvoiceType,
                  taxNumber:
                    event.target.value === 'personal' ? '' : current.taxNumber,
                }))
              }
            >
              <NativeSelectOption
                value='personal'
                disabled={
                  props.draft.id === null &&
                  !!props.config &&
                  !props.config.personal_enabled
                }
              >
                {t('Personal')}
              </NativeSelectOption>
              <NativeSelectOption
                value='company'
                disabled={
                  props.draft.id === null &&
                  !!props.config &&
                  !props.config.company_enabled
                }
              >
                {t('Company')}
              </NativeSelectOption>
            </NativeSelect>
          </Field>
          <Field>
            <FieldLabel htmlFor='invoice-profile-title'>
              {props.draft.type === 'personal'
                ? t('Full name')
                : t('Company name')}
            </FieldLabel>
            <Input
              id='invoice-profile-title'
              value={props.draft.title}
              onChange={(event) =>
                props.setDraft((current) => ({
                  ...current,
                  title: event.target.value,
                }))
              }
            />
          </Field>
          {props.draft.type === 'company' ? (
            <Field>
              <FieldLabel htmlFor='invoice-profile-tax-number'>
                {t('Tax number')}
              </FieldLabel>
              <Input
                id='invoice-profile-tax-number'
                value={props.draft.taxNumber}
                onChange={(event) =>
                  props.setDraft((current) => ({
                    ...current,
                    taxNumber: event.target.value,
                  }))
                }
              />
            </Field>
          ) : null}
          <Field orientation='horizontal'>
            <Checkbox
              id='invoice-profile-default'
              checked={props.draft.isDefault}
              onCheckedChange={(checked) =>
                props.setDraft((current) => ({
                  ...current,
                  isDefault: checked,
                }))
              }
            />
            <div>
              <FieldLabel htmlFor='invoice-profile-default'>
                {t('Set as default')}
              </FieldLabel>
              <FieldDescription>
                {t('Default is maintained separately for each profile type.')}
              </FieldDescription>
            </div>
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={
              props.saving ||
              !props.draft.title.trim() ||
              (props.draft.type === 'company' && !props.draft.taxNumber.trim())
            }
            onClick={props.onSave}
          >
            {props.saving ? <Spinner data-icon='inline-start' /> : null}
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
