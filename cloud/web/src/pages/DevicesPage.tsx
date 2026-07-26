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
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const [target, setTarget] = useState<ManagedDaemon>()
  const [reason, setReason] = useState('')
  const enroll = useMutation({ mutationFn: () => protoSend('/api/daemons/enroll', CreateMyDaemonEnrollmentRequestSchema, create(CreateMyDaemonEnrollmentRequestSchema, { daemonName }), CreateDaemonEnrollmentResponseSchema), onSuccess: (value) => setCommand(value.enrollCommand) })
  const revoke = useMutation({ mutationFn: () => protoSend(`/api/daemons/${target?.daemon?.daemonId}/revoke`, RevokeMyDaemonRequestSchema, create(RevokeMyDaemonRequestSchema, { expectedRevision: target?.daemon?.revision, reason }), RevokeMyDaemonResponseSchema), onSuccess: () => { setTarget(undefined); setReason(''); void client.invalidateQueries({ queryKey: ['user', 'daemons'] }) } })
  async function copyCommand() {
    try { await navigator.clipboard.writeText(command); setCopyState('copied') } catch { setCopyState('failed') }
  }
  return <>
    <PageHeader title="我的设备" meta="在需要远程访问的电脑上安装 Muxvia daemon，设备上线后会自动显示当前连接区域" actions={<Button tone="primary" onClick={() => { setCommand(''); setDaemonName(''); setCopyState('idle'); setOpen(true) }}><Plus size={17} />添加设备</Button>} />
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} /> : !query.data?.daemons.length ? <Empty>还没有设备。点击“添加设备”生成一次性安装命令。</Empty> : <div className="user-data-table"><TableFrame><table><thead><tr><th>设备</th><th>状态</th><th>当前连接区域</th><th>设备身份</th><th>添加时间</th><th /></tr></thead><tbody>{query.data.daemons.map((value) => <tr key={value.daemon?.daemonId}><td data-label="设备"><strong>{value.daemon?.displayName}</strong><small className="mono">{compactID(value.daemon?.daemonId ?? '')}</small></td><td data-label="状态"><Status active={Boolean(value.runtime?.online)}>{value.daemon?.revoked ? '已撤销' : value.runtime?.online ? '在线' : '离线'}</Status></td><td data-label="连接区域">{value.runtime?.online ? <><strong>{value.runtime.edgeName}</strong><small>{value.runtime.edgeRegion} · {value.runtime.edgePublicEndpoint}</small></> : '-'}</td><td data-label="设备身份"><span className="mono">{compactID(value.daemon?.deviceId ?? '')}</span><small className="mono">{compactID(value.daemon?.deviceFingerprint ?? '')}</small></td><td data-label="添加时间">{dateTime(value.daemon?.createdAt)}</td><td data-label="">{!value.daemon?.revoked && <Button tone="danger" onClick={() => setTarget(value)}><ShieldX size={15} />撤销</Button>}</td></tr>)}</tbody></table></TableFrame></div>}
    <Dialog title={command ? '安装命令已生成' : '添加设备'} open={open} onClose={() => setOpen(false)} footer={command ? <Button tone="primary" onClick={() => setOpen(false)}>完成</Button> : <><Button tone="quiet" onClick={() => setOpen(false)}>取消</Button><Button tone="primary" onClick={() => enroll.mutate()} disabled={!daemonName.trim() || enroll.isPending}>生成命令</Button></>}>
      {command ? <><Notice tone="warning">请在目标电脑的终端中运行下面的命令。注册口令十分钟内有效，且只能使用一次。</Notice><div className="command-box"><code>{command}</code><Button onClick={() => void copyCommand()}><Copy size={16} />{copyState === 'copied' ? '已复制' : '复制命令'}</Button></div>{copyState === 'copied' && <Notice>安装命令已复制到剪贴板。</Notice>}{copyState === 'failed' && <Notice tone="error">无法访问剪贴板，请手动选择并复制命令。</Notice>}</> : <><Field label="设备名称" hint="例如：办公室 Mac"><Input autoFocus value={daemonName} onChange={(event) => setDaemonName(event.target.value)} /></Field>{enroll.error && <p className="form-error" role="alert">无法生成安装命令。请检查当前套餐的设备数量上限。</p>}</>}
    </Dialog>
    <Dialog title="撤销设备" open={Boolean(target)} onClose={() => setTarget(undefined)} footer={<><Button tone="quiet" onClick={() => setTarget(undefined)}>取消</Button><Button tone="danger" onClick={() => revoke.mutate()} disabled={!reason.trim() || revoke.isPending}>确认撤销</Button></>}><Notice tone="warning">撤销后，这台设备不能再获取 Cloud 连接票据；现有在线连接会立即尝试断开。</Notice><Field label="撤销原因"><Input autoFocus value={reason} onChange={(event) => setReason(event.target.value)} /></Field>{revoke.error && <p className="form-error" role="alert">撤销失败，设备状态可能已经变化，请刷新后重试。</p>}</Dialog>
  </>
}
