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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { FieldSet, FieldLegend } from '@/components/ui/field'
import { Skeleton } from '@/components/ui/skeleton'

import type { EligibleInvoiceOrder, InvoicePage } from '../types'
import { invoicePageCount, type InvoiceOrderSelection } from '../user-workspace'

interface EligibleOrderSelectionProps {
  ordersPage: InvoicePage<EligibleInvoiceOrder> | undefined
  loading: boolean
  error: boolean
  retry: () => void
  selection: InvoiceOrderSelection
  onSelectionChange: (order: EligibleInvoiceOrder, selected: boolean) => void
  onPageChange: (page: number) => void
  formatAmount: (minor: number) => string
}

/** Renders paginated complete-order selection for an invoice draft. */
export function EligibleOrderSelection(props: EligibleOrderSelectionProps) {
  const { t } = useTranslation()
  const orders = props.ordersPage?.items || []
  const ordersPage = props.ordersPage
  let orderContent: ReactNode
  if (props.loading) {
    orderContent = <Skeleton className='h-28 w-full' />
  } else if (props.error) {
    orderContent = (
      <Alert variant='destructive'>
        <AlertTitle>{t('Eligible orders failed to load')}</AlertTitle>
        <AlertDescription>
          <Button variant='outline' size='sm' onClick={props.retry}>
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  } else if (orders.length === 0) {
    orderContent = (
      <Empty className='border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={InvoiceIcon} strokeWidth={2} />
          </EmptyMedia>
          <EmptyTitle>{t('No eligible orders')}</EmptyTitle>
          <EmptyDescription>
            {t('Eligible paid orders will appear here.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    orderContent = (
      <div className='overflow-x-auto rounded-lg border'>
        <table className='w-full min-w-[640px] text-left text-sm'>
          <thead className='bg-muted/50'>
            <tr>
              <th className='p-3'>{t('Select')}</th>
              <th className='p-3'>{t('Order number')}</th>
              <th className='p-3'>{t('Product')}</th>
              <th className='p-3'>{t('Paid at')}</th>
              <th className='p-3 text-right'>{t('Paid amount')}</th>
            </tr>
          </thead>
          <tbody>
            {orders.map((order) => {
              const checkboxId = `invoice-order-${order.topup_id}`
              return (
                <tr key={order.topup_id} className='border-t'>
                  <td className='p-3'>
                    <Checkbox
                      id={checkboxId}
                      checked={props.selection.has(order.topup_id)}
                      onCheckedChange={(checked) =>
                        props.onSelectionChange(order, checked)
                      }
                      aria-label={t('Select order {{order}}', {
                        order: order.order_no,
                      })}
                    />
                  </td>
                  <td className='p-3 font-medium'>
                    <label htmlFor={checkboxId}>{order.order_no}</label>
                  </td>
                  <td className='p-3'>{order.product_description}</td>
                  <td className='p-3'>
                    {new Date(order.paid_at * 1000).toLocaleString()}
                  </td>
                  <td className='p-3 text-right'>
                    {props.formatAmount(order.paid_amount_minor)}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    )
  }

  return (
    <FieldSet>
      <FieldLegend>{t('Eligible paid orders')}</FieldLegend>
      {orderContent}
      {ordersPage ? (
        <div className='flex items-center justify-end gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={ordersPage.page <= 1}
            onClick={() => props.onPageChange(ordersPage.page - 1)}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground text-sm'>
            {t('Page {{page}} of {{pages}}', {
              page: ordersPage.page,
              pages: invoicePageCount(ordersPage.total, ordersPage.page_size),
            })}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={
              ordersPage.page >=
              invoicePageCount(ordersPage.total, ordersPage.page_size)
            }
            onClick={() => props.onPageChange(ordersPage.page + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      ) : null}
    </FieldSet>
  )
}
