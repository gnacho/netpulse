// integrationsApi.ts - wire types and calls for the external router pushers
// ("integrations") admin API. Mirrors rackApi.ts (req<T> + json helpers) but
// the error envelope here carries a machine code in `error` plus an optional
// human `message`, so ApiError keeps both for the i18n code -> key mapping.

export interface Integration {
  slug: string
  vendor: string
  mode: 'local' | 'ssh'
  host: string
  username: string
  version: string
  arch: string
  intervalS: number
  enabled: boolean
  state: string
  lastError: string
  /** Código estable del motivo offline (#1718): vacío si empuja o está pausada. */
  offlineReason?: string
  lastStartMs: number
  lastPushMs: number
  fresh: boolean
}

export interface Vendor {
  id: string
  mode: 'local' | 'ssh'
  arches: string[]
  tagPrefix: string
  description: string
}

/** Catalog served by GET /api/integrations/vendors (#1697). */
export interface VendorsResponse {
  vendors: Vendor[]
  /** Unix ms of the last successful fetch, 0 when only the seed was served. */
  lastRefreshedAt?: number
}

export interface CreateIntegrationInput {
  slug: string
  vendor: string
  host: string
  username: string
  password: string
  intervalS: number
  arch?: string
}

/** Editable fields of an existing integration. Omitted fields are left as they are. */
export interface EditIntegrationInput {
  host?: string
  username?: string
  password?: string
  intervalS?: number
}

/** Error carrying the backend machine code plus a human-readable message. */
export class ApiError extends Error {
  code?: string
  constructor(message: string, code?: string) {
    super(message)
    this.name = 'ApiError'
    this.code = code
  }
}

async function req<T>(input: string, init?: RequestInit): Promise<T> {
  const res = await fetch(input, init)
  if (!res.ok) {
    let code: string | undefined
    let msg = `HTTP ${res.status}`
    try {
      const body = (await res.json()) as { error?: string; message?: string }
      if (body?.error) code = body.error
      if (body?.message) msg = body.message
      else if (body?.error) msg = body.error
    } catch {
      /* default message */
    }
    throw new ApiError(msg, code)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

const json = (method: string, body: unknown): RequestInit => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
})

export function fetchVendors(): Promise<VendorsResponse> {
  return req<VendorsResponse>('/api/integrations/vendors')
}

/** Forces the vendor catalog discovery (admin). Returns the refreshed catalog. */
export function refreshVendors(): Promise<VendorsResponse> {
  return req<VendorsResponse>('/api/integrations/vendors/refresh', { method: 'POST' })
}

export function fetchIntegrations(): Promise<{ integrations: Integration[] }> {
  return req<{ integrations: Integration[] }>('/api/integrations')
}

export function createIntegration(
  input: CreateIntegrationInput,
): Promise<{ integration: Integration; token: string }> {
  return req<{ integration: Integration; token: string }>('/api/integrations', json('POST', input))
}

export function startIntegration(slug: string): Promise<{ integration: Integration }> {
  return req<{ integration: Integration }>(`/api/integrations/${encodeURIComponent(slug)}/start`, {
    method: 'POST',
  })
}

export function stopIntegration(slug: string): Promise<{ integration: Integration }> {
  return req<{ integration: Integration }>(`/api/integrations/${encodeURIComponent(slug)}/stop`, {
    method: 'POST',
  })
}

export function updateIntegration(slug: string): Promise<{ integration: Integration }> {
  return req<{ integration: Integration }>(`/api/integrations/${encodeURIComponent(slug)}/update`, {
    method: 'POST',
  })
}

export function editIntegration(
  slug: string,
  input: EditIntegrationInput,
): Promise<{ integration: Integration }> {
  return req<{ integration: Integration }>(
    `/api/integrations/${encodeURIComponent(slug)}`,
    json('PUT', input),
  )
}

export function checkIntegration(slug: string): Promise<{ integration: Integration; running: boolean }> {
  return req<{ integration: Integration; running: boolean }>(
    `/api/integrations/${encodeURIComponent(slug)}/check`,
    { method: 'POST' },
  )
}

export function deleteIntegration(slug: string): Promise<void> {
  return req<void>(`/api/integrations/${encodeURIComponent(slug)}`, { method: 'DELETE' })
}
