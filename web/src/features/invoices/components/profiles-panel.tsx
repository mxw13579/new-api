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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'

import { InvoiceApiError } from '../api'
import { getInvoiceErrorMessageKey } from '../contract'
import { invoiceQueryKeys } from '../queries'
import type { InvoiceApi, InvoiceConfig, InvoiceProfile } from '../types'
import { ProfileEditor, type ProfileDraft } from './profile-editor'
import { ProfileList } from './profile-list'

interface ProfilesPanelProps {
  invoiceApi: InvoiceApi
  profiles: InvoiceProfile[] | undefined
  loading: boolean
  error: boolean
  retry: () => void
  config: InvoiceConfig | undefined
}

const EMPTY_PROFILE_DRAFT: ProfileDraft = {
  id: null,
  type: 'personal',
  title: '',
  taxNumber: '',
  identityCardNumber: '',
  isDefault: false,
  version: 0,
}

/**
 * Renders version-aware invoice profile management.
 *
 * @param props - Current profiles, loading state, and API dependency.
 * @returns Profile cards and the create or edit dialog.
 */
export function ProfilesPanel(props: ProfilesPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<ProfileDraft>(EMPTY_PROFILE_DRAFT)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<InvoiceProfile | null>(null)

  const saveMutation = useMutation({
    mutationFn: () => {
      if (draft.id === null) {
        return props.invoiceApi.createProfile({
          type: draft.type,
          title: draft.title.trim(),
          tax_number: draft.type === 'company' ? draft.taxNumber.trim() : '',
          identity_card_number: draft.type === 'personal' ? draft.identityCardNumber.trim() : '',
          is_default: draft.isDefault,
        })
      }
      return props.invoiceApi.updateProfile({
        id: draft.id,
        expected_version: draft.version,
        title: draft.title.trim(),
        tax_number: draft.type === 'company' ? draft.taxNumber.trim() : '',
        identity_card_number: draft.type === 'personal' ? draft.identityCardNumber.trim() : '',
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
      setDeleteTarget(null)
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

  if (props.error) {
    return (
      <Alert variant='destructive'>
        <AlertTitle>{t('Invoice profiles failed to load')}</AlertTitle>
        <AlertDescription>
          <Button variant='outline' size='sm' onClick={props.retry}>
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  }

  const profiles = props.profiles || []
  return (
    <>
      <ProfileList
        profiles={profiles}
        config={props.config}
        deletingProfileId={
          deleteMutation.isPending ? deleteMutation.variables?.id : undefined
        }
        onAdd={() => {
          setDraft({
            ...EMPTY_PROFILE_DRAFT,
            type:
              props.config && !props.config.personal_enabled
                ? 'company'
                : 'personal',
          })
          setDialogOpen(true)
        }}
        onEdit={(profile) => {
          setDraft({
            id: profile.id,
            type: profile.type,
            title: profile.title,
            taxNumber: profile.tax_number,
            identityCardNumber: profile.identity_card_number,
            isDefault: profile.is_default,
            version: profile.version,
          })
          setDialogOpen(true)
        }}
        onDelete={setDeleteTarget}
      />

      <ProfileEditor
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        draft={draft}
        setDraft={setDraft}
        config={props.config}
        saving={saveMutation.isPending}
        onSave={() => saveMutation.mutate()}
      />

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) setDeleteTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete invoice profile?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This profile cannot be restored. Existing invoice applications remain available.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={() => {
                if (deleteTarget) deleteMutation.mutate(deleteTarget)
              }}
            >
              {deleteMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : null}
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
