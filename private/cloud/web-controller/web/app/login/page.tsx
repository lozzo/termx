'use client'

import { ArrowRight, ShieldCheck } from 'lucide-react'
import { useState } from 'react'

export default function LoginPage() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  async function login() {
    setLoading(true); setError('')
    const response = await fetch('/api/auth/login', { method: 'POST' })
    if (response.ok) window.location.href = '/account'
    else { setError('Development identity provider is unavailable.'); setLoading(false) }
  }
  return <main className="auth-shell">
    <a className="auth-brand" href="/">TermX</a>
    <section className="auth-panel">
      <p className="kicker dark"><span /> Staging account</p>
      <h1>Sign in to Web Controller</h1>
      <p>This development environment uses the fixed Control Plane account. Production identity remains disabled until an OAuth provider is configured.</p>
      <button className="button primary auth-button" disabled={loading} onClick={login}>{loading ? 'Signing in...' : 'Continue with staging account'} <ArrowRight size={18} /></button>
      {error ? <p className="form-error">{error}</p> : null}
      <small><ShieldCheck size={15} /> HttpOnly session · SameSite strict · CSRF protected</small>
    </section>
  </main>
}
