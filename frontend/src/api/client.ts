import type { Envelope } from '@/types'

let tokenProvider = (): string | null => null
let csrfProvider = (): string | null => null
export function configureAuth(token: () => string | null, csrf: () => string | null) { tokenProvider = token; csrfProvider = csrf }

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public details: Record<string, unknown> = {}) { super(message) }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<Envelope<T>> {
  const headers = new Headers(init.headers)
  const token = tokenProvider()
  const csrf = csrfProvider()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  if (csrf && init.method && !['GET', 'HEAD'].includes(init.method)) headers.set('X-CSRF-Token', csrf)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const response = await fetch(`/api/v1${path}`, { ...init, headers, credentials: 'include' })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = payload.error ?? {}
    throw new ApiError(response.status, error.code ?? 'REQUEST_FAILED', error.message ?? 'Request failed.', error.details ?? {})
  }
  return payload as Envelope<T>
}

export function idempotencyKey(): string { return crypto.randomUUID() }
export function money(amount: number, currency = 'THB'): string { return new Intl.NumberFormat('en-TH', { style: 'currency', currency }).format(amount / 100) }
export function localDate(value: string): string { return new Intl.DateTimeFormat('en-TH', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }

