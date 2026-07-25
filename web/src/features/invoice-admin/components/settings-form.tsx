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

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

import { validateInvoiceSetting } from '../contract'
import type { InvoiceSetting, UpdateInvoiceSettingRequest } from '../types'
import { R2StorageFields } from './r2-storage-fields'

interface SettingsFormProps {
  setting: InvoiceSetting
  pending: boolean
  onSave: (setting: UpdateInvoiceSettingRequest) => void
}

/** Edits invoice policy and database-backed R2 storage settings. */
export function SettingsForm(props: SettingsFormProps) {
  const { t } = useTranslation()
  const [setting, setSetting] = useState<UpdateInvoiceSettingRequest>({
    personal_enabled: props.setting.personal_enabled,
    company_enabled: props.setting.company_enabled,
    application_window_days: props.setting.application_window_days,
    minimum_amount_minor: props.setting.minimum_amount_minor,
    fee_percent: props.setting.fee_percent,
    pdf_retention_days: props.setting.pdf_retention_days,
    r2_endpoint: props.setting.r2_endpoint,
    r2_bucket: props.setting.r2_bucket,
    r2_access_key_id: props.setting.r2_access_key_id,
    r2_secret_access_key: '',
  })
  const [errorKey, setErrorKey] = useState<string | null>(null)
  const r2Invalid = errorKey === 'Enter a complete R2 configuration'

  function numberField(
    field:
      | 'application_window_days'
      | 'minimum_amount_minor'
      | 'fee_percent'
      | 'pdf_retention_days',
    value: string
  ): void {
    setSetting((current) => ({ ...current, [field]: Number(value) }))
  }

  function submit(event: React.FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    const result = validateInvoiceSetting(
      setting,
      props.setting.r2_secret_configured
    )
    if (!result.ok) {
      setErrorKey(result.errorKey)
      return
    }
    setErrorKey(null)
    props.onSave(result.data)
  }

  return (
    <form onSubmit={submit} aria-busy={props.pending}>
      <FieldSet disabled={props.pending}>
        <FieldLegend>{t('Invoice policy')}</FieldLegend>
        <FieldDescription>
          {t('All invoice policy fields are saved together.')}
        </FieldDescription>
        <FieldGroup>
          <Field
            orientation='horizontal'
            data-disabled={props.pending || undefined}
          >
            <FieldLabel htmlFor='invoice-personal-enabled'>
              {t('Enable personal invoices')}
            </FieldLabel>
            <Switch
              id='invoice-personal-enabled'
              checked={setting.personal_enabled}
              disabled={props.pending}
              onCheckedChange={(checked) =>
                setSetting((current) => ({
                  ...current,
                  personal_enabled: checked,
                }))
              }
            />
          </Field>
          <Field
            orientation='horizontal'
            data-disabled={props.pending || undefined}
          >
            <FieldLabel htmlFor='invoice-company-enabled'>
              {t('Enable company invoices')}
            </FieldLabel>
            <Switch
              id='invoice-company-enabled'
              checked={setting.company_enabled}
              disabled={props.pending}
              onCheckedChange={(checked) =>
                setSetting((current) => ({
                  ...current,
                  company_enabled: checked,
                }))
              }
            />
          </Field>
          <div className='grid gap-4 sm:grid-cols-2'>
            <Field
              data-invalid={errorKey ? true : undefined}
              data-disabled={props.pending || undefined}
            >
              <FieldLabel htmlFor='invoice-application-window'>
                {t('Application window days')}
              </FieldLabel>
              <Input
                id='invoice-application-window'
                type='number'
                min={1}
                step={1}
                value={setting.application_window_days}
                disabled={props.pending}
                aria-invalid={errorKey ? true : undefined}
                aria-describedby={
                  errorKey ? 'invoice-settings-error' : undefined
                }
                onChange={(event) =>
                  numberField('application_window_days', event.target.value)
                }
              />
            </Field>
            <Field
              data-invalid={errorKey ? true : undefined}
              data-disabled={props.pending || undefined}
            >
              <FieldLabel htmlFor='invoice-minimum-amount'>
                {t('Minimum amount in minor units')}
              </FieldLabel>
              <Input
                id='invoice-minimum-amount'
                type='number'
                min={0}
                step={1}
                value={setting.minimum_amount_minor}
                disabled={props.pending}
                aria-invalid={errorKey ? true : undefined}
                aria-describedby={
                  errorKey ? 'invoice-settings-error' : undefined
                }
                onChange={(event) =>
                  numberField('minimum_amount_minor', event.target.value)
                }
              />
            </Field>
            <Field
              data-invalid={errorKey ? true : undefined}
              data-disabled={props.pending || undefined}
            >
              <FieldLabel htmlFor='invoice-fee-percent'>
                {t('Invoice fee percentage')}
              </FieldLabel>
              <Input
                id='invoice-fee-percent'
                type='number'
                min={0}
                max={100}
                step={1}
                value={setting.fee_percent}
                disabled={props.pending}
                aria-invalid={errorKey ? true : undefined}
                aria-describedby={
                  errorKey ? 'invoice-settings-error' : undefined
                }
                onChange={(event) =>
                  numberField('fee_percent', event.target.value)
                }
              />
            </Field>
            <Field
              data-invalid={errorKey ? true : undefined}
              data-disabled={props.pending || undefined}
            >
              <FieldLabel htmlFor='invoice-pdf-retention'>
                {t('PDF retention days')}
              </FieldLabel>
              <Input
                id='invoice-pdf-retention'
                type='number'
                min={1}
                step={1}
                value={setting.pdf_retention_days}
                disabled={props.pending}
                aria-invalid={errorKey ? true : undefined}
                aria-describedby={
                  errorKey ? 'invoice-settings-error' : undefined
                }
                onChange={(event) =>
                  numberField('pdf_retention_days', event.target.value)
                }
              />
            </Field>
          </div>
          <R2StorageFields
            value={setting}
            secretConfigured={props.setting.r2_secret_configured}
            disabled={props.pending}
            invalid={r2Invalid}
            onChange={setSetting}
          />
          {errorKey ? (
            <FieldDescription id='invoice-settings-error' role='alert'>
              {t(errorKey)}
            </FieldDescription>
          ) : null}
          <Button type='submit' disabled={props.pending}>
            {props.pending ? <Spinner data-icon='inline-start' /> : null}
            {t('Save invoice settings')}
          </Button>
        </FieldGroup>
      </FieldSet>
    </form>
  )
}
