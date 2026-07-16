import { beforeEach, describe, expect, it } from 'vitest'
import { clearIdempotencyKey, money, savedIdempotencyFingerprint, stableIdempotencyKey } from './client'

describe('money', () => {
  it('formats minor THB units', () => { expect(money(25000)).toContain('250') })
})

describe('stableIdempotencyKey', () => {
  beforeEach(() => sessionStorage.clear())

  it('reuses a key for the same request fingerprint and rotates it for a new request', () => {
    const first = stableIdempotencyKey('hold:showtime', 'seat-a,seat-b')
    expect(stableIdempotencyKey('hold:showtime', 'seat-a,seat-b')).toBe(first)
    expect(savedIdempotencyFingerprint('hold:showtime')).toBe('seat-a,seat-b')
    expect(stableIdempotencyKey('hold:showtime', 'seat-c')).not.toBe(first)
  })

  it('clears a completed request key', () => {
    const first = stableIdempotencyKey('confirm:hold', 'MOCK')
    clearIdempotencyKey('confirm:hold')
    expect(stableIdempotencyKey('confirm:hold', 'MOCK')).not.toBe(first)
  })
})
