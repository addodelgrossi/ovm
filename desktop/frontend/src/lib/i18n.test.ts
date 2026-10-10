import { describe, expect, it } from 'vitest'
import { en, normalizeLocale, ptBR, t } from './i18n'

describe('i18n dictionaries', () => {
  it('keeps English and pt-BR keys in exact parity', () => {
    expect(Object.keys(ptBR).sort()).toEqual(Object.keys(en).sort())
  })

  it('normalizes unsupported locales and interpolates typed messages', () => {
    expect(normalizeLocale('fr')).toBe('en')
    expect(normalizeLocale('pt-BR')).toBe('pt-BR')
    expect(t('en', 'app.version', { version: '0.3.0' })).toBe('Version 0.3.0')
    expect(t('pt-BR', 'app.version', { version: '0.3.0' })).toBe('Versão 0.3.0')
  })
})
