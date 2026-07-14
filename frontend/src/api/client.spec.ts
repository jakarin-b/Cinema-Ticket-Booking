import { describe, expect, it } from 'vitest'
import { money } from './client'

describe('money', () => {
  it('formats minor THB units', () => { expect(money(25000)).toContain('250') })
})

