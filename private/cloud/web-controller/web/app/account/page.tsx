'use client'

import { Check, CreditCard, LogOut, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'

interface Order { id: string; plan_id: string; status: string; created_at: string }
interface Account { email: string; account_id: string; plan_id: string; valid_until?: string; orders: Order[] }

function csrf(): string { return document.cookie.split('; ').find((value) => value.startsWith('termx_csrf='))?.split('=')[1] ?? '' }

export default function AccountPage() {
  const [account, setAccount] = useState<Account | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function load() {
    const response = await fetch('/api/account', { cache: 'no-store' })
    if (response.status === 401) { window.location.href = '/login'; return }
    setAccount(await response.json())
  }
  useEffect(() => { void load() }, [])
  async function subscribe() {
    setBusy(true); setError('')
    const created = await fetch('/api/checkout', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-TermX-CSRF': csrf() }, body: JSON.stringify({ plan_id: 'pro' }) })
    if (!created.ok) { setError('Checkout could not be created.'); setBusy(false); return }
    const order = await created.json() as Order
    const paid = await fetch('/api/checkout/confirm', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-TermX-CSRF': csrf() }, body: JSON.stringify({ order_id: order.id }) })
    if (!paid.ok) { setError('Staging payment could not be confirmed.'); setBusy(false); return }
    await load(); setBusy(false)
  }
  async function logout() {
    await fetch('/api/auth/logout', { method: 'POST', headers: { 'X-TermX-CSRF': csrf() } }); window.location.href = '/'
  }
  if (!account) return <main className="account-loading"><RefreshCw className="spin" /> Loading account</main>
  return <main className="account-shell">
    <header><a className="brand" href="/">TermX</a><button onClick={logout} aria-label="Sign out"><LogOut size={17} /> Sign out</button></header>
    <div className="account-content">
      <div className="account-heading"><div><p>Web Controller</p><h1>Subscription</h1><span>{account.email}</span></div><strong>{account.plan_id === 'pro' ? 'Pro' : 'Managed Free'}</strong></div>
      <section className="subscription-summary">
        <div><small>Current plan</small><h2>{account.plan_id === 'pro' ? 'Pro' : 'Managed Free'}</h2><p>{account.valid_until ? `Active until ${new Date(account.valid_until).toLocaleDateString()}` : 'Direct connectivity and managed signaling included.'}</p></div>
        {account.plan_id !== 'pro' ? <button className="button primary" disabled={busy} onClick={subscribe}><CreditCard size={18} /> {busy ? 'Processing...' : 'Test Pro checkout'}</button> : <span className="active-plan"><Check size={17} /> Active</span>}
      </section>
      {error ? <p className="form-error">{error}</p> : null}
      <section className="orders"><h2>Orders</h2>{account.orders.length === 0 ? <p>No orders yet.</p> : account.orders.map(order => <div key={order.id}><span><strong>{order.plan_id.toUpperCase()}</strong><small>{order.id}</small></span><span>{new Date(order.created_at).toLocaleString()}</span><em className={order.status}>{order.status}</em></div>)}</section>
      <p className="staging-notice">Staging payment provider. No money is charged. Production checkout remains disabled.</p>
    </div>
  </main>
}
