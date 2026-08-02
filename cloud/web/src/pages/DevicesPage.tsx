import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Ban, Copy, Plus, RadioTower, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { protoSend } from '../api'
import { compactID, dateTime } from '../format'
import { ChangeMyDaemonEdgePreferenceRequestSchema, ChangeMyDaemonEdgePreferenceResponseSchema, ChangeMyDaemonStateRequestSchema, ChangeMyDaemonStateResponseSchema, CreateDaemonEnrollmentResponseSchema, CreateMyDaemonEnrollmentRequestSchema, DaemonState, ListMyDaemonEdgesResponseSchema, ListMyDaemonsResponseSchema, ReselectMyDaemonEdgeRequestSchema, ReselectMyDaemonEdgeResponseSchema, type DaemonEdgeCandidate, type ManagedDaemon } from '../generated/cloud/v1/enrollment_pb'
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
  const [targetState, setTargetState] = useState<DaemonState>()
  const [reason, setReason] = useState('')
  const [edgeTarget, setEdgeTarget] = useState<ManagedDaemon>()
  const enroll = useMutation({ mutationFn: () => protoSend('/api/daemons/enroll', CreateMyDaemonEnrollmentRequestSchema, create(CreateMyDaemonEnrollmentRequestSchema, { daemonName }), CreateDaemonEnrollmentResponseSchema), onSuccess: (value) => setCommand(value.enrollCommand) })
  const changeState = useMutation({ mutationFn: () => protoSend(`/api/daemons/${target?.daemon?.daemonId}/state`, ChangeMyDaemonStateRequestSchema, create(ChangeMyDaemonStateRequestSchema, { daemonId: target?.daemon?.daemonId, targetState, expectedStateRevision: target?.daemon?.stateRevision, reason }), ChangeMyDaemonStateResponseSchema), onSuccess: () => { closeStateDialog(); void client.invalidateQueries({ queryKey: ['user', 'daemons'] }) } })
  function openStateDialog(daemon: ManagedDaemon, state: DaemonState) {
    setTarget(daemon)
    setTargetState(state)
    setReason('')
    changeState.reset()
  }
  function closeStateDialog() {
    setTarget(undefined)
    setTargetState(undefined)
    setReason('')
  }
  async function copyCommand() {
    try { await navigator.clipboard.writeText(command); setCopyState('copied') } catch { setCopyState('failed') }
  }
  return <>
    <PageHeader title="Daemon 管理" meta="Cloud 账号管理 daemon 注册、路由与 Relay；它不会把 daemon 自动加入 AnyTTY App" actions={<Button tone="primary" onClick={() => { setCommand(''); setDaemonName(''); setCopyState('idle'); setOpen(true) }}><Plus size={17} />注册 daemon</Button>} />
    <Notice>在这里注册目标 daemon。安装完成后，请在目标 daemon 或服务上生成配对二维码，并用无需账号的 AnyTTY App 扫描；配对端点只保存在 App 本机。</Notice>
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : !query.data?.daemons.length ? <Empty>还没有注册 daemon。生成一次性注册命令即可开始。</Empty> : <div className="user-data-table"><TableFrame><table><thead><tr><th>daemon</th><th>状态</th><th>Edge 路由</th><th>设备身份</th><th>注册时间</th><th /></tr></thead><tbody>{query.data.daemons.map((value) => {
      const blocked = value.daemon?.state === DaemonState.BLOCKED
      const preferred = value.daemon?.preferredEdgeId
      return <tr key={value.daemon?.daemonId}><td data-label="daemon"><strong>{value.daemon?.displayName}</strong><small className="mono">{compactID(value.daemon?.daemonId ?? '')}</small></td><td data-label="状态"><Status active={!blocked && Boolean(value.runtime?.online)}>{blocked ? '已停用' : value.runtime?.online ? '在线' : '离线'}</Status></td><td data-label="Edge 路由">{!blocked && value.runtime?.online ? <><strong>当前：{value.runtime.edgeName}</strong><small>{value.runtime.edgeRegion} · {value.runtime.edgePublicEndpoint}</small></> : <strong>当前：-</strong>}<small>偏好：{preferred ? compactID(preferred) : '自动选择'}</small></td><td data-label="设备身份"><span className="mono">{compactID(value.daemon?.deviceId ?? '')}</span><small className="mono">{compactID(value.daemon?.deviceFingerprint ?? '')}</small></td><td data-label="注册时间">{dateTime(value.daemon?.createdAt)}</td><td data-label=""><div className="daemon-actions"><Button onClick={() => setEdgeTarget(value)} disabled={blocked}><RadioTower size={15} />Edge</Button>{blocked ? <Button onClick={() => openStateDialog(value, DaemonState.ACTIVE)}><RotateCcw size={15} />恢复</Button> : <Button onClick={() => openStateDialog(value, DaemonState.BLOCKED)}><Ban size={15} />停用</Button>}<Button tone="danger" onClick={() => openStateDialog(value, DaemonState.DELETED)}><Trash2 size={15} />删除</Button></div></td></tr>
    })}</tbody></table></TableFrame></div>}
    <Dialog title={command ? '注册命令已生成' : '注册 daemon'} open={open} onClose={() => setOpen(false)} closable={!enroll.isPending} footer={command ? <Button tone="primary" onClick={() => setOpen(false)}>完成</Button> : <><Button tone="quiet" onClick={() => setOpen(false)} disabled={enroll.isPending}>取消</Button><Button tone="primary" onClick={() => enroll.mutate()} disabled={!daemonName.trim() || enroll.isPending}>生成命令</Button></>}>
      {command ? <><Notice tone="warning">请在目标电脑的终端中运行下面的命令。注册口令十分钟内有效且只能使用一次；注册完成后，还需在目标服务生成配对二维码供 App 扫描。</Notice><div className="command-box"><code>{command}</code><Button autoFocus onClick={() => void copyCommand()}><Copy size={16} />{copyState === 'copied' ? '已复制' : '复制命令'}</Button></div>{copyState === 'copied' && <Notice>注册命令已复制到剪贴板。</Notice>}{copyState === 'failed' && <Notice tone="error">无法访问剪贴板，请手动选择并复制命令。</Notice>}</> : <><Field label="daemon 名称" hint="例如：办公室 Mac"><Input autoFocus value={daemonName} onChange={(event) => setDaemonName(event.target.value)} /></Field>{enroll.error && <p className="form-error" role="alert">无法生成注册命令。请检查当前套餐的 daemon 数量上限。</p>}</>}
    </Dialog>
    <Dialog title={targetState === DaemonState.DELETED ? '永久删除 daemon' : targetState === DaemonState.ACTIVE ? '恢复 daemon' : '停用 daemon'} open={Boolean(target && targetState)} onClose={closeStateDialog} closable={!changeState.isPending} footer={<><Button tone="quiet" onClick={closeStateDialog} disabled={changeState.isPending}>取消</Button><Button tone={targetState === DaemonState.DELETED ? 'danger' : 'primary'} onClick={() => changeState.mutate()} disabled={!reason.trim() || changeState.isPending}>{targetState === DaemonState.DELETED ? '确认永久删除' : targetState === DaemonState.ACTIVE ? '确认恢复' : '确认停用'}</Button></>}>
      {targetState === DaemonState.DELETED ? <Notice tone="warning">删除不可恢复。daemon 会断开 Cloud 并移除本机 Cloud 注册凭据；以后使用 Cloud 必须重新注册 daemon，再生成并扫描新的配对二维码。Direct、SSH 和本地数据不受影响。</Notice> : targetState === DaemonState.ACTIVE ? <Notice>恢复后，daemon 会重新开放 Cloud 连接，无需重新注册。</Notice> : <Notice tone="warning">停用会阻断新 Cloud 连接并断开当前 Cloud 会话，之后可以在这里恢复。</Notice>}
      <Field label="操作原因"><Input autoFocus value={reason} onChange={(event) => setReason(event.target.value)} /></Field>
      {changeState.error && <p className="form-error" role="alert">操作失败，daemon 状态可能已经变化，请刷新后重试。</p>}
    </Dialog>
    {edgeTarget && <DaemonEdgeDialog daemon={edgeTarget} onClose={() => setEdgeTarget(undefined)} onChanged={() => void client.invalidateQueries({ queryKey: ['user', 'daemons'] })} />}
  </>
}

function DaemonEdgeDialog({ daemon, onClose, onChanged }: { daemon: ManagedDaemon; onClose: () => void; onChanged: () => void }) {
  const daemonID = daemon.daemon?.daemonId ?? ''
  const query = useProtoQuery(['user', 'daemon-edges', daemonID], `/api/daemons/${daemonID}/edges`, ListMyDaemonEdgesResponseSchema, 5_000)
  const [preferred, setPreferred] = useState(daemon.daemon?.preferredEdgeId ?? '')
  const [message, setMessage] = useState('')
  const preference = useMutation({ mutationFn: () => protoSend(`/api/daemons/${daemonID}/edge-preference`, ChangeMyDaemonEdgePreferenceRequestSchema, create(ChangeMyDaemonEdgePreferenceRequestSchema, { daemonId: daemonID, preferredEdgeId: preferred, expectedPreferenceRevision: query.data?.selection?.preferenceRevision ?? daemon.daemon?.edgePreferenceRevision, reselectNow: true }), ChangeMyDaemonEdgePreferenceResponseSchema), onSuccess: (value) => { setMessage(value.message); onChanged(); void query.refetch() } })
  const reselect = useMutation({ mutationFn: () => protoSend(`/api/daemons/${daemonID}/edge-reselect`, ReselectMyDaemonEdgeRequestSchema, create(ReselectMyDaemonEdgeRequestSchema, { daemonId: daemonID }), ReselectMyDaemonEdgeResponseSchema), onSuccess: (value) => { setMessage(value.message); onChanged(); void query.refetch() } })
  const pending = preference.isPending || reselect.isPending
  const selection = query.data?.selection
  useEffect(() => { if (selection) setPreferred(selection.preferredEdgeId) }, [selection])
  return <Dialog title={`Edge 路由 · ${daemon.daemon?.displayName ?? daemonID}`} open onClose={onClose} closable={!pending} footer={<><Button tone="quiet" onClick={onClose} disabled={pending}>关闭</Button><Button onClick={() => reselect.mutate()} disabled={pending || !daemon.runtime?.online}><RefreshCw size={16} />立即重选</Button><Button tone="primary" onClick={() => preference.mutate()} disabled={pending || !selection || preferred === selection.preferredEdgeId}>保存偏好并重选</Button></>}>
    <Notice>偏好不是强制绑定。偏好 Edge 离线、不可达或容量已满时，daemon 会自动回退到综合评分更好的节点。</Notice>
    {query.isPending ? <Skeleton rows={4} /> : query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : <div className="edge-choice-list" role="radiogroup" aria-label="偏好 Edge">
      <label className={`edge-choice ${preferred === '' ? 'is-selected' : ''}`}><input type="radio" name="preferred-edge" checked={preferred === ''} onChange={() => setPreferred('')} /><span className="edge-choice-main"><strong>自动选择</strong><small>不指定偏好，按连接质量与节点负载选择</small></span></label>
      {selection?.candidates.map((candidate) => <EdgeChoice key={candidate.locator?.edgeId} candidate={candidate} checked={preferred === candidate.locator?.edgeId} onChange={() => setPreferred(candidate.locator?.edgeId ?? '')} />)}
    </div>}
    {message && <Notice tone={message.includes('但') ? 'warning' : 'info'}>{message}</Notice>}
    {(preference.error || reselect.error) && <Notice tone="error">Edge 操作失败。偏好版本或当前连接可能已经变化，请刷新后重试。</Notice>}
  </Dialog>
}

function EdgeChoice({ candidate, checked, onChange }: { candidate: DaemonEdgeCandidate; checked: boolean; onChange: () => void }) {
  const measurement = candidate.measurement
  return <label className={`edge-choice ${checked ? 'is-selected' : ''}`}><input type="radio" name="preferred-edge" checked={checked} onChange={onChange} /><span className="edge-choice-main"><strong>{candidate.locator?.name}</strong><small>{candidate.locator?.region} · {candidate.locator?.publicEndpoint}</small></span><span className="edge-choice-metrics"><Status active={candidate.eligible}>{candidate.status}</Status><small>{measurement ? `${measurement.connectLatencyMs} ms · 连接失败率 ${Math.round(measurement.connectionFailureRate * 100)}%` : '尚无 daemon 侧测速'}</small><small>负载 {candidate.agentCount.toString()} / {candidate.capacity.toString()}{candidate.current ? ' · 当前' : ''}{candidate.preferred ? ' · 已偏好' : ''}</small></span></label>
}
