import { KeyRound } from 'lucide-react'
import { ListEdgesResponseSchema } from '../generated/cloud/v1/edge_config_pb'
import { useProtoQuery } from '../query'
import { Empty, ErrorState, PageHeader, Skeleton, Status, TableFrame } from '../ui'

export function CertificatesPage() {
  const query = useProtoQuery(['edges'], '/api/operator/edges', ListEdgesResponseSchema, 30_000)
  return <>
    <PageHeader title="证书" meta="Edge 入口与当前部署证书状态" />
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} /> : !query.data?.edges.length ? <Empty>尚无可绑定的 Edge</Empty> : <TableFrame><table><thead><tr><th>Edge</th><th>域名或入口</th><th>档案版本</th><th>发布状态</th><th>有效期</th></tr></thead><tbody>{query.data.edges.map((value) => <tr key={value.config?.edgeId}><td><strong>{value.config?.name}</strong><small>{value.config?.region}</small></td><td className="mono">{value.config?.publicEndpoint}</td><td>-</td><td><Status active={false}>尚未接入档案</Status></td><td>-</td></tr>)}</tbody></table></TableFrame>}
    <section className="plain-section empty-section"><KeyRound size={22} /><h2>尚未配置证书档案</h2><p className="muted">当前 Edge 继续使用部署证书；没有可发布或回滚的受管版本。</p></section>
  </>
}
