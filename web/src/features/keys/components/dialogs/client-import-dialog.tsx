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
import { MultiSelect } from '@/components/multi-select'
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

import type { ClientImportTarget } from '../../types'

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

interface ImportLink {
  app: AppType
  name: string
  /** Per-application model fields (CC Switch, AiMaMi) */
  models: Record<string, string>
  /** Model ids picked in the multi-select (magpie) */
  modelIds: string[]
  apiKey: string
  /** Currency for the usage query, or null when it is left out */
  usageCurrency: UsageScriptCurrency | null
  /** Whether the key spends the account quota instead of its own */
  unlimitedQuota: boolean
}

function buildCCSwitchURL(link: ImportLink): string {
  const usageScript = link.usageCurrency
    ? buildTokenUsageScript(link.usageCurrency)
    : null
  const serverAddress = getServerAddress()
  const endpoint = link.app === 'codex' ? `${serverAddress}/v1` : serverAddress
  const params = new URLSearchParams()
  params.set('resource', 'provider')
  params.set('app', link.app)
  params.set('name', link.name)
  params.set('endpoint', endpoint)
  params.set('apiKey', link.apiKey)
  for (const [k, v] of Object.entries(link.models)) {
    if (v) params.set(k, v)
  }
  params.set('homepage', serverAddress)
  params.set('enabled', 'true')
  if (usageScript) {
    params.set('usageScript', btoa(usageScript))
  }
  return `ccswitch://v1/import?${params.toString()}`
}

/**
 * Serializes import parameters with spaces as `%20` instead of `+`, so the
 * values decode correctly with both form and percent decoding. A literal `+`
 * is already escaped as `%2B`, so every remaining `+` is a space.
 */
function toPercentEncodedQuery(params: URLSearchParams): string {
  return params.toString().replaceAll('+', '%20')
}

/** Codex++ provider protocol for each application choice. */
const CODEX_PLUS_PLUS_WIRE_APIS: Record<AppType, string> = {
  claude: 'anthropic',
  codex: 'responses',
  gemini: 'chat',
}

/**
 * Codex++ adds the provider to its pool. The link cannot carry models or a
 * usage script; Codex++ detects usage on its own.
 */
function buildCodexPlusPlusURL(link: ImportLink): string {
  const params = new URLSearchParams()
  params.set('name', link.name)
  params.set('baseUrl', `${getServerAddress()}/v1`)
  params.set('apiKey', link.apiKey)
  params.set('wireApi', CODEX_PLUS_PLUS_WIRE_APIS[link.app])
  params.set('target', 'pool')
  return `codexplusplus://v1/import/provider?${toPercentEncodedQuery(params)}`
}

/** AiMaMi only imports Codex providers with a single primary model. */
function buildAiMaMiURL(link: ImportLink): string {
  const params = new URLSearchParams()
  params.set('resource', 'provider')
  params.set('app', 'codex')
  params.set('name', link.name)
  params.set('endpoint', `${getServerAddress()}/v1`)
  params.set('apiKey', link.apiKey)
  params.set('model', link.models.model ?? '')
  return `aimami://v1/import?${toPercentEncodedQuery(params)}`
}

/** Most model ids a magpie import link carries; magpie ignores the rest. */
const MAGPIE_MAX_MODELS = 200

/**
 * magpie takes a plain-http base URL only for this machine or the local
 * network (localhost, *.local, loopback and private addresses).
 */
function isMagpieLocalHost(hostname: string): boolean {
  const host = hostname.replaceAll(/^\[|\]$/g, '').toLowerCase()
  if (host === 'localhost' || host.endsWith('.local')) return true
  if (host.includes(':')) {
    return host === '::1' || /^f[cd][0-9a-f]{0,2}:/.test(host)
  }
  const octets = host.split('.').map(Number)
  if (
    octets.length !== 4 ||
    octets.some((n) => !Number.isInteger(n) || n < 0 || n > 255)
  ) {
    return false
  }
  const [a, b] = octets
  return (
    a === 127 ||
    a === 10 ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 168)
  )
}

/** Whether magpie accepts this site as a provider base URL. */
function supportsMagpieBaseURL(serverAddress: string): boolean {
  try {
    const url = new URL(serverAddress)
    if (url.protocol === 'https:') return true
    return url.protocol === 'http:' && isMagpieLocalHost(url.hostname)
  } catch {
    return false
  }
}

/**
 * magpie keeps a website and asks a balance query only over HTTPS, with no
 * exception for loopback hosts.
 */
function isHTTPSAddress(serverAddress: string): boolean {
  try {
    return new URL(serverAddress).protocol === 'https:'
  } catch {
    return false
  }
}

/** A number as a magpie balance expression reads one: digits and a dot. */
function toMagpieNumber(value: number): string | null {
  const text = String(value)
  return /^\d+(\.\d+)?$/.test(text) ? text : null
}

/**
 * magpie's balance field for `GET /api/usage/token/`: a reply path with
 * arithmetic and a leading currency sign. It mirrors the CC Switch script's
 * conversion: CNY sites multiply by the USD exchange rate, every other
 * display shows USD. A key without its own limit reads the account quota,
 * which the endpoint returns only when the key may query it.
 */
function buildMagpieBalancePath(
  currency: UsageScriptCurrency,
  unlimitedQuota: boolean
): string | null {
  const quotaPerUnit = toMagpieNumber(currency.quotaPerUnit)
  const exchangeRate = toMagpieNumber(currency.exchangeRate)
  if (!quotaPerUnit || !exchangeRate) return null
  const path = unlimitedQuota
    ? 'data.user_quota.total_available'
    : 'data.total_available'
  if (currency.unit === 'CNY') {
    return `¥${path} / ${quotaPerUnit} * ${exchangeRate}`
  }
  return `$${path} / ${quotaPerUnit}`
}

/**
 * magpie takes one provider with all four protocol base URLs, so the link
 * has no application choice. Without models it lists the site's own.
 */
function buildMagpieURL(link: ImportLink): string {
  const serverAddress = getServerAddress().replace(/\/+$/, '')
  const params = new URLSearchParams()
  params.set('name', link.name)
  params.set('key', link.apiKey)
  params.set('chat', `${serverAddress}/v1`)
  params.set('responses', `${serverAddress}/v1`)
  params.set('anthropic', serverAddress)
  params.set('gemini', `${serverAddress}/v1beta`)
  if (link.modelIds.length > 0) {
    params.set('models', link.modelIds.slice(0, MAGPIE_MAX_MODELS).join(','))
  }
  if (isHTTPSAddress(serverAddress)) {
    params.set('website', serverAddress)
  }
  const balancePath =
    link.usageCurrency &&
    buildMagpieBalancePath(link.usageCurrency, link.unlimitedQuota)
  if (balancePath) {
    params.set('balance', `${serverAddress}/api/usage/token/`)
    params.set('balance_path', balancePath)
  }
  return `magpie://import?${toPercentEncodedQuery(params)}`
}

interface UsageQueryConfig {
  /** Whether the client can query usage for this site */
  supports: (serverAddress: string) => boolean
  /** i18n key of the switch description when the query is available */
  description: string
  /** i18n key of the switch description when the site is not supported */
  unsupportedDescription: string
}

interface ImportTargetConfig {
  /** i18n key of the dialog title */
  title: string
  /** i18n key of the submit button */
  submitLabel: string
  /** Applications to choose from; empty when the client takes no choice */
  apps: readonly AppType[]
  /** Provider name used when there is no application choice */
  defaultName?: string
  /** none, one field per application model, or a list of model ids */
  modelSelection: 'none' | 'per-app' | 'multiple'
  usageQuery: UsageQueryConfig | null
  /** Sites the client accepts, checked before the link is built */
  siteCheck?: {
    supports: (serverAddress: string) => boolean
    /** i18n key of the warning shown instead of opening the link */
    unsupportedMessage: string
  }
  buildURL: (link: ImportLink) => string
}

const IMPORT_TARGETS: Record<ClientImportTarget, ImportTargetConfig> = {
  'cc-switch': {
    title: 'Import to CC Switch',
    submitLabel: 'Open CC Switch',
    apps: ['claude', 'codex', 'gemini'],
    modelSelection: 'per-app',
    usageQuery: {
      supports: supportsCCSwitchUsageQuery,
      description: 'Lets CC Switch show the remaining quota after import.',
      unsupportedDescription: 'CC Switch can only query usage for HTTPS sites.',
    },
    buildURL: buildCCSwitchURL,
  },
  'codex-plus-plus': {
    title: 'Import to Codex++',
    submitLabel: 'Open Codex++',
    apps: ['claude', 'codex', 'gemini'],
    modelSelection: 'none',
    usageQuery: null,
    buildURL: buildCodexPlusPlusURL,
  },
  aimami: {
    title: 'Import to AiMaMi',
    submitLabel: 'Open AiMaMi',
    apps: ['codex'],
    modelSelection: 'per-app',
    usageQuery: null,
    buildURL: buildAiMaMiURL,
  },
  magpie: {
    title: 'Import to magpie',
    submitLabel: 'Open magpie',
    apps: [],
    defaultName: 'My Provider',
    modelSelection: 'multiple',
    usageQuery: {
      supports: isHTTPSAddress,
      description: 'Lets magpie show the remaining quota after import.',
      unsupportedDescription: 'magpie can only query balance for HTTPS sites.',
    },
    siteCheck: {
      supports: supportsMagpieBaseURL,
      unsupportedMessage:
        'magpie only imports HTTPS sites, or HTTP sites on this machine or the local network.',
    },
    buildURL: buildMagpieURL,
  },
}

interface Props {
  target: ClientImportTarget
  open: boolean
  onOpenChange: (open: boolean) => void
  tokenKey: string
  /** Whether the key has no limit of its own; used by the magpie balance */
  unlimitedQuota?: boolean
}

export function ClientImportDialog(props: Props) {
  const { t } = useTranslation()
  const targetConfig = IMPORT_TARGETS[props.target]
  // A target without an application choice ignores the app value.
  const defaultApp: AppType = targetConfig.apps[0] ?? 'codex'
  const defaultName =
    targetConfig.defaultName ?? APP_CONFIGS[defaultApp].defaultName
  const [app, setApp] = useState<AppType>(defaultApp)
  const [name, setName] = useState<string>(defaultName)
  const [models, setModels] = useState<Record<string, string>>({})
  const [modelIds, setModelIds] = useState<string[]>([])
  const [usageQueryEnabled, setUsageQueryEnabled] = useState(true)
  const currency = useSystemConfigStore((state) => state.config.currency)
  const usageQuerySupported = useMemo(
    () =>
      props.open &&
      targetConfig.usageQuery !== null &&
      targetConfig.usageQuery.supports(getServerAddress()),
    [props.open, targetConfig.usageQuery]
  )

  const { data: modelsData } = useQuery({
    queryKey: ['user-models-ccswitch'],
    queryFn: async () => requireServerSuccess(await getUserModels()),
    enabled: props.open && targetConfig.modelSelection !== 'none',
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

      setModelIds([])

      setApp(defaultApp)

      setName(defaultName)

      setUsageQueryEnabled(true)
    }
  }, [props.open, defaultApp, defaultName])

  const currentConfig = APP_CONFIGS[app]

  const handleAppChange = (val: string) => {
    const appVal = val as AppType
    setApp(appVal)
    setName(APP_CONFIGS[appVal].defaultName)
    setModels({})
  }

  const handleSubmit = () => {
    if (targetConfig.modelSelection === 'per-app' && !models.model) {
      toast.warning(t('Please select a primary model'))
      return
    }
    if (
      targetConfig.siteCheck &&
      !targetConfig.siteCheck.supports(getServerAddress())
    ) {
      toast.warning(t(targetConfig.siteCheck.unsupportedMessage))
      return
    }
    if (
      targetConfig.modelSelection === 'multiple' &&
      modelIds.length > MAGPIE_MAX_MODELS
    ) {
      toast.warning(
        t('Only the first {{max}} models are included in the link.', {
          max: MAGPIE_MAX_MODELS,
        })
      )
    }
    const key = props.tokenKey.startsWith('sk-')
      ? props.tokenKey
      : `sk-${props.tokenKey}`
    let usageCurrency: UsageScriptCurrency | null = null
    if (usageQuerySupported && usageQueryEnabled) {
      const isCNY = currency?.quotaDisplayType === 'CNY'
      const usdExchangeRate =
        currency?.usdExchangeRate && currency.usdExchangeRate > 0
          ? currency.usdExchangeRate
          : DEFAULT_CURRENCY_CONFIG.usdExchangeRate
      usageCurrency = {
        quotaPerUnit:
          currency?.quotaPerUnit && currency.quotaPerUnit > 0
            ? currency.quotaPerUnit
            : DEFAULT_CURRENCY_CONFIG.quotaPerUnit,
        exchangeRate: isCNY ? usdExchangeRate : 1,
        unit: isCNY ? 'CNY' : 'USD',
      }
    }
    const url = targetConfig.buildURL({
      app,
      name,
      models,
      modelIds,
      apiKey: key,
      usageCurrency,
      unlimitedQuota: props.unlimitedQuota === true,
    })
    window.open(url, '_blank')
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t(targetConfig.title)}
      contentClassName='sm:max-w-md'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSubmit}>{t(targetConfig.submitLabel)}</Button>
        </>
      }
    >
      <div className='space-y-4'>
        {targetConfig.apps.length > 0 && (
          <div className='space-y-2'>
            <Label>{t('Application')}</Label>
            <RadioGroup
              value={app}
              onValueChange={handleAppChange}
              className='flex gap-4'
            >
              {targetConfig.apps.map((key) => (
                <div key={key} className='flex items-center gap-2'>
                  <RadioGroupItem value={key} id={`app-${key}`} />
                  <Label htmlFor={`app-${key}`} className='cursor-pointer'>
                    {APP_CONFIGS[key].label}
                  </Label>
                </div>
              ))}
            </RadioGroup>
          </div>
        )}

        <div className='space-y-2'>
          <Label htmlFor='client-import-name'>{t('Name')}</Label>
          <Input
            id='client-import-name'
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={defaultName}
          />
        </div>

        {targetConfig.modelSelection === 'per-app' &&
          currentConfig.modelFields.map((field) => (
            <div key={field.key} className='space-y-2'>
              <Label
                htmlFor={`client-import-${field.key}`}
                required={field.required}
              >
                {t(field.labelKey)}
              </Label>
              <Combobox
                id={`client-import-${field.key}`}
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

        {targetConfig.modelSelection === 'multiple' && (
          <div className='space-y-2'>
            <Label htmlFor='client-import-models'>{t('Models')}</Label>
            <MultiSelect
              id='client-import-models'
              options={modelOptions}
              selected={modelIds}
              onChange={setModelIds}
              placeholder={t('Leave empty to use all models of the site')}
              emptyText={t('No models found')}
              maxVisibleChips={8}
            />
          </div>
        )}

        {targetConfig.usageQuery && (
          <div className='flex items-start justify-between gap-4'>
            <div className='space-y-1'>
              <Label htmlFor='client-import-usage-query'>
                {t('Also configure usage query')}
              </Label>
              <p
                id='client-import-usage-query-description'
                className='text-muted-foreground text-xs'
              >
                {usageQuerySupported
                  ? t(targetConfig.usageQuery.description)
                  : t(targetConfig.usageQuery.unsupportedDescription)}
              </p>
            </div>
            <Switch
              id='client-import-usage-query'
              aria-describedby='client-import-usage-query-description'
              checked={usageQuerySupported && usageQueryEnabled}
              disabled={!usageQuerySupported}
              onCheckedChange={setUsageQueryEnabled}
            />
          </div>
        )}
      </div>
    </Dialog>
  )
}
