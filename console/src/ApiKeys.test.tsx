import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent, screen } from '@testing-library/react'
import { ApiKeyRow, ApiKeyAccessSelect, accessScopes } from './ApiKeys'

const base = { id: 'k1', name: 'updater', prefix: 'sk_abc', created_at: '2026-08-01T00:00:00Z', last_used_at: null }

describe('ApiKeys', () => {
  it('shows an upgrade-only badge for scoped keys and none for full keys', () => {
    const { rerender } = render(<ApiKeyRow k={{ ...base, scopes: ['upgrade'] }} onRevoke={() => {}} />)
    expect(screen.getByTestId('api-key-scope').textContent).toBe('upgrade-only')
    rerender(<ApiKeyRow k={{ ...base, scopes: [] }} onRevoke={() => {}} />)
    expect(screen.queryByTestId('api-key-scope')).toBeNull()
  })

  it('maps the access select to scopes', () => {
    const onChange = vi.fn()
    render(<ApiKeyAccessSelect value="" onChange={onChange} />)
    fireEvent.change(screen.getByTestId('api-key-access'), { target: { value: 'upgrade' } })
    expect(onChange).toHaveBeenCalledWith('upgrade')
    expect(accessScopes('upgrade')).toEqual(['upgrade'])
    expect(accessScopes('')).toEqual([])
  })
})
