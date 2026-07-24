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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'

import { buildInvoiceDocumentFormData } from '../contract'
import type { InvoiceApplicationDetail } from '../types'

const CURRENCY_ITEMS = [{ label: 'CNY', value: 'CNY' }]

interface DocumentUploadProps {
  application: InvoiceApplicationDetail
  pending: boolean
  onUpload: (document: FormData) => void
}

/** Renders strict initial-issue and locked replacement PDF forms. */
export function DocumentUpload(props: DocumentUploadProps) {
  const { t } = useTranslation()
  const replacement = props.application.status === 'issued'
  const issuance = props.application.issuance
  const [file, setFile] = useState<File | null>(null)
  const [invoiceNumber, setInvoiceNumber] = useState(
    issuance?.invoice_number ?? ''
  )
  const [invoiceCode, setInvoiceCode] = useState(issuance?.invoice_code ?? '')
  const [invoiceDate, setInvoiceDate] = useState(
    issuance ? dayjs.unix(issuance.invoice_date).format('YYYY-MM-DD') : ''
  )
  const [faceAmountMinor, setFaceAmountMinor] = useState(
    issuance
      ? String(issuance.face_amount_minor)
      : String(props.application.amount_minor)
  )
  const [attested, setAttested] = useState(false)
  const [errorKey, setErrorKey] = useState<string | null>(null)

  function submit(event: React.FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    const invoiceDateSeconds = Math.floor(
      new Date(`${invoiceDate}T00:00:00`).getTime() / 1000
    )
    const result = buildInvoiceDocumentFormData(props.application, {
      file,
      invoice_number: invoiceNumber,
      invoice_code: invoiceCode,
      invoice_date: invoiceDateSeconds,
      face_amount_minor: Number(faceAmountMinor),
      currency: 'CNY',
      pdf_facts_attested: attested,
    })
    if (!result.ok) {
      setErrorKey(result.errorKey)
      return
    }
    setErrorKey(null)
    props.onUpload(result.data)
  }

  return (
    <form onSubmit={submit} aria-busy={props.pending}>
      <FieldSet disabled={props.pending}>
        <FieldLegend variant='label'>
          {replacement ? t('Replace invoice PDF') : t('Issue invoice PDF')}
        </FieldLegend>
        <FieldDescription>
          {replacement
            ? t('Replacement keeps the original issuance facts locked.')
            : t(
                'Upload one PDF and attest that its issuance facts are correct.'
              )}
        </FieldDescription>
        <FieldGroup>
          <Field
            data-invalid={errorKey ? true : undefined}
            data-disabled={props.pending || undefined}
          >
            <FieldLabel htmlFor='invoice-pdf-file'>
              {t('PDF document')}
            </FieldLabel>
            <Input
              id='invoice-pdf-file'
              type='file'
              accept='application/pdf,.pdf'
              disabled={props.pending}
              aria-invalid={errorKey ? true : undefined}
              aria-describedby={
                errorKey ? 'invoice-upload-error' : 'invoice-pdf-help'
              }
              onChange={(event) => setFile(event.target.files?.[0] ?? null)}
            />
            <FieldDescription id='invoice-pdf-help'>
              {t('PDF only, maximum 10 MiB.')}
            </FieldDescription>
          </Field>

          <div className='grid gap-4 sm:grid-cols-2'>
            <Field data-disabled={replacement || props.pending || undefined}>
              <FieldLabel htmlFor='invoice-number'>
                {t('Invoice number')}
              </FieldLabel>
              <Input
                id='invoice-number'
                value={invoiceNumber}
                disabled={replacement || props.pending}
                onChange={(event) => setInvoiceNumber(event.target.value)}
              />
            </Field>
            <Field data-disabled={replacement || props.pending || undefined}>
              <FieldLabel htmlFor='invoice-code'>
                {t('Invoice code')}
              </FieldLabel>
              <Input
                id='invoice-code'
                value={invoiceCode}
                disabled={replacement || props.pending}
                onChange={(event) => setInvoiceCode(event.target.value)}
              />
            </Field>
            <Field data-disabled={replacement || props.pending || undefined}>
              <FieldLabel htmlFor='invoice-date'>
                {t('Invoice date')}
              </FieldLabel>
              <Input
                id='invoice-date'
                type='date'
                value={invoiceDate}
                disabled={replacement || props.pending}
                onChange={(event) => setInvoiceDate(event.target.value)}
              />
            </Field>
            <Field data-disabled={replacement || props.pending || undefined}>
              <FieldLabel htmlFor='invoice-face-amount'>
                {t('Face amount in minor units')}
              </FieldLabel>
              <Input
                id='invoice-face-amount'
                type='number'
                min={1}
                step={1}
                value={faceAmountMinor}
                disabled={replacement || props.pending}
                onChange={(event) => setFaceAmountMinor(event.target.value)}
              />
            </Field>
            <Field data-disabled>
              <FieldLabel htmlFor='invoice-currency'>
                {t('Currency')}
              </FieldLabel>
              <Select items={CURRENCY_ITEMS} value='CNY' disabled>
                <SelectTrigger id='invoice-currency' className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='CNY'>CNY</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          </div>

          <Field
            orientation='horizontal'
            data-invalid={errorKey ? true : undefined}
            data-disabled={props.pending || undefined}
          >
            <Checkbox
              id='invoice-pdf-attestation'
              checked={attested}
              disabled={props.pending}
              aria-invalid={errorKey ? true : undefined}
              aria-describedby={errorKey ? 'invoice-upload-error' : undefined}
              onCheckedChange={(checked) => setAttested(checked === true)}
            />
            <FieldLabel
              htmlFor='invoice-pdf-attestation'
              className='font-normal'
            >
              {t('I confirm the PDF matches these issuance facts.')}
            </FieldLabel>
          </Field>

          {errorKey ? (
            <FieldDescription id='invoice-upload-error' role='alert'>
              {t(errorKey)}
            </FieldDescription>
          ) : null}

          <Button type='submit' disabled={props.pending}>
            {props.pending ? <Spinner data-icon='inline-start' /> : null}
            {replacement ? t('Replace PDF') : t('Upload PDF')}
          </Button>
        </FieldGroup>
      </FieldSet>
    </form>
  )
}
