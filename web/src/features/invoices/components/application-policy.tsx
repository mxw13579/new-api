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
import { Skeleton } from '@/components/ui/skeleton'

import type { InvoiceConfig } from '../types'

interface ApplicationPolicyProps {
  config: InvoiceConfig | undefined
  loading: boolean
  error: boolean
  retry: () => void
  formatAmount: (minor: number) => string
}

/** Renders the invoice eligibility, fee, and retention policy. */
export function ApplicationPolicy(props: ApplicationPolicyProps) {
  const { t } = useTranslation()
  if (props.error) {
    return (
      <Alert variant='destructive'>
        <AlertTitle>{t('Invoice policy failed to load')}</AlertTitle>
        <AlertDescription>
          <Button variant='outline' size='sm' onClick={props.retry}>
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  }
  if (props.loading) return <Skeleton className='h-20 w-full' />

  return (
    <Alert>
      <AlertTitle>{t('Invoice policy')}</AlertTitle>
      <AlertDescription>
        {t(
          'Orders from the last {{days}} days are eligible. Minimum {{minimum}}, invoice fee {{fee}}%, PDF retention {{retention}} days.',
          {
            days: props.config?.application_window_days,
            minimum: props.formatAmount(
              props.config?.minimum_amount_minor ?? 0
            ),
            fee: props.config?.fee_percent,
            retention: props.config?.pdf_retention_days,
          }
        )}
      </AlertDescription>
    </Alert>
  )
}
