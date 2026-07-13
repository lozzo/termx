import 'server-only'

import { cookies, headers } from 'next/headers'
import { NextResponse } from 'next/server'
import { randomBytes } from 'node:crypto'

export const sessionCookie = 'termx_web_session'
export const csrfCookie = 'termx_csrf'

export function bffURL(path: string): string {
  const origin = process.env.WEB_CONTROLLER_BFF_URL?.replace(/\/$/, '')
  if (!origin) throw new Error('WEB_CONTROLLER_BFF_URL is required')
  return origin + path
}

export async function sessionToken(): Promise<string | undefined> {
  return (await cookies()).get(sessionCookie)?.value
}

export async function requireCSRF(request: Request): Promise<boolean> {
  const requestHeaders = await headers()
  const host = requestHeaders.get('host')
  const origin = requestHeaders.get('origin')
  const expectedOrigin = `${requestHeaders.get('x-forwarded-proto') ?? 'http'}://${host}`
  const expectedToken = (await cookies()).get(csrfCookie)?.value
  return Boolean(host && origin === expectedOrigin && expectedToken && request.headers.get('x-termx-csrf') === expectedToken)
}

export function setSessionCookies(response: NextResponse, token: string): string {
  const csrf = randomBytes(24).toString('hex')
  const base = { sameSite: 'strict' as const, secure: process.env.NODE_ENV === 'production' && process.env.TERMX_ALLOW_HTTP_COOKIE !== 'true', path: '/', maxAge: 8 * 60 * 60 }
  response.cookies.set(sessionCookie, token, { ...base, httpOnly: true })
  response.cookies.set(csrfCookie, csrf, { ...base, httpOnly: false })
  return csrf
}

export function clearSessionCookies(response: NextResponse) {
  response.cookies.set(sessionCookie, '', { path: '/', maxAge: 0 })
  response.cookies.set(csrfCookie, '', { path: '/', maxAge: 0 })
}
