import { NextResponse } from 'next/server'
import { clearSessionCookies, requireCSRF } from '../../../../lib/bff'

export async function POST(request: Request) {
  if (!await requireCSRF(request)) return NextResponse.json({ error: 'csrf_rejected' }, { status: 403 })
  const response = NextResponse.json({ ok: true })
  clearSessionCookies(response)
  return response
}
