import { NextResponse } from 'next/server'
import { loadCatalog } from '../../../lib/catalog'

export const dynamic = 'force-dynamic'

export async function GET() {
  try {
    return NextResponse.json(await loadCatalog(), {
      headers: { 'Cache-Control': 'public, max-age=60, stale-while-revalidate=300' },
    })
  } catch {
    return NextResponse.json({ error: 'catalog_unavailable' }, { status: 503 })
  }
}
