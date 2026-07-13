import { NextResponse } from 'next/server'
import { bffURL, sessionToken } from '../../../lib/bff'

export async function GET() {
  const token = await sessionToken()
  if (!token) return NextResponse.json({ error: 'login_required' }, { status: 401 })
  const upstream = await fetch(bffURL('/v1/web/account'), { headers: { Authorization: `Bearer ${token}` }, cache: 'no-store' })
  return new NextResponse(await upstream.text(), { status: upstream.status, headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' } })
}
