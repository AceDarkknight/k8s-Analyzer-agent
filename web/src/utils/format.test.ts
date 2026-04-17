import { describe, expect, it } from 'vitest'
import { formatDuration, formatTimestamp, formatTokens } from './format'

describe('format utils', () => {
  it('formats duration', () => {
    expect(formatDuration(999)).toBe('999ms')
    expect(formatDuration(1500)).toBe('1.5s')
    expect(formatDuration(61_000)).toBe('1m 1s')
  })

  it('formats timestamp', () => {
    expect(formatTimestamp('2026-04-16T18:22:34+08:00')).toBe('2026-04-16 18:22:34')
    expect(formatTimestamp('')).toBe('-')
  })

  it('formats tokens', () => {
    expect(formatTokens(42)).toBe('42')
    expect(formatTokens(1500)).toBe('1.5K')
    expect(formatTokens(2_000_000)).toBe('2.0M')
  })
})
