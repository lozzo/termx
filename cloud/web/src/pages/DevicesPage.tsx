import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy, Plus, ShieldX } from 'lucide-react'
import { useState } from 'react'
import { protoSend } from '../api'
import { compactID, dateTime } from '../format'
import { CreateDaemonEnrollmentResponseSchema, CreateMyDaemonEnrollmentRequestSchema, ListMyDaemonsResponseSchema, RevokeMyDaemonRequestSchema, RevokeMyDaemonResponseSchema, type ManagedDaemon } from '../generated/cloud/v1/enrollment_pb'
import { useProtoQuery } from '../query'
import { Button, Dialog, Empty, ErrorState, Field, Input, Notice, PageHeader, Skeleton, Status, TableFrame } from '../ui'

export function DevicesPage() {
  const query = useProtoQuery(['user', 'daemons'], '/api/daemons', ListMyDaemonsResponseSchema, 8_000)
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const [daemonName, setDaemonName] = useState('')
  const [command, setCommand] = useState('')
  const [target, setTarget] = useState<ManagedDaemon>()
  const [reason, setReason] = useState('')
  const enroll = useMutation({ mutationFn: () => protoSend('/api/daemons/enroll', CreateMyDaemonEnrollmentRequestSchema, create(CreateMyDaemonEnrollmentRequestSchema, { daemonName }), CreateDaemonEnrollmentResponseSchema), onSuccess: (value) => setCommand(value.enrollCommand) })
  const revoke = useMutation({ mutationFn: () => protoSend(`/api/daemons/${target?.daemon?.daemonId}/revoke`, RevokeMyDaemonRequestSchema, create(RevokeMyDaemonRequestSchema, { expectedRevision: target?.daemon?.revision, reason }), RevokeMyDaemonResponseSchema), onSuccess: () => { setTarget(undefined); setReason(''); void client.invalidateQueries({ queryKey: ['user', 'daemons'] }) } })
  return <>
    <PageHeader title="我的设备" meta="daemon 身份持久保存，在线 Edge 位置只保存在 Controller 内存中" actions={<Button tone="primary" onClick={() => { setCommand(''); setDaemonName(''); setOpen(true) }}><Plus size={17} />添加 daemon</Button>} />
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} /> : !query.data?.daemons.length ? <Empty>还没有 daemon，点击“添加 daemon”生成一次性注册命令。</Empty> : <div className="user-data-table"><TableFrame><table><thead><tr><th>设备</th><th>状态</th><th>当前 Edge</th><th>设备身份</th><th>添加时间</th><th /></tr></thead><tbody>{query.data.daemons.map((value) => <tr key={value.daemon?.daemonId}><td data-label="设备"><strong>{value.daemon?.displayName}</strong><small className="mono">{compactID(value.daemon?.daemonId ?? '')}</small></td><td data-label="状态"><Status active={Boolean(value.runtime?.online)}>{value.daemon?.revoked ? '已撤销' : value.runtime?.online ? '在线' : '离线'}</Status></td><td data-label="当前 Edge">{value.runtime?.online ? <><strong>{value.runtime.edgeName}</strong><small>{value.runtime.edgeRegion} · {value.runtime.edgePublicEndpoint}</small></> : '-'}</td><td data-label="设备身份"><span className="mono">{compactID(value.daemon?.deviceId ?? '')}</span><small className="mono">{compactID(value.daemon?.deviceFingerprint ?? '')}</small></td><td data-label="添加时间">{dateTime(value.daemon?.createdAt)}</td><td data-label="">{!value.daemon?.revoked && <Button tone="danger" onClick={() => setTarget(value)}><ShieldX size={15} />撤销</Button>}</td></tr>)}</tbody></table></TableFrame></div>}
    <Dialog title={command ? '注册命令已生成' : '添加 daemon'} open={open} onClose={() => setOpen(false)} footer={command ? <Button tone="primary" onClick={() => setOpen(false)}>完成</Button> : <><Button tone="quiet" onClick={() => setOpen(false)}>取消</Button><Button tone="primary" onClick={() => enroll.mutate()} disabled={!daemonName.trim() || enroll.isPending}>生成命令</Button></>}>
      {command ? <><Notice tone="warning">命令中的注册口令十分钟内有效，且只能使用一次。</Notice><div className="command-box"><code>{command}</code><Button onClick={() => navigator.clipboard.writeText(command)}><Copy size={16} />复制</Button></div></> : <><Field label="设备名称" hint="例如：办公室 Mac"><Input autoFocus value={daemonName} onChange={(event) => setDaemonName(event.target.value)} /></Field>{enroll.error && <p className="form-error" role="alert">无法生成注册命令，请检查当前套餐设备额度。</p>}</>}
    </Dialog>
    <Dialog title="撤销 daemon" open={Boolean(target)} onClose={() => setTarget(undefined)} footer={<><Button tone="quiet" onClick={() => setTarget(undefined)}>取消</Button><Button tone="danger" onClick={() => revoke.mutate()} disabled={!reason.trim() || revoke.isPending}>确认撤销</Button></>}><Notice tone="warning">撤销后该 daemon 不能再获取 Cloud 票据；在线连接会立即尝试断开。</Notice><Field label="撤销原因"><Input autoFocus value={reason} onChange={(event) => setReason(event.target.value)} /></Field>{revoke.error && <p className="form-error" role="alert">撤销失败，设备状态可能已经变化，请刷新后重试。</p>}</Dialog>
  </>
}
