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
import { useTranslation } from 'react-i18next'

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
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

import type { InvoiceConfig, InvoiceProfile } from '../types'
import { isInvoiceProfileEnabled } from '../user-workspace'

interface ProfileListProps {
  profiles: InvoiceProfile[]
  config: InvoiceConfig | undefined
  deletingProfileId: number | undefined
  onAdd: () => void
  onEdit: (profile: InvoiceProfile) => void
  onDelete: (profile: InvoiceProfile) => void
}

/** Renders versioned invoice identities and their available actions. */
export function ProfileList(props: ProfileListProps) {
  const { t } = useTranslation()

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
          disabled={
            !props.config ||
            (!props.config.personal_enabled && !props.config.company_enabled)
          }
          onClick={props.onAdd}
        >
          <HugeiconsIcon
            icon={AddInvoiceIcon}
            strokeWidth={2}
            data-icon='inline-start'
          />
          {t('Add invoice profile')}
        </Button>
      </div>

      {props.profiles.length === 0 ? (
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
          {props.profiles.map((profile) => (
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
                  {props.config &&
                  !isInvoiceProfileEnabled(props.config, profile.type) ? (
                    <Badge variant='secondary'>
                      {t('Disabled for new applications')}
                    </Badge>
                  ) : null}
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
                <Button variant='outline' onClick={() => props.onEdit(profile)}>
                  <HugeiconsIcon
                    icon={PencilEdit02Icon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                  {t('Edit')}
                </Button>
                <Button
                  variant='destructive'
                  disabled={props.deletingProfileId === profile.id}
                  onClick={() => props.onDelete(profile)}
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
    </>
  )
}
