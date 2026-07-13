import { proxyMutation } from '../../../lib/proxy'
export async function PATCH(request: Request) { return proxyMutation(request, '/v1/web/profile', 'PATCH') }
