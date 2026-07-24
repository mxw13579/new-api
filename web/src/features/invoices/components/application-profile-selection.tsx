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

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'

import type { InvoiceConfig, InvoiceProfile } from '../types'
import { isInvoiceProfileEnabled } from '../user-workspace'

interface ApplicationProfileSelectionProps {
  config: InvoiceConfig | undefined
  profiles: InvoiceProfile[]
  selectedProfileId: number
  onSelectedProfileIdChange: (profileId: number) => void
  loading: boolean
  error: boolean
  retry: () => void
}

/** Selects the versioned invoice identity used by an application draft. */
export function ApplicationProfileSelection(
  props: ApplicationProfileSelectionProps
) {
  const { t } = useTranslation()

  return (
    <Field>
      <FieldLabel htmlFor='invoice-profile'>{t('Invoice profile')}</FieldLabel>
      <NativeSelect
        id='invoice-profile'
        className='w-full'
        value={props.selectedProfileId || ''}
        onChange={(event) =>
          props.onSelectedProfileIdChange(Number(event.target.value))
        }
      >
        <NativeSelectOption value=''>
          {t('Select an invoice profile')}
        </NativeSelectOption>
        {props.profiles.map((profile) => {
          const enabled =
            !props.config || isInvoiceProfileEnabled(props.config, profile.type)
          return (
            <NativeSelectOption
              key={profile.id}
              value={profile.id}
              disabled={!enabled}
            >
              {profile.title} 路 v{profile.version}
              {!enabled ? ` (${t('Disabled for new applications')})` : ''}
            </NativeSelectOption>
          )
        })}
      </NativeSelect>
      <FieldDescription>
        {t('The selected profile version is submitted with the application.')}
      </FieldDescription>
      {props.loading ? <Skeleton className='h-8 w-full' /> : null}
      {props.error ? (
        <Alert variant='destructive'>
          <AlertTitle>{t('Invoice profiles failed to load')}</AlertTitle>
          <AlertDescription>
            <Button variant='outline' size='sm' onClick={props.retry}>
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
    </Field>
  )
}
