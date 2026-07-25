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
import { useTranslation } from 'react-i18next'

import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'

import type { UpdateInvoiceSettingRequest } from '../types'

interface R2StorageFieldsProps {
  value: UpdateInvoiceSettingRequest
  secretConfigured: boolean
  disabled: boolean
  invalid: boolean
  onChange: (value: UpdateInvoiceSettingRequest) => void
}

/** Edits the complete R2 credential group without owning form state. */
export function R2StorageFields(props: R2StorageFieldsProps) {
  const { t } = useTranslation()
  const describedBy = props.invalid ? 'invoice-settings-error' : undefined

  return (
    <FieldGroup>
      <Field
        data-invalid={props.invalid || undefined}
        data-disabled={props.disabled || undefined}
      >
        <FieldLabel htmlFor='invoice-r2-endpoint'>
          {t('R2 endpoint')}
        </FieldLabel>
        <Input
          id='invoice-r2-endpoint'
          value={props.value.r2_endpoint}
          disabled={props.disabled}
          aria-invalid={props.invalid || undefined}
          aria-describedby={describedBy}
          onChange={(event) =>
            props.onChange({
              ...props.value,
              r2_endpoint: event.target.value,
            })
          }
        />
      </Field>
      <div className='grid gap-4 sm:grid-cols-2'>
        <Field
          data-invalid={props.invalid || undefined}
          data-disabled={props.disabled || undefined}
        >
          <FieldLabel htmlFor='invoice-r2-bucket'>{t('R2 bucket')}</FieldLabel>
          <Input
            id='invoice-r2-bucket'
            value={props.value.r2_bucket}
            disabled={props.disabled}
            aria-invalid={props.invalid || undefined}
            aria-describedby={describedBy}
            onChange={(event) =>
              props.onChange({
                ...props.value,
                r2_bucket: event.target.value,
              })
            }
          />
        </Field>
        <Field
          data-invalid={props.invalid || undefined}
          data-disabled={props.disabled || undefined}
        >
          <FieldLabel htmlFor='invoice-r2-access-key'>
            {t('R2 access key ID')}
          </FieldLabel>
          <Input
            id='invoice-r2-access-key'
            value={props.value.r2_access_key_id}
            disabled={props.disabled}
            aria-invalid={props.invalid || undefined}
            aria-describedby={describedBy}
            onChange={(event) =>
              props.onChange({
                ...props.value,
                r2_access_key_id: event.target.value,
              })
            }
          />
        </Field>
      </div>
      <Field
        data-invalid={props.invalid || undefined}
        data-disabled={props.disabled || undefined}
      >
        <FieldLabel htmlFor='invoice-r2-secret'>
          {t('R2 secret access key')}
        </FieldLabel>
        <Input
          id='invoice-r2-secret'
          type='password'
          autoComplete='new-password'
          value={props.value.r2_secret_access_key}
          disabled={props.disabled}
          aria-invalid={props.invalid || undefined}
          aria-describedby={describedBy}
          onChange={(event) =>
            props.onChange({
              ...props.value,
              r2_secret_access_key: event.target.value,
            })
          }
        />
        <FieldDescription>
          {props.secretConfigured
            ? t('Leave blank to keep the current secret.')
            : t('Enter the R2 secret access key.')}
        </FieldDescription>
      </Field>
    </FieldGroup>
  )
}
