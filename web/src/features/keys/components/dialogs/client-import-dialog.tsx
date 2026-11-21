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

/** Provider name magpie gets when the name field is left blank. */
const MAGPIE_DEFAULT_NAME = 'My Provider'

/** Provider id used when neither the name nor the host gives a usable one. */
const MAGPIE_FALLBACK_ID = 'relay'

/**
 * magpie takes a plain-http base URL only for this machine or the local
 * network: localhost, *.local, loopback and private addresses. The hostname
 * is the one `URL` normalized, so IPv4 is dotted and IPv6 compressed.
 */
function isMagpieLocalHost(hostname: string): boolean {
  if (hostname === 'localhost' || hostname.endsWith('.local')) return true
  if (hostname.startsWith('[')) {
    const ipv6 = hostname.slice(1, -1)
    // fc00::/7 is a first hextet of fcXX or fdXX, written with four digits
    return ipv6 === '::1' || /^f[cd][0-9a-f]{2}:/.test(ipv6)
  }
  const octets = hostname.split('.').map(Number)
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

/**
 * The site as a magpie link carries it, normalized by `URL`, or null when
 * magpie would refuse it as a base URL: not https, plain http off the local
 * network, or userinfo, a query or a fragment in it.
 */
function toMagpieSite(serverAddress: string): URL | null {
  let url: URL
  try {
    url = new URL(serverAddress)
  } catch {
    return null
  }
  if (url.username || url.password || url.search || url.hash) return null
  if (url.protocol === 'https:') return url
  if (url.protocol === 'http:' && isMagpieLocalHost(url.hostname)) return url
  return null
}

function supportsMagpieBaseURL(serverAddress: string): boolean {
  return toMagpieSite(serverAddress) !== null
}

/**
 * magpie keeps a website and asks a balance query only over HTTPS, with no
 * exception for loopback hosts.
 */
function supportsMagpieBalance(serverAddress: string): boolean {
  return toMagpieSite(serverAddress)?.protocol === 'https:'
}

/** magpie's provider id rule (Slug): lowercase, other runs become "-". */
function toMagpieSlug(text: string): string {
  return text
    .toLowerCase()
    .replaceAll(/[^a-z0-9]+/g, '-')
    .replaceAll(/^-|-$/g, '')
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
 * which the endpoint returns only when the key allows that query.
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
 * has no application choice. Without models it lists the site's own. magpie
 * derives the provider id from the name and refuses an empty id or
 * "magpie", so such a name gets an id from the site host.
 */
function buildMagpieURL(link: ImportLink): string {
  const site = toMagpieSite(getServerAddress())
  if (!site) throw new Error('magpie does not accept this site')
  const siteAddress = site.origin + site.pathname.replace(/\/+$/, '')
  const name = link.name.trim() ? link.name : MAGPIE_DEFAULT_NAME
  const params = new URLSearchParams()
  params.set('name', name)
  const nameSlug = toMagpieSlug(name)
  if (!nameSlug || nameSlug === 'magpie') {
    const hostSlug = toMagpieSlug(site.hostname)
    params.set(
      'id',
      hostSlug && hostSlug !== 'magpie' ? hostSlug : MAGPIE_FALLBACK_ID
    )
  }
  params.set('key', link.apiKey)
  params.set('chat', `${siteAddress}/v1`)
  params.set('responses', `${siteAddress}/v1`)
  params.set('anthropic', siteAddress)
  params.set('gemini', `${siteAddress}/v1beta`)
  if (link.modelIds.length > 0) {
    params.set('models', link.modelIds.slice(0, MAGPIE_MAX_MODELS).join(','))
  }
  if (site.protocol === 'https:') {
    params.set('website', siteAddress)
  }
  const balancePath =
    link.usageCurrency &&
    buildMagpieBalancePath(link.usageCurrency, link.unlimitedQuota)
  if (balancePath) {
    params.set('balance', `${siteAddress}/api/usage/token/`)
    params.set('balance_path', balancePath)
  }
  return `magpie://import?${toPercentEncodedQuery(params)}`
}

/** What the dialog knows about the key's quota. */
interface KeyQuota {
  unlimitedQuota: boolean
  allowUserQuotaQuery: boolean
}

interface UsageQueryConfig {
  /** Whether the client can query usage for this site */
  supports: (serverAddress: string) => boolean
  /** i18n key of the switch description when the query is available */
  description: string
  /** i18n key of the switch description when the site is not supported */
  unsupportedDescription: string
  /** Keys the client cannot query, with the switch description for them */
  keyCheck?: {
    supports: (key: KeyQuota) => boolean
    unsupportedDescription: string
  }
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
    defaultName: MAGPIE_DEFAULT_NAME,
    modelSelection: 'multiple',
    usageQuery: {
      supports: supportsMagpieBalance,
      description: 'Lets magpie show the remaining quota after import.',
      unsupportedDescription: 'magpie can only query balance for HTTPS sites.',
      keyCheck: {
        // An unlimited key's balance is the account quota, which the usage
        // endpoint returns only when the key allows querying it.
        supports: (key) => !key.unlimitedQuota || key.allowUserQuotaQuery,
        unsupportedDescription:
          'This key has no limit of its own and does not allow querying the account quota, so magpie cannot show its balance.',
      },
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
  /** Whether the key may query the account quota; used by the magpie balance */
  allowUserQuotaQuery?: boolean
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
  const unlimitedQuota = props.unlimitedQuota === true
  const allowUserQuotaQuery = props.allowUserQuotaQuery === true
  const usageQuerySiteSupported = useMemo(
    () =>
      props.open &&
      targetConfig.usageQuery !== null &&
      targetConfig.usageQuery.supports(getServerAddress()),
    [props.open, targetConfig.usageQuery]
  )
  const usageQueryKeySupported =
    targetConfig.usageQuery?.keyCheck?.supports({
      unlimitedQuota,
      allowUserQuotaQuery,
    }) ?? true
  const usageQuerySupported = usageQuerySiteSupported && usageQueryKeySupported
  let usageQueryDescription = targetConfig.usageQuery?.description ?? ''
  if (!usageQuerySiteSupported) {
    usageQueryDescription =
      targetConfig.usageQuery?.unsupportedDescription ?? ''
  } else if (!usageQueryKeySupported) {
    usageQueryDescription =
      targetConfig.usageQuery?.keyCheck?.unsupportedDescription ?? ''
  }

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
      unlimitedQuota,
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
            placeholder={targetConfig.defaultName ?? currentConfig.defaultName}
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
                {t(usageQueryDescription)}
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
