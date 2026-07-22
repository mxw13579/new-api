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
import {
  AddInvoiceIcon,
  Delete02Icon,
  PencilEdit02Icon,
  UserAccountIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'

import { InvoiceApiError } from '../api'
import { getInvoiceErrorMessageKey } from '../contract'
import { invoiceQueryKeys } from '../queries'
import type { InvoiceApi, InvoiceProfile, InvoiceType } from '../types'

interface ProfilesPanelProps {
  invoiceApi: InvoiceApi
  profiles: InvoiceProfile[] | undefined
  loading: boolean
}

interface ProfileDraft {
  id: number | null
  type: InvoiceType
  title: string
  taxNumber: string
  isDefault: boolean
  version: number
}

const EMPTY_PROFILE_DRAFT: ProfileDraft = {
  id: null,
  type: 'personal',
  title: '',
  taxNumber: '',
  isDefault: false,
  version: 0,
}

export function ProfilesPanel(props: ProfilesPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<ProfileDraft>(EMPTY_PROFILE_DRAFT)
  const [dialogOpen, setDialogOpen] = useState(false)

  const saveMutation = useMutation({
    mutationFn: () => {
      if (draft.id === null) {
        return props.invoiceApi.createProfile({
          type: draft.type,
          title: draft.title.trim(),
          tax_number: draft.type === 'company' ? draft.taxNumber.trim() : '',
          is_default: draft.isDefault,
        })
      }
      return props.invoiceApi.updateProfile({
        id: draft.id,
        expected_version: draft.version,
        title: draft.title.trim(),
        tax_number: draft.type === 'company' ? draft.taxNumber.trim() : '',
        is_default: draft.isDefault,
      })
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: invoiceQueryKeys.profiles(),
      })
      setDialogOpen(false)
      toast.success(t('Invoice profile saved'))
    },
    onError: async (error) => {
      if (error instanceof InvoiceApiError) {
        if (error.code === 'INVOICE_STATE_CONFLICT') {
          await queryClient.invalidateQueries({
            queryKey: invoiceQueryKeys.profiles(),
          })
          setDialogOpen(false)
        }
        toast.error(t(getInvoiceErrorMessageKey(error.code)))
        return
      }
      toast.error(t('Invoice error: service unavailable'))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (profile: InvoiceProfile) =>
      props.invoiceApi.deleteProfile({
        id: profile.id,
        expected_version: profile.version,
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: invoiceQueryKeys.profiles(),
      })
      toast.success(t('Invoice profile deleted'))
    },
    onError: async (error) => {
      if (error instanceof InvoiceApiError) {
        await queryClient.invalidateQueries({
          queryKey: invoiceQueryKeys.profiles(),
        })
        toast.error(t(getInvoiceErrorMessageKey(error.code)))
        return
      }
      toast.error(t('Invoice error: service unavailable'))
    },
  })

  if (props.loading) {
    return (
      <div className='grid gap-4 md:grid-cols-2'>
        <Skeleton className='h-44' />
        <Skeleton className='h-44' />
      </div>
    )
  }

  const profiles = props.profiles || []
  return (
    <>
      <div className='mb-4 flex items-center justify-between gap-3'>
        <div>
          <h2 className='text-lg font-semibold'>{t('Invoice profiles')}</h2>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Manage personal and company invoice identities with version checks.'
            )}
          </p>
        </div>
        <Button
          onClick={() => {
            setDraft(EMPTY_PROFILE_DRAFT)
            setDialogOpen(true)
          }}
        >
          <HugeiconsIcon
            icon={AddInvoiceIcon}
            strokeWidth={2}
            data-icon='inline-start'
          />
          {t('Add invoice profile')}
        </Button>
      </div>

      {profiles.length === 0 ? (
        <Empty className='border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <HugeiconsIcon icon={UserAccountIcon} strokeWidth={2} />
            </EmptyMedia>
            <EmptyTitle>{t('No invoice profiles')}</EmptyTitle>
            <EmptyDescription>
              {t('Create a profile before applying for an invoice.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='grid gap-4 md:grid-cols-2'>
          {profiles.map((profile) => (
            <Card key={profile.id}>
              <CardHeader>
                <CardTitle>{profile.title}</CardTitle>
                <CardDescription>
                  {profile.type === 'personal'
                    ? t('Personal invoice profile')
                    : t('Company invoice profile')}
                </CardDescription>
                <CardAction className='flex gap-1'>
                  {profile.is_default ? <Badge>{t('Default')}</Badge> : null}
                  <Badge variant='outline'>v{profile.version}</Badge>
                </CardAction>
              </CardHeader>
              <CardContent>
                {profile.type === 'company' ? (
                  <p className='text-muted-foreground text-sm'>
                    {t('Tax number')}: {profile.tax_number}
                  </p>
                ) : (
                  <p className='text-muted-foreground text-sm'>
                    {t('Personal profiles do not include a tax number.')}
                  </p>
                )}
              </CardContent>
              <CardFooter className='justify-end gap-2'>
                <Button
                  variant='outline'
                  onClick={() => {
                    setDraft({
                      id: profile.id,
                      type: profile.type,
                      title: profile.title,
                      taxNumber: profile.tax_number,
                      isDefault: profile.is_default,
                      version: profile.version,
                    })
                    setDialogOpen(true)
                  }}
                >
                  <HugeiconsIcon
                    icon={PencilEdit02Icon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                  {t('Edit')}
                </Button>
                <Button
                  variant='destructive'
                  disabled={deleteMutation.isPending}
                  onClick={() => deleteMutation.mutate(profile)}
                >
                  <HugeiconsIcon
                    icon={Delete02Icon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                  {t('Delete')}
                </Button>
              </CardFooter>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {draft.id === null
                ? t('Add invoice profile')
                : t('Edit invoice profile')}
            </DialogTitle>
            <DialogDescription>
              {draft.id === null
                ? t('Create a personal or company invoice identity.')
                : t('Saving requires the current profile version.')}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field data-disabled={draft.id !== null || undefined}>
              <FieldLabel htmlFor='invoice-profile-type'>
                {t('Profile type')}
              </FieldLabel>
              <NativeSelect
                id='invoice-profile-type'
                className='w-full'
                value={draft.type}
                disabled={draft.id !== null}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    type: event.target.value as InvoiceType,
                    taxNumber:
                      event.target.value === 'personal'
                        ? ''
                        : current.taxNumber,
                  }))
                }
              >
                <NativeSelectOption value='personal'>
                  {t('Personal')}
                </NativeSelectOption>
                <NativeSelectOption value='company'>
                  {t('Company')}
                </NativeSelectOption>
              </NativeSelect>
            </Field>
            <Field>
              <FieldLabel htmlFor='invoice-profile-title'>
                {draft.type === 'personal' ? t('Full name') : t('Company name')}
              </FieldLabel>
              <Input
                id='invoice-profile-title'
                value={draft.title}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    title: event.target.value,
                  }))
                }
              />
            </Field>
            {draft.type === 'company' ? (
              <Field>
                <FieldLabel htmlFor='invoice-profile-tax-number'>
                  {t('Tax number')}
                </FieldLabel>
                <Input
                  id='invoice-profile-tax-number'
                  value={draft.taxNumber}
                  onChange={(event) =>
                    setDraft((current) => ({
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
                checked={draft.isDefault}
                onCheckedChange={(checked) =>
                  setDraft((current) => ({ ...current, isDefault: checked }))
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
            <Button variant='outline' onClick={() => setDialogOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={
                saveMutation.isPending ||
                !draft.title.trim() ||
                (draft.type === 'company' && !draft.taxNumber.trim())
              }
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : null}
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
