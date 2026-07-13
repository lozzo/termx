import { NextResponse } from 'next/server'
import { bffURL, setSessionCookies } from '../../../../lib/bff'

export async function POST() {
  const upstream = await fetch(bffURL('/v1/web/login'), { method: 'POST', cache: 'no-store' })
  if (!upstream.ok) return NextResponse.json({ error: 'login_unavailable' }, { status: 503 })
  const session = await upstream.json() as { token: string; email: string }
  const response = NextResponse.json({ email: session.email })
  setSessionCookies(response, session.token)
  return response
}
