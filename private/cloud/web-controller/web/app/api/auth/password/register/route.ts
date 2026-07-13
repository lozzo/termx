import { NextResponse } from 'next/server'
import { bffURL, requireSameOrigin, setSessionCookies } from '../../../../../lib/bff'

export async function POST(request: Request) {
  if (!await requireSameOrigin(request)) return NextResponse.json({ error: 'origin_rejected' }, { status: 403 })
  const upstream = await fetch(bffURL('/v1/web/auth/password/register'), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: await request.text(), cache: 'no-store' })
  if (!upstream.ok) return NextResponse.json({ error: (await upstream.json().catch(() => ({ error: 'registration_failed' }))).error }, { status: upstream.status })
  const session = await upstream.json() as { token: string; email: string }
  const response = NextResponse.json({ email: session.email })
  setSessionCookies(response, session.token)
  return response
}
