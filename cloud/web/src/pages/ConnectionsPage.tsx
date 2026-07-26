import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link2Off } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { protoSend } from '../api'
import { compactID, dateTime, product } from '../format'
import { DisconnectSessionRequestSchema, DisconnectSessionResponseSchema, ListRuntimeSessionsResponseSchema, type RuntimeSessionProjection } from '../generated/cloud/v1/operator_pb'
import { useProtoQuery } from '../query'
import { Button, Dialog, Empty, ErrorState, Field, Input, Notice, PageHeader, Skeleton, Status, TableFrame } from '../ui'

export function ConnectionsPage() {
  const query = useProtoQuery(['connections'], '/api/operator/connections', ListRuntimeSessionsResponseSchema, 5_000)
  const client = useQueryClient()
  const [target, setTarget] = useState<RuntimeSessionProjection>()
  const [reason, setReason] = useState('')
  const disconnect = useMutation({ mutationFn: () => protoSend(`/api/operator/connections/${target?.sessionId}/disconnect`, DisconnectSessionRequestSchema, create(DisconnectSessionRequestSchema, { generation: target?.generation, reason }), DisconnectSessionResponseSchema), onSuccess: () => { setTarget(undefined); setReason(''); void client.invalidateQueries({ queryKey: ['connections'] }) } })
  return <>
    <PageHeader title="实时连接" meta="对象离线后立即从内存列表消失，不读取历史拓扑" />
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} /> : !query.data?.sessions.length ? <Empty>当前没有客户端连接</Empty> : <TableFrame><table><thead><tr><th>Session</th><th>客户端</th><th>账号</th><th>目标 daemon</th><th>Edge</th><th>路径</th><th>开始时间</th><th /></tr></thead><tbody>{query.data.sessions.map((value) => <tr key={value.sessionId}><td className="mono">{compactID(value.sessionId)}</td><td><strong>{product(value.product)}</strong><small className="mono">{compactID(value.clientId)}</small></td><td><Link className="table-link mono" to={`/app/admin/accounts/${value.accountId}`}>{compactID(value.accountId)}</Link></td><td className="mono">{compactID(value.daemonId)}</td><td className="mono">{compactID(value.edgeId)}</td><td><Status active>{value.relay ? 'Relay' : 'P2P'}</Status></td><td>{dateTime(value.connectedAt)}</td><td><Button tone="danger" onClick={() => setTarget(value)}><Link2Off size={15} />断开</Button></td></tr>)}</tbody></table></TableFrame>}
    <Dialog title="断开实时连接" open={Boolean(target)} onClose={() => setTarget(undefined)} footer={<><Button tone="quiet" onClick={() => setTarget(undefined)}>取消</Button><Button tone="danger" onClick={() => disconnect.mutate()} disabled={!reason.trim() || disconnect.isPending}>确认断开</Button></>}><Notice tone="warning">将精确断开 session {compactID(target?.sessionId ?? '')}，客户端需要重新建立连接。</Notice><Field label="操作原因"><Input autoFocus value={reason} onChange={(event) => setReason(event.target.value)} /></Field>{disconnect.error && <p className="form-error">{disconnect.error.message}</p>}</Dialog>
  </>
}
