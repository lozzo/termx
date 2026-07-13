import { proxyMutation } from '../../../../lib/proxy'
export async function POST(request: Request) { return proxyMutation(request, '/v1/web/nodes/revoke') }
