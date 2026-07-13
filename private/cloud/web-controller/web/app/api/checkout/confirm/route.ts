import { NextResponse } from 'next/server'
import { bffURL, requireCSRF, sessionToken } from '../../../../lib/bff'

export async function POST(request: Request) {
  if (!await requireCSRF(request)) return NextResponse.json({ error: 'csrf_rejected' }, { status: 403 })
  const token = await sessionToken()
  if (!token) return NextResponse.json({ error: 'login_required' }, { status: 401 })
  const upstream = await fetch(bffURL('/v1/web/staging/confirm'), { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: await request.text(), cache: 'no-store' })
  return new NextResponse(await upstream.text(), { status: upstream.status, headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' } })
}
