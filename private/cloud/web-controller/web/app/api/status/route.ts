import { NextResponse } from 'next/server'

export const dynamic = 'force-dynamic'

export async function GET() {
  const upstream = process.env.WEB_CONTROLLER_BFF_URL?.replace(/\/$/, '')
  if (!upstream) {
    return NextResponse.json({ ready: false, error: 'bff_not_configured' }, { status: 503 })
  }
  try {
    const response = await fetch(`${upstream}/v1/status`, { cache: 'no-store', signal: AbortSignal.timeout(2500) })
    const body = await response.json()
    return NextResponse.json(body, { status: response.status, headers: { 'Cache-Control': 'no-store' } })
  } catch {
    return NextResponse.json({ ready: false, error: 'bff_unavailable' }, { status: 503 })
  }
}
