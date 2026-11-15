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
import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Switch } from '@/components/ui/switch'
import { getUserModels } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

const APP_CONFIGS = {
  claude: {
    label: 'Claude',
    defaultName: 'My Claude',
    modelFields: [
      { key: 'model', labelKey: 'Primary Model', required: true },
      { key: 'haikuModel', labelKey: 'Haiku Model', required: false },
      { key: 'sonnetModel', labelKey: 'Sonnet Model', required: false },
      { key: 'opusModel', labelKey: 'Opus Model', required: false },
    ],
  },
  codex: {
    label: 'Codex',
    defaultName: 'My Codex',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
  gemini: {
    label: 'Gemini',
    defaultName: 'My Gemini',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
} as const

type AppType = keyof typeof APP_CONFIGS

function getServerAddress(): string {
  try {
    const raw = localStorage.getItem('status')
    if (raw) {
      const status = JSON.parse(raw)
      if (status.server_address) return status.server_address
    }
  } catch {
    /* empty */
  }
  return window.location.origin
}

/**
 * CC Switch only runs usage queries against HTTPS endpoints, with an exception
 * for loopback hosts during local development.
 */
function supportsCCSwitchUsageQuery(serverAddress: string): boolean {
  try {
    const url = new URL(serverAddress)
    if (url.protocol === 'https:') return true
    return ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  } catch {
    return false
  }
}

interface UsageScriptCurrency {
  /** Internal quota units that equal one USD */
  quotaPerUnit: number
  /** Multiplier applied to USD amounts */
  exchangeRate: number
  unit: 'USD' | 'CNY'
}

/**
 * Builds the CC Switch usage query script for this site's
 * `GET /api/usage/token/` endpoint. CC Switch substitutes `{{baseUrl}}` with
 * the provider endpoint and `{{apiKey}}` with the provider key, then runs the
 * script in a QuickJS sandbox, so it stays ASCII-only and avoids modern syntax.
 * Quota values are converted with the site's currency settings.
 */
function buildTokenUsageScript(currency: UsageScriptCurrency): string {
  return `({
  request: {
    url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/v1$/, "") + "/api/usage/token/",
    method: "GET",
    headers: {
      "Authorization": "Bearer {{apiKey}}",
      "User-Agent": "cc-switch/1.0"
    }
  },
  extractor: function (response) {
    var quotaPerUnit = ${currency.quotaPerUnit};
    var exchangeRate = ${currency.exchangeRate};
    function toAmount(quota) {
      return (Number(quota) || 0) / quotaPerUnit * exchangeRate;
    }
    if (!response || response.code !== true || !response.data) {
      return {
        isValid: false,
        invalidMessage: response && typeof response.message === "string" && response.message
          ? response.message
          : "Failed to query API key usage"
      };
    }
    var data = response.data;
    if (data.expires_at > 0 && data.expires_at * 1000 < Date.now()) {
      return {
        isValid: false,
        invalidMessage: "API key has expired"
      };
    }
    var result = { isValid: true, unit: "${currency.unit}" };
    if (typeof data.name === "string" && data.name) {
      result.planName = data.name;
    }
    if (!data.unlimited_quota) {
      result.remaining = toAmount(data.total_available);
      result.used = toAmount(data.total_used);
      result.total = toAmount(data.total_granted);
      return result;
    }
    if (data.user_quota) {
      var userRemaining = Number(data.user_quota.total_available) || 0;
      var userUsed = Number(data.user_quota.total_used) || 0;
      result.remaining = toAmount(userRemaining);
      result.used = toAmount(userUsed);
      result.total = toAmount(userRemaining + userUsed);
      return result;
    }
    result.used = toAmount(data.total_used);
    return result;
  }
})`
}

function buildCCSwitchURL(
  app: string,
  name: string,
  models: Record<string, string>,
  apiKey: string,
  usageScript: string | null
): string {
  const serverAddress = getServerAddress()
  const endpoint = app === 'codex' ? `${serverAddress}/v1` : serverAddress
  const params = new URLSearchParams()
  params.set('resource', 'provider')
  params.set('app', app)
  params.set('name', name)
  params.set('endpoint', endpoint)
  params.set('apiKey', apiKey)
  for (const [k, v] of Object.entries(models)) {
    if (v) params.set(k, v)
  }
  params.set('homepage', serverAddress)
  params.set('enabled', 'true')
  if (usageScript) {
    params.set('usageScript', btoa(usageScript))
  }
  return `ccswitch://v1/import?${params.toString()}`
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  tokenKey: string
}

export function CCSwitchDialog(props: Props) {
  const { t } = useTranslation()
  const [app, setApp] = useState<AppType>('claude')
  const [name, setName] = useState<string>(APP_CONFIGS.claude.defaultName)
  const [models, setModels] = useState<Record<string, string>>({})
  const [usageQueryEnabled, setUsageQueryEnabled] = useState(true)
  const currency = useSystemConfigStore((state) => state.config.currency)
  const usageQuerySupported = useMemo(
    () => props.open && supportsCCSwitchUsageQuery(getServerAddress()),
    [props.open]
  )

  const { data: modelsData } = useQuery({
    queryKey: ['user-models-ccswitch'],
    queryFn: async () => requireServerSuccess(await getUserModels()),
    enabled: props.open,
    staleTime: 5 * 60 * 1000,
  })

  const modelOptions = useMemo(() => {
    const items = modelsData?.data ?? []
    return items.map((m) => ({ value: m, label: m }))
  }, [modelsData?.data])

  useEffect(() => {
    if (props.open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setModels({})

      setApp('claude')

      setName(APP_CONFIGS.claude.defaultName)

      setUsageQueryEnabled(true)
    }
  }, [props.open])

  const currentConfig = APP_CONFIGS[app]

  const handleAppChange = (val: string) => {
    const appVal = val as AppType
    setApp(appVal)
    setName(APP_CONFIGS[appVal].defaultName)
    setModels({})
  }

  const handleSubmit = () => {
    if (!models.model) {
      toast.warning(t('Please select a primary model'))
      return
    }
    const key = props.tokenKey.startsWith('sk-')
      ? props.tokenKey
      : `sk-${props.tokenKey}`
    let usageScript: string | null = null
    if (usageQuerySupported && usageQueryEnabled) {
      const isCNY = currency?.quotaDisplayType === 'CNY'
      const usdExchangeRate =
        currency?.usdExchangeRate && currency.usdExchangeRate > 0
          ? currency.usdExchangeRate
          : DEFAULT_CURRENCY_CONFIG.usdExchangeRate
      usageScript = buildTokenUsageScript({
        quotaPerUnit:
          currency?.quotaPerUnit && currency.quotaPerUnit > 0
            ? currency.quotaPerUnit
            : DEFAULT_CURRENCY_CONFIG.quotaPerUnit,
        exchangeRate: isCNY ? usdExchangeRate : 1,
        unit: isCNY ? 'CNY' : 'USD',
      })
    }
    const url = buildCCSwitchURL(app, name, models, key, usageScript)
    window.open(url, '_blank')
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Import to CC Switch')}
      contentClassName='sm:max-w-md'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSubmit}>{t('Open CC Switch')}</Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='space-y-2'>
          <Label>{t('Application')}</Label>
          <RadioGroup
            value={app}
            onValueChange={handleAppChange}
            className='flex gap-4'
          >
            {(
              Object.entries(APP_CONFIGS) as [
                AppType,
                (typeof APP_CONFIGS)[AppType],
              ][]
            ).map(([key, cfg]) => (
              <div key={key} className='flex items-center gap-2'>
                <RadioGroupItem value={key} id={`app-${key}`} />
                <Label htmlFor={`app-${key}`} className='cursor-pointer'>
                  {cfg.label}
                </Label>
              </div>
            ))}
          </RadioGroup>
        </div>

        <div className='space-y-2'>
          <Label htmlFor='cc-switch-name'>{t('Name')}</Label>
          <Input
            id='cc-switch-name'
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={currentConfig.defaultName}
          />
        </div>

        {currentConfig.modelFields.map((field) => (
          <div key={field.key} className='space-y-2'>
            <Label htmlFor={`cc-switch-${field.key}`} required={field.required}>
              {t(field.labelKey)}
            </Label>
            <Combobox
              id={`cc-switch-${field.key}`}
              aria-label={t(field.labelKey)}
              options={modelOptions}
              value={models[field.key] || ''}
              onValueChange={(v) =>
                setModels((prev) => ({ ...prev, [field.key]: v ?? '' }))
              }
              placeholder={t('Select or enter model name')}
              emptyText={t('No models found')}
            />
          </div>
        ))}

        <div className='flex items-start justify-between gap-4'>
          <div className='space-y-1'>
            <Label htmlFor='cc-switch-usage-query'>
              {t('Also configure usage query')}
            </Label>
            <p
              id='cc-switch-usage-query-description'
              className='text-muted-foreground text-xs'
            >
              {usageQuerySupported
                ? t('Lets CC Switch show the remaining quota after import.')
                : t('CC Switch can only query usage for HTTPS sites.')}
            </p>
          </div>
          <Switch
            id='cc-switch-usage-query'
            aria-describedby='cc-switch-usage-query-description'
            checked={usageQuerySupported && usageQueryEnabled}
            disabled={!usageQuerySupported}
            onCheckedChange={setUsageQueryEnabled}
          />
        </div>
      </div>
    </Dialog>
  )
}
