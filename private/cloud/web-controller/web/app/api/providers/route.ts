import { NextResponse } from 'next/server'
import { bffURL } from '../../../lib/bff'
export async function GET() { const response = await fetch(bffURL('/v1/web/auth/providers'), { cache: 'no-store' }); return new NextResponse(await response.text(), { status: response.status, headers: { 'Content-Type': 'application/json' } }) }
