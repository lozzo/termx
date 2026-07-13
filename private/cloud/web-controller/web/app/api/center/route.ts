import { proxyGET } from '../../../lib/proxy'
export async function GET() { return proxyGET('/v1/web/center') }
