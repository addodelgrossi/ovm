import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'
import App from './App.svelte'

afterEach(cleanup)

describe('App', () => {
  it('renders the foundation shell and project principles', () => {
    render(App)

    expect(screen.getByRole('heading', { name: 'ovm' })).toBeTruthy()
    expect(
      screen.getByRole('heading', { name: 'Connect your first camera' })
    ).toBeTruthy()
    expect(screen.getByText('Local by design')).toBeTruthy()
    expect(screen.getByText('Credentials protected')).toBeTruthy()
    expect(screen.getByText('Changes verified')).toBeTruthy()
  })

  it('makes the unavailable scan action explicit', () => {
    render(App)

    const scan = screen.getByRole('button', {
      name: 'Scan network'
    }) as HTMLButtonElement
    expect(scan.disabled).toBe(true)
    expect(scan.title).toBe('Available in milestone M1')
  })
})
