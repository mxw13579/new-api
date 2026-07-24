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
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'

import { DOCUMENT_STATUS_CONFIG } from '../../invoices/contract'
import { PROTECTED_INVOICE_VALUE_KEY } from '../contract'
import type { InvoiceApplicationDetail } from '../types'

interface ApplicationSectionProps {
  application: InvoiceApplicationDetail
}

/** Displays the immutable, permission-masked billing profile snapshot. */
export function ApplicationProfileSection({
  application,
}: ApplicationSectionProps) {
  const { t } = useTranslation()
  const profile = application.profile_snapshot
  const title =
    profile.title === PROTECTED_INVOICE_VALUE_KEY
      ? t(PROTECTED_INVOICE_VALUE_KEY)
      : profile.title
  const taxNumber =
    profile.tax_number === PROTECTED_INVOICE_VALUE_KEY
      ? t(PROTECTED_INVOICE_VALUE_KEY)
      : profile.tax_number

  return (
    <section aria-labelledby='invoice-profile-heading'>
      <h3 id='invoice-profile-heading' className='mb-2 font-medium'>
        {t('Profile snapshot')}
      </h3>
      <dl className='grid min-w-0 gap-3 rounded-lg border p-3 sm:grid-cols-2'>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>{t('Invoice title')}</dt>
          <dd className='break-words'>{title}</dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>{t('Tax identifier')}</dt>
          <dd className='break-all'>{taxNumber}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Invoice type')}</dt>
          <dd>{t(profile.type === 'company' ? 'Company' : 'Personal')}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Snapshot version')}</dt>
          <dd>{profile.version}</dd>
        </div>
      </dl>
    </section>
  )
}

/** Displays the permanently retained order snapshots for an application. */
export function ApplicationItemsSection({
  application,
}: ApplicationSectionProps) {
  const { t } = useTranslation()

  return (
    <section aria-labelledby='invoice-orders-heading'>
      <h3 id='invoice-orders-heading' className='mb-2 font-medium'>
        {t('Invoice items')}
      </h3>
      <ul className='space-y-2'>
        {application.items.map((item) => (
          <li key={item.topup_id} className='min-w-0 rounded-lg border p-3'>
            <p className='font-medium break-all'>{item.order_no}</p>
            <p className='text-muted-foreground break-words'>
              {item.product_description}
            </p>
            <p>
              {new Intl.NumberFormat(undefined, {
                style: 'currency',
                currency: 'CNY',
              }).format(item.paid_amount_minor / 100)}
            </p>
          </li>
        ))}
      </ul>
    </section>
  )
}

/** Displays immutable invoice issuance and document facts when available. */
export function ApplicationIssuanceSection({
  application,
}: ApplicationSectionProps) {
  const { t } = useTranslation()
  const issuance = application.issuance
  if (!issuance) return null

  return (
    <section aria-labelledby='invoice-issuance-heading'>
      <h3 id='invoice-issuance-heading' className='mb-2 font-medium'>
        {t('Issuance facts')}
      </h3>
      <dl className='grid gap-3 rounded-lg border p-3 sm:grid-cols-2'>
        <div>
          <dt className='text-muted-foreground'>{t('Invoice number')}</dt>
          <dd className='break-all'>{issuance.invoice_number}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Invoice code')}</dt>
          <dd className='break-all'>
            {issuance.invoice_code || t('Not provided')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Invoice date')}</dt>
          <dd>{dayjs.unix(issuance.invoice_date).format('YYYY-MM-DD')}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Document status')}</dt>
          <dd>
            {t(DOCUMENT_STATUS_CONFIG[application.document_status].labelKey)}
          </dd>
        </div>
      </dl>
    </section>
  )
}
