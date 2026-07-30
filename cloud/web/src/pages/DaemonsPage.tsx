import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy, Link2Off, Plus } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { APIError, protoSend } from '../api'
import { compactID, dateTime } from '../format'
import { CreateDaemonEnrollmentRequestSchema, CreateDaemonEnrollmentResponseSchema, ListDaemonsResponseSchema, type ManagedDaemon } from '../generated/cloud/v1/enrollment_pb'
import { DisconnectDaemonRequestSchema, DisconnectDaemonResponseSchema } from '../generated/cloud/v1/operator_pb'
import { useProtoQuery } from '../query'
import { Button, Dialog, Empty, ErrorState, Field, Input, Notice, PageHeader, Skeleton, Status, TableFrame } from '../ui'

export function DaemonsPage() {
  const query = useProtoQuery(['daemons'], '/api/operator/daemons', ListDaemonsResponseSchema, 5_000)
  const client = useQueryClient()
  const [enrollOpen, setEnrollOpen] = useState(false)
  const [accountID, setAccountID] = useState('')
  const [daemonName, setDaemonName] = useState('')
  const [command, setCommand] = useState('')
  const [target, setTarget] = useState<ManagedDaemon>()
  const [reason, setReason] = useState('')
  const enroll = useMutation({ mutationFn: () => protoSend('/api/operator/daemons', CreateDaemonEnrollmentRequestSchema, create(CreateDaemonEnrollmentRequestSchema, { accountId: accountID, daemonName }), CreateDaemonEnrollmentResponseSchema), onSuccess: (value) => setCommand(value.enrollCommand) })
  const disconnect = useMutation({ mutationFn: () => protoSend(`/api/operator/daemons/${target?.daemon?.daemonId}/disconnect`, DisconnectDaemonRequestSchema, create(DisconnectDaemonRequestSchema, { generation: target?.runtime?.generation, reason }), DisconnectDaemonResponseSchema), onSuccess: () => { setTarget(undefined); setReason(''); void client.invalidateQueries({ queryKey: ['daemons'] }); void client.invalidateQueries({ queryKey: ['connections'] }) } })
  return <>
    <PageHeader title="在线 daemon" meta="身份记录来自数据库，在线位置只来自当前内存目录" actions={<Button tone="primary" onClick={() => { setCommand(''); setEnrollOpen(true) }}><Plus size={17} />注册 daemon</Button>} />
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : !query.data?.daemons.length ? <Empty>尚无 daemon</Empty> : <TableFrame><table><thead><tr><th>daemon</th><th>账号</th><th>当前 Edge</th><th>状态</th><th>设备</th><th>更新时间</th><th /></tr></thead><tbody>{query.data.daemons.map((value) => <tr key={value.daemon?.daemonId}><td><strong>{value.daemon?.displayName}</strong><small className="mono">{compactID(value.daemon?.daemonId ?? '')}</small></td><td><Link className="table-link" to={`/app/admin/accounts/${value.daemon?.accountId}`}>{value.daemon?.accountName || compactID(value.daemon?.accountId ?? '')}</Link></td><td>{value.runtime?.online ? <><strong>{value.runtime.edgeName}</strong><small>{value.runtime.edgeRegion} · {value.runtime.edgePublicEndpoint}</small></> : '-'}</td><td><Status active={Boolean(value.runtime?.online)}>{value.runtime?.online ? '在线' : '离线'}</Status></td><td>{value.daemon?.deviceId || '-'}</td><td>{dateTime(value.daemon?.updatedAt)}</td><td>{value.runtime?.online && <Button tone="danger" onClick={() => setTarget(value)}><Link2Off size={15} />断开</Button>}</td></tr>)}</tbody></table></TableFrame>}
    <Dialog title={command ? '注册命令' : '注册 daemon'} open={enrollOpen} onClose={() => setEnrollOpen(false)} footer={command ? <Button tone="primary" onClick={() => setEnrollOpen(false)}>完成</Button> : <><Button tone="quiet" onClick={() => setEnrollOpen(false)}>取消</Button><Button tone="primary" onClick={() => enroll.mutate()} disabled={!accountID || !daemonName || enroll.isPending}>生成命令</Button></>}>
      {command ? <><Notice tone="warning">注册口令只可消费一次。</Notice><div className="command-box"><code>{command}</code><Button onClick={() => navigator.clipboard.writeText(command)}><Copy size={16} />复制</Button></div></> : <div className="form-grid"><Field label="账号 ID" hint="必须是已经注册且状态正常的账号"><Input required value={accountID} onChange={(event) => setAccountID(event.target.value)} /></Field><Field label="daemon 名称"><Input required value={daemonName} onChange={(event) => setDaemonName(event.target.value)} /></Field>{enroll.error && <p className="form-error" role="alert">无法生成 daemon 注册命令，请检查账号和名称后重试。{enroll.error instanceof APIError && enroll.error.correlationID ? ` 关联 ID：${enroll.error.correlationID}` : ''}</p>}</div>}
    </Dialog>
    <Dialog title="断开 daemon" open={Boolean(target)} onClose={() => setTarget(undefined)} footer={<><Button tone="quiet" onClick={() => setTarget(undefined)}>取消</Button><Button tone="danger" onClick={() => disconnect.mutate()} disabled={!reason.trim() || disconnect.isPending}>确认断开</Button></>}><Notice tone="warning">将断开 {target?.daemon?.displayName} 当前 generation，不会删除 daemon 身份。</Notice><Field label="操作原因"><Input autoFocus value={reason} onChange={(event) => setReason(event.target.value)} /></Field>{disconnect.error && <p className="form-error" role="alert">无法断开 daemon，请刷新后重试。{disconnect.error instanceof APIError && disconnect.error.correlationID ? ` 关联 ID：${disconnect.error.correlationID}` : ''}</p>}</Dialog>
  </>
}
