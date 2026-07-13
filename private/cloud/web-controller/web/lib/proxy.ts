import 'server-only'
import { NextResponse } from 'next/server'
import { bffURL, requireCSRF, sessionToken } from './bff'

export async function proxyGET(path: string) {
  const token = await sessionToken(); if (!token) return NextResponse.json({ error: 'login_required' }, { status: 401 })
  const upstream = await fetch(bffURL(path), { headers: { Authorization: `Bearer ${token}` }, cache: 'no-store' })
  return new NextResponse(await upstream.text(), { status: upstream.status, headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' } })
}

export async function proxyMutation(request: Request, path: string, method = 'POST') {
  if (!await requireCSRF(request)) return NextResponse.json({ error: 'csrf_rejected' }, { status: 403 })
  const token = await sessionToken(); if (!token) return NextResponse.json({ error: 'login_required' }, { status: 401 })
  const upstream = await fetch(bffURL(path), { method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: await request.text(), cache: 'no-store' })
  if (upstream.status === 204) return new NextResponse(null, { status: 204, headers: { 'Cache-Control': 'no-store' } })
  return new NextResponse(await upstream.text(), { status: upstream.status, headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' } })
}
