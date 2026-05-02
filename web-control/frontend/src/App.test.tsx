import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { App } from './App'

describe('App', () => {
  it('renders a control-plane shell instead of a marketing landing page', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: 'TermX Control' })).toBeInTheDocument()
    expect(screen.getByText('Control Plane')).toBeInTheDocument()
    expect(screen.getByText('SQLite dev database')).toBeInTheDocument()
    expect(screen.getByText('WebRTC DataChannel runtime only')).toBeInTheDocument()
  })

  it('shows only supported client connection paths', () => {
    render(<App />)

    expect(screen.getByText('local')).toBeInTheDocument()
    expect(screen.getByText('public_p2p')).toBeInTheDocument()
    expect(screen.getByText('managed')).toBeInTheDocument()
    expect(screen.queryByText('paid_relay')).not.toBeInTheDocument()
    expect(screen.queryByText('anonymous_p2p')).not.toBeInTheDocument()
  })
})
