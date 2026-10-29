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

export interface CreateIntegrationInput {
  slug: string
  vendor: string
  host: string
  username: string
  password: string
  intervalS: number
  arch?: string
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

export function fetchVendors(): Promise<{ vendors: Vendor[] }> {
  return req<{ vendors: Vendor[] }>('/api/integrations/vendors')
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

export function checkIntegration(slug: string): Promise<{ integration: Integration; running: boolean }> {
  return req<{ integration: Integration; running: boolean }>(
    `/api/integrations/${encodeURIComponent(slug)}/check`,
    { method: 'POST' },
  )
}

export function deleteIntegration(slug: string): Promise<void> {
  return req<void>(`/api/integrations/${encodeURIComponent(slug)}`, { method: 'DELETE' })
}
