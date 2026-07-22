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

import { Badge } from '@/components/ui/badge'

import {
  APPLICATION_STATUS_CONFIG,
  DOCUMENT_STATUS_CONFIG,
  FEE_STATUS_CONFIG,
  PAYMENT_REVIEW_STATUS_CONFIG,
} from '../contract'
import type { InvoiceApplicationSummary } from '../types'

interface InvoiceStatusBadgesProps {
  application: InvoiceApplicationSummary
}

export function InvoiceStatusBadges(props: InvoiceStatusBadgesProps) {
  const { t } = useTranslation()
  const applicationStatus = APPLICATION_STATUS_CONFIG[props.application.status]
  const feeStatus = FEE_STATUS_CONFIG[props.application.fee_status]
  const paymentReviewStatus =
    PAYMENT_REVIEW_STATUS_CONFIG[props.application.payment_review_status]
  const documentStatus =
    DOCUMENT_STATUS_CONFIG[props.application.document_status]

  return (
    <div className='flex flex-wrap gap-1.5'>
      <Badge variant={applicationStatus.variant}>
        {t(applicationStatus.labelKey)}
      </Badge>
      <Badge variant={feeStatus.variant}>{t(feeStatus.labelKey)}</Badge>
      <Badge variant={paymentReviewStatus.variant}>
        {t(paymentReviewStatus.labelKey)}
      </Badge>
      <Badge variant={documentStatus.variant}>
        {t(documentStatus.labelKey)}
      </Badge>
    </div>
  )
}
